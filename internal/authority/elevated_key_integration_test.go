package authority

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
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
	must("owner", owner, "agent.activate", "candidate", model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}})

	action := protocol.OrchestratorGrantApprovalAction("candidate")
	must("owner", owner, "approval.request", protocol.OrchestratorGrantApprovalID("candidate"), model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "test"})
	must("owner", owner, "approval.approve", protocol.OrchestratorGrantApprovalID("candidate"), model.ApprovalResponse{})

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

	// The revoke side: candidate is now an orchestrator (granted above), so
	// revoking it is exactly as sensitive as the grant and must hit the same
	// elevated-key requirement -- this is the Postgres-backend sibling of
	// personalauthority's TestRevokeOfOrchestratorRejectsPrimaryKeySignatureOnceElevatedKeyRegistered,
	// exercising scopedElevationState's own "agent.revoke" SQL query, not
	// just the personal-mode in-memory state path.
	if _, _, err = mutate("owner", owner, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected revoking an orchestrator once an elevated key is registered")
	}
	if _, _, err = mutate("owner", elevated, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}

	if _, _, err = mutate("owner", owner, "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected deleting a principal once an elevated key is registered")
	}
	deleted, _, err := mutate("owner", elevated, "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"})
	if err != nil {
		t.Fatalf("expected elevated-key deletion to be accepted: %v", err)
	}
	if deleted.ActorKeyFingerprint != identity.Fingerprint(elevated.PublicKey()) {
		t.Fatalf("delete event fingerprint=%q want elevated key fingerprint", deleted.ActorKeyFingerprint)
	}
	state, _, err := engine.State(context.Background(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Agents["candidate"]; exists {
		t.Fatal("deleted principal remained in the Postgres projection")
	}

	replacement, _ := controlplane.GenerateSigner()
	registered, _, err := mutate("candidate", replacement, "agent.register", "candidate",
		model.AgentRegistered{PublicKey: replacement.PublicKey(), PrincipalType: model.PrincipalAgent, DisplayName: "replacement"})
	if err != nil {
		t.Fatalf("expected deleted ID to be reusable: %v", err)
	}
	if registered.ActorKeyFingerprint != identity.Fingerprint(replacement.PublicKey()) ||
		registered.ActorKeyFingerprint == deleted.ActorKeyFingerprint {
		t.Fatalf("replacement registration did not record a distinct key fingerprint: delete=%q register=%q",
			deleted.ActorKeyFingerprint, registered.ActorKeyFingerprint)
	}
	if verifyErr := engine.VerifyRange(context.Background(), projectID, 1, 0); verifyErr != nil {
		t.Fatalf("verification failed across deletion and ID reuse: %v", verifyErr)
	}
	page, err := engine.Events(context.Background(), projectID,
		controlplane.PageRequest{Limit: controlplane.MaxPageSize})
	if err != nil {
		t.Fatal(err)
	}
	originalFingerprint := identity.Fingerprint(candidate.PublicKey())
	seenOriginal := false
	seenReplacement := false
	for _, record := range page.Items {
		if record.Event.Type != "agent.register" || record.Event.Actor != "candidate" {
			continue
		}
		switch record.Event.ActorKeyFingerprint {
		case originalFingerprint:
			seenOriginal = true
		case registered.ActorKeyFingerprint:
			seenReplacement = true
		}
	}
	if !seenOriginal || !seenReplacement || originalFingerprint == registered.ActorKeyFingerprint {
		t.Fatalf("Postgres history did not retain the identity-reuse key boundary: original=%t replacement=%t",
			seenOriginal, seenReplacement)
	}
}
