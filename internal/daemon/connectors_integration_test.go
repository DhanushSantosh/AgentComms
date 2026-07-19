package daemon

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	client, err := remote.New(server.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	projectID := "connector-" + uuid.NewString()
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	builder, _ := controlplane.GenerateSigner()
	signers := map[string]*controlplane.Signer{"owner": owner, "builder": builder}
	cache, err := localcache.Open(filepath.Join(t.TempDir(), "cache.db"), serviceSigner.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
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
		return cache.Apply(ctx, event, receipt)
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
		{"builder", "agent.register", "builder", model.AgentRegistered{
			PublicKey: builder.PublicKey(), PrincipalType: model.PrincipalAgent,
		}},
		{"owner", "agent.activate", "builder", model.AgentActivated{
			Role: model.RoleAgent, Scopes: []string{"src"},
		}},
		{"builder", "runtime.register", "runtime-builder", model.RuntimeRegistered{
			AgentID: "builder", Connector: "LOCAL_PROCESS", ConfigReference: "builder-local", MaxConcurrent: 1,
		}},
		{"owner", "invocation.request", "inv-deliver", model.InvocationRequested{
			Target: "builder", Instruction: "Run connector integration", Scopes: []string{"src"},
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
}
