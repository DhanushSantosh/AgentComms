package authority

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

func TestPostgresTransactionalAuthority(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	serviceSigner, _ := controlplane.GenerateSigner()
	engine, err := Open(context.Background(), Config{DatabaseURL: databaseURL}, serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	projectID := "integration-" + uuid.NewString()
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	alpha, _ := controlplane.GenerateSigner()
	beta, _ := controlplane.GenerateSigner()
	mutate := func(actor string, signer *controlplane.Signer, typ, entity string, payload any, key string) (controlplane.Event, controlplane.Receipt, error) {
		raw, encodeErr := model.EncodePayload(typ, payload)
		if encodeErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, encodeErr
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: typ, EntityID: entity,
			Payload: raw, IdempotencyKey: key, IssuedAt: time.Now().UTC(),
		}
		if typ == "agent.register" {
			command.PublicKey = signer.PublicKey()
		}
		if signErr := command.Sign(signer.PrivateKey()); signErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, signErr
		}
		return engine.Mutate(context.Background(), command)
	}
	register := func(id string, signer *controlplane.Signer) {
		t.Helper()
		if _, _, registerErr := mutate(id, signer, "agent.register", id, model.AgentRegistered{
			PublicKey: signer.PublicKey(), PrincipalType: model.PrincipalAgent, DisplayName: id,
		}, uuid.NewString()); registerErr != nil {
			t.Fatal(registerErr)
		}
	}
	register("owner", owner)
	if _, _, err = mutate("owner", owner, "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	register("alpha", alpha)
	register("beta", beta)
	for _, id := range []string{"alpha", "beta"} {
		if _, _, err = mutate("owner", owner, "agent.activate", id,
			model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	createPayload := model.TaskCreated{
		Title: "Exclusive", Repository: "local", Branch: "feature", Resources: []string{"src/exclusive"},
	}
	createRaw, _ := model.EncodePayload("task.create", createPayload)
	createCommand := controlplane.Command{
		ProjectID: projectID, Actor: "owner", Type: "task.create", EntityID: "exclusive",
		Payload: createRaw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	_ = createCommand.Sign(owner.PrivateKey())
	created, receipt, err := engine.Mutate(context.Background(), createCommand)
	if err != nil {
		t.Fatal(err)
	}
	replayed, replayReceipt, err := engine.Mutate(context.Background(), createCommand)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != created.ID || replayReceipt.Signature != receipt.Signature {
		t.Fatal("idempotent replay returned a different event or receipt")
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var writers sync.WaitGroup
	for id, signer := range map[string]*controlplane.Signer{"alpha": alpha, "beta": beta} {
		writers.Add(1)
		go func(id string, signer *controlplane.Signer) {
			defer writers.Done()
			<-start
			_, _, claimErr := mutate(id, signer, "task.claim", "exclusive", model.TaskClaimed{}, uuid.NewString())
			results <- claimErr
		}(id, signer)
	}
	close(start)
	writers.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent claims=%d, want 1", successes)
	}
	state, _, err := engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["exclusive"].Owner == "" {
		t.Fatal("exclusive task has no owner")
	}
	if _, _, err = mutate("alpha", alpha, "runtime.register", "runtime-alpha",
		model.RuntimeRegistered{AgentID: "alpha", Connector: "MCP", MaxConcurrent: 2}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = mutate("alpha", alpha, "runtime.heartbeat", "runtime-alpha",
		model.RuntimeHeartbeat{Health: "HEALTHY"}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = mutate("alpha", alpha, "runtime.register", "runtime-delivery",
		model.RuntimeRegistered{
			AgentID: "alpha", Connector: "LOCAL_PROCESS",
			ConfigReference: "integration-local", MaxConcurrent: 1,
		}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	for _, runtimeID := range []string{"runtime-a", "runtime-b"} {
		if _, _, err = mutate("alpha", alpha, "runtime.register", runtimeID,
			model.RuntimeRegistered{
				AgentID: "alpha", Connector: "MCP", MaxConcurrent: 1,
			}, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		if _, _, err = mutate("alpha", alpha, "runtime.heartbeat", runtimeID,
			model.RuntimeHeartbeat{Health: "HEALTHY"}, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = mutate("owner", owner, "invocation.request", "inv-exclusive",
		model.InvocationRequested{Target: "alpha", Instruction: "Perform one exclusive action"}, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	notificationStart := make(chan struct{})
	notificationResults := make(chan error, 2)
	var notificationWriters sync.WaitGroup
	for _, deliveryID := range []string{"delivery-a", "delivery-b"} {
		notificationWriters.Add(1)
		go func(deliveryID string) {
			defer notificationWriters.Done()
			<-notificationStart
			_, _, notifyErr := mutate("owner", owner, "invocation.delivery-attempt", "inv-exclusive",
				model.InvocationDeliveryAttempted{
					DeliveryID: deliveryID, RuntimeID: "runtime-delivery",
					Transport: "LOCAL_PROCESS",
				}, uuid.NewString())
			notificationResults <- notifyErr
		}(deliveryID)
	}
	close(notificationStart)
	notificationWriters.Wait()
	close(notificationResults)
	notificationSuccesses := 0
	for result := range notificationResults {
		if result == nil {
			notificationSuccesses++
		}
	}
	if notificationSuccesses != 1 {
		t.Fatalf("successful concurrent notification reservations=%d, want 1", notificationSuccesses)
	}
	invocationStart := make(chan struct{})
	invocationResults := make(chan error, 2)
	var invocationWriters sync.WaitGroup
	for _, runtimeID := range []string{"runtime-a", "runtime-b"} {
		invocationWriters.Add(1)
		go func(runtimeID string) {
			defer invocationWriters.Done()
			<-invocationStart
			_, _, claimErr := mutate("alpha", alpha, "invocation.claim", "inv-exclusive",
				model.InvocationClaimed{RuntimeID: runtimeID}, uuid.NewString())
			invocationResults <- claimErr
		}(runtimeID)
	}
	close(invocationStart)
	invocationWriters.Wait()
	close(invocationResults)
	invocationSuccesses := 0
	for result := range invocationResults {
		if result == nil {
			invocationSuccesses++
		}
	}
	if invocationSuccesses != 1 {
		t.Fatalf("successful concurrent invocation claims=%d, want 1", invocationSuccesses)
	}
	state, _, err = engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-exclusive"].Status != "CLAIMED" {
		t.Fatalf("invocation is not claimed: %+v", state.Invocations["inv-exclusive"])
	}
	if state.AgentRuntimes["runtime-alpha"].Status != "ONLINE" {
		t.Fatalf("runtime is not online: %+v", state.AgentRuntimes["runtime-alpha"])
	}
	const concurrentInvocations = 100
	loadResults := make(chan error, concurrentInvocations)
	var loadWriters sync.WaitGroup
	loadStart := make(chan struct{})
	for index := range concurrentInvocations {
		loadWriters.Add(1)
		go func(index int) {
			defer loadWriters.Done()
			<-loadStart
			_, _, loadErr := mutate("owner", owner, "invocation.request",
				fmt.Sprintf("inv-load-%03d", index),
				model.InvocationRequested{Target: "beta", Instruction: "Concurrent invocation load"},
				uuid.NewString())
			loadResults <- loadErr
		}(index)
	}
	close(loadStart)
	loadWriters.Wait()
	close(loadResults)
	for loadErr := range loadResults {
		if loadErr != nil {
			t.Fatalf("concurrent invocation load failed: %v", loadErr)
		}
	}
	state, _, err = engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range concurrentInvocations {
		if _, exists := state.Invocations[fmt.Sprintf("inv-load-%03d", index)]; !exists {
			t.Fatalf("concurrent invocation %d was lost", index)
		}
	}
	if err = engine.VerifyRange(context.Background(), projectID, 1, 0); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentSchemaInitializationIsSerialized(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	const workers = 4
	start := make(chan struct{})
	results := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			signer, generateErr := controlplane.GenerateSigner()
			if generateErr != nil {
				results <- generateErr
				return
			}
			engine, openErr := Open(context.Background(), Config{DatabaseURL: databaseURL}, signer)
			if openErr == nil {
				openErr = engine.Close()
			}
			results <- openErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent authority initialization failed: %v", err)
		}
	}
}

// TestApplySchemaSkipsDisruptiveMigrationWithoutAllowFlag proves finding
// 6/7's classification actually gates ordinary startup: a pending
// migration marked Automatic:false must be refused (and left unrecorded)
// under normal ApplySchema(allowDisruptive=false) startup, and only
// applied when the caller passes allowDisruptive=true -- the same flag
// `agent-comms-server migrate apply --yes --allow-disruptive` sets.
func TestApplySchemaSkipsDisruptiveMigrationWithoutAllowFlag(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ApplySchema(context.Background(), db, false); err != nil {
		t.Fatal(err)
	}

	const disruptiveVersion = 900001
	original := schemaMigrations
	schemaMigrations = append(append([]schemaMigration{}, original...), schemaMigration{
		Version: disruptiveVersion, Name: "test-disruptive-migration", Automatic: false, SQL: "SELECT 1",
	})
	defer func() { schemaMigrations = original }()
	if _, err = db.ExecContext(context.Background(), `DELETE FROM schema_migrations WHERE version=$1`, disruptiveVersion); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM schema_migrations WHERE version=$1`, disruptiveVersion)
	}()

	if err = ApplySchema(context.Background(), db, false); err == nil {
		t.Fatal("expected ApplySchema to refuse a pending disruptive migration without --allow-disruptive")
	}
	var recorded bool
	if err = db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, disruptiveVersion).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded {
		t.Fatal("disruptive migration must not be recorded as applied when it was refused")
	}

	if err = ApplySchema(context.Background(), db, true); err != nil {
		t.Fatalf("expected ApplySchema to apply the disruptive migration once allowDisruptive=true: %v", err)
	}
	if err = db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, disruptiveVersion).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if !recorded {
		t.Fatal("expected the disruptive migration to be recorded as applied after --allow-disruptive")
	}
}
