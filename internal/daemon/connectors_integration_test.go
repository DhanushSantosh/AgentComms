package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/authority"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
	"github.com/google/uuid"
)

func TestPostgresToCacheToLocalConnectorDelivery(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := authority.Open(context.Background(), authority.Config{DatabaseURL: databaseURL}, serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	server := httptest.NewServer(authority.NewHTTPServer(engine, authority.HTTPConfig{}).Handler())
	defer server.Close()
	client, err := remote.NewWithToken(server.URL, 5*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	projectID := "connector-" + uuid.NewString()
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	builder, _ := controlplane.GenerateSigner()
	secondBuilder, _ := controlplane.GenerateSigner()
	signers := map[string]*controlplane.Signer{"owner": owner, "claude-builder": builder, "codex-builder": secondBuilder}
	cache, err := localcache.Open(filepath.Join(t.TempDir(), "cache.db"), serviceSigner.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	applyToCache := true
	submit := func(ctx context.Context, _ string, actor, eventType, entityID string, payload any) error {
		raw, encodeErr := model.EncodePayload(eventType, payload)
		if encodeErr != nil {
			return encodeErr
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: eventType, EntityID: entityID,
			Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if eventType == "agent.register" {
			command.PublicKey = signers[actor].PublicKey()
		}
		if signErr := command.Sign(signers[actor].PrivateKey()); signErr != nil {
			return signErr
		}
		event, receipt, commandErr := client.Command(ctx, command)
		if commandErr != nil {
			return commandErr
		}
		if applyToCache {
			return cache.Apply(ctx, event, receipt)
		}
		return nil
	}
	for _, seed := range []struct {
		actor, eventType, entityID string
		payload                    any
	}{
		{"owner", "agent.register", "owner", model.AgentRegistered{
			PublicKey: owner.PublicKey(), PrincipalType: model.PrincipalHuman,
		}},
		{"owner", "agent.activate", "owner", model.AgentActivated{
			Role: model.RoleOwner, Scopes: []string{"*"}, Capabilities: []string{"*"},
		}},
		{"claude-builder", "agent.register", "claude-builder", model.AgentRegistered{
			PublicKey: builder.PublicKey(), PrincipalType: model.PrincipalAgent,
		}},
		{"owner", "agent.activate", "claude-builder", model.AgentActivated{
			Role: model.Role("MEMBER"), Scopes: []string{"src"},
		}},
		{"codex-builder", "agent.register", "codex-builder", model.AgentRegistered{
			PublicKey: secondBuilder.PublicKey(), PrincipalType: model.PrincipalAgent,
		}},
		{"owner", "agent.activate", "codex-builder", model.AgentActivated{
			Role: model.Role("MEMBER"), Scopes: []string{"src"},
		}},
		{"claude-builder", "runtime.register", "runtime-builder", model.RuntimeRegistered{
			AgentID: "claude-builder", Connector: "LOCAL_PROCESS", ConfigReference: "builder-local", MaxConcurrent: 1,
		}},
		{"codex-builder", "runtime.register", "runtime-second", model.RuntimeRegistered{
			AgentID: "codex-builder", Connector: "LOCAL_PROCESS", ConfigReference: "builder-local", MaxConcurrent: 1,
		}},
		{"owner", "invocation.request", "inv-deliver", model.InvocationRequested{
			Target: "claude-builder", Instruction: "Run connector integration", Scopes: []string{"src"},
		}},
	} {
		if err = submit(context.Background(), projectID, seed.actor, seed.eventType, seed.entityID, seed.payload); err != nil {
			t.Fatalf("%s: %v", seed.eventType, err)
		}
	}
	outputPath := filepath.Join(t.TempDir(), "connector-envelope.json")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewDispatcher(map[string]ConnectorConfig{
		"builder-local": {
			Type: "LOCAL_PROCESS", Executable: executable,
			Arguments: []string{"-test.run=TestConnectorHelperProcess", "--"},
			Environment: map[string]string{
				"CONNECTOR_TEST_HELPER": "1", "CONNECTOR_TEST_OUTPUT": outputPath,
			},
			Timeout: 5 * time.Second,
		},
	}, submit)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := New(cache, client)
	if err != nil {
		t.Fatal(err)
	}
	instance.SetDispatcher(dispatcher)
	if err = instance.Sync(context.Background(), projectID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(outputPath); err != nil {
		t.Fatalf("local connector did not receive the invocation: %v", err)
	}
	state, _, err := engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-deliver"].Status != "NOTIFIED" {
		t.Fatalf("authority did not record notification: %+v", state.Invocations["inv-deliver"])
	}
	if len(state.InvocationDeliveries) != 1 {
		t.Fatalf("delivery projection count=%d, want 1", len(state.InvocationDeliveries))
	}
	// Sustained alternating targets exercise two runtimes while the local
	// cache repeatedly catches up with authority writes and delivery receipts.
	for i := 0; i < 20; i++ {
		target, runtimeID := "claude-builder", "runtime-builder"
		if i%2 != 0 {
			target, runtimeID = "codex-builder", "runtime-second"
		}
		id := fmt.Sprintf("inv-batch-%02d", i)
		// Every third request is committed only at authority first. The daemon
		// must read it from a lagging cache before choosing the right runtime.
		applyToCache = i%3 != 0
		if err = submit(context.Background(), projectID, "owner", "invocation.request", id,
			model.InvocationRequested{Target: target, Instruction: "Validate repeated delivery", Scopes: []string{"src"}}); err != nil {
			t.Fatalf("request %s: %v", id, err)
		}
		if !applyToCache {
			_, localMeta, cacheErr := cache.State(context.Background(), projectID)
			_, remoteMeta, authorityErr := engine.State(context.Background(), projectID)
			if cacheErr != nil || authorityErr != nil || localMeta.CacheSequence >= remoteMeta.ServerSequence {
				t.Fatalf("expected cache lag for %s: cache=%+v authority=%+v errors=%v/%v", id, localMeta, remoteMeta, cacheErr, authorityErr)
			}
		}
		applyToCache = true
		if err = instance.Sync(context.Background(), projectID); err != nil {
			t.Fatalf("sync %s: %v", id, err)
		}
		raw, readErr := os.ReadFile(outputPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var envelope InvocationEnvelope
		if readErr = json.Unmarshal(raw, &envelope); readErr != nil {
			t.Fatal(readErr)
		}
		if envelope.Invocation.ID != id || envelope.Runtime.ID != runtimeID {
			t.Fatalf("delivery %s reached %+v / %+v", id, envelope.Invocation, envelope.Runtime)
		}
	}
	state, _, err = engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.InvocationDeliveries) != 21 {
		t.Fatalf("delivery projection count=%d, want 21", len(state.InvocationDeliveries))
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("inv-batch-%02d", i)
		if state.Invocations[id].Status != "NOTIFIED" {
			t.Fatalf("invocation %s status=%s, want NOTIFIED", id, state.Invocations[id].Status)
		}
	}
	// Queue a larger burst at authority without pushing those writes into the
	// cache. Concurrent callers request synchronization; all must complete,
	// catch up, and deliver each request once to the correct target runtime.
	// This does not assert that the callers share a single underlying fetch.
	applyToCache = false
	for i := 0; i < 40; i++ {
		target := "claude-builder"
		if i%2 != 0 {
			target = "codex-builder"
		}
		id := fmt.Sprintf("inv-queued-%02d", i)
		if err = submit(context.Background(), projectID, "owner", "invocation.request", id,
			model.InvocationRequested{Target: target, Instruction: "Validate queued delivery", Scopes: []string{"src"}}); err != nil {
			t.Fatalf("queue %s: %v", id, err)
		}
	}
	_, stale, err := cache.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	_, latest, err := engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.CacheSequence >= latest.ServerSequence {
		t.Fatal("queued burst did not create cache lag")
	}
	applyToCache = true
	const syncCallers = 4
	var callers sync.WaitGroup
	results := make(chan error, syncCallers)
	for i := 0; i < syncCallers; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			results <- instance.Sync(context.Background(), projectID)
		}()
	}
	callers.Wait()
	close(results)
	for syncErr := range results {
		if syncErr != nil {
			t.Fatalf("concurrent sync: %v", syncErr)
		}
	}
	state, _, err = engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.InvocationDeliveries) != 61 {
		t.Fatalf("delivery projection count=%d, want 61", len(state.InvocationDeliveries))
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("inv-queued-%02d", i)
		wantRuntime := "runtime-builder"
		if i%2 != 0 {
			wantRuntime = "runtime-second"
		}
		if state.Invocations[id].Status != "NOTIFIED" {
			t.Fatalf("queued invocation %s status=%s, want NOTIFIED", id, state.Invocations[id].Status)
		}
		matches := 0
		for _, delivery := range state.InvocationDeliveries {
			if delivery.InvocationID == id {
				matches++
				if delivery.RuntimeID != wantRuntime {
					t.Fatalf("queued invocation %s delivered to %s", id, delivery.RuntimeID)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("queued invocation %s has %d deliveries, want 1", id, matches)
		}
	}
}
