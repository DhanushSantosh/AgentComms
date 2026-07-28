package authority

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/google/uuid"
)

// TestPostgresOrchestratorGrantRejectsPrimaryKeySignatureOnceElevatedKeyRegistered
// is the Postgres-backend sibling of the equivalent personalauthority test:
// commandPublicKey's elevated-key branch and its scopedApprovalState helper
// are backend-specific code (a real SQL query, not the in-memory state
// already loaded for personalauthority), so this needs its own live
// verification rather than trusting parity by inspection.
func TestPostgresOrchestratorGrantRejectsPrimaryKeySignatureOnceElevatedKeyRegistered(t *testing.T) {
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
	projectID := "elevated-key-" + uuid.NewString()
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	candidate, _ := controlplane.GenerateSigner()
	elevated, _ := controlplane.GenerateSigner()

	mutate := func(actor string, signer *controlplane.Signer, typ, entity string, payload any) (controlplane.Event, controlplane.Receipt, error) {
		raw, encodeErr := model.EncodePayload(typ, payload)
		if encodeErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, encodeErr
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: typ, EntityID: entity,
			Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if typ == "agent.register" {
			command.PublicKey = signer.PublicKey()
		}
		if signErr := command.Sign(signer.PrivateKey()); signErr != nil {
			return controlplane.Event{}, controlplane.Receipt{}, signErr
		}
		return engine.Mutate(context.Background(), command)
	}
	must := func(actor string, signer *controlplane.Signer, typ, entity string, payload any) {
		t.Helper()
		if _, _, mutateErr := mutate(actor, signer, typ, entity, payload); mutateErr != nil {
			t.Fatalf("%s: %v", typ, mutateErr)
		}
	}
	register := func(id string, signer *controlplane.Signer, principal model.PrincipalType) {
		t.Helper()
		must(id, signer, "agent.register", id, model.AgentRegistered{
			PublicKey: signer.PublicKey(), PrincipalType: principal, DisplayName: id,
		})
	}

	register("owner", owner, model.PrincipalHuman)
	must("owner", owner, "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})
	register("candidate", candidate, model.PrincipalAgent)
	must("owner", owner, "agent.activate", "candidate", model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}})

	action := protocol.OrchestratorGrantApprovalAction("candidate")
	must("owner", owner, "approval.request", "candidate-approval", model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "test"})
	must("owner", owner, "approval.approve", "candidate-approval", model.ApprovalResponse{})

	must("owner", owner, "agent.elevate-key", "owner", model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})

	if _, _, err = mutate("owner", owner, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected once an elevated key is registered")
	}
	if _, _, err = mutate("owner", elevated, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}

	must("owner", owner, "approval.request", "release-approval", model.ApprovalRequested{Tier: "HUMAN", Action: "release.publish", Reason: "test"})
	if _, _, err = mutate("owner", owner, "approval.approve", "release-approval", model.ApprovalResponse{}); err == nil {
		t.Fatal("expected a primary-key signature on a HUMAN-tier approval to be rejected once an elevated key is registered")
	}
	if _, _, err = mutate("owner", elevated, "approval.approve", "release-approval", model.ApprovalResponse{}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}
}
