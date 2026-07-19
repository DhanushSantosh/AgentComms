package authority

import (
	"context"
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
	if _, _, err = mutate("owner", owner, "invocation.request", "inv-exclusive",
		model.InvocationRequested{Target: "alpha", Instruction: "Perform one exclusive action"}, uuid.NewString()); err != nil {
		t.Fatal(err)
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
