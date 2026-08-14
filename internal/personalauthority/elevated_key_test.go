package personalauthority

import (
	"context"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/google/uuid"
)

// elevatedKeyFixture bootstraps a project with a single HUMAN owner and one
// pending "candidate" agent, wired up so the caller can drive
// agent.activate(ORCHESTRATOR) / approval.approve(HUMAN) scenarios directly
// against the engine -- this is the layer that actually verifies
// signatures, which is what the elevated-key feature is meant to gate.
type elevatedKeyFixture struct {
	t         *testing.T
	engine    *Engine
	projectID string
	owner     *controlplane.Signer
	candidate *controlplane.Signer
}

func newElevatedKeyFixture(t *testing.T) *elevatedKeyFixture {
	t.Helper()
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(t.TempDir()+"/authority.db", serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	const projectID = "personal-project"
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	f := &elevatedKeyFixture{t: t, engine: engine, projectID: projectID}
	f.owner, _ = controlplane.GenerateSigner()
	f.candidate, _ = controlplane.GenerateSigner()
	f.register("owner", f.owner, model.PrincipalHuman)
	f.mustMutate("owner", f.owner, "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})
	f.register("candidate", f.candidate, model.PrincipalAgent)
	f.mustMutate("owner", f.owner, "agent.activate", "candidate",
		model.AgentActivated{Role: model.Role("MEMBER"), Scopes: []string{"src"}})
	return f
}

func (f *elevatedKeyFixture) mutate(actor string, signer *controlplane.Signer, eventType, entityID string, payload any) (controlplane.Event, controlplane.Receipt, error) {
	f.t.Helper()
	raw, err := model.EncodePayload(eventType, payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	command := controlplane.Command{
		ProjectID: f.projectID, Actor: actor, Type: eventType, EntityID: entityID,
		Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
	}
	if eventType == "agent.register" {
		command.PublicKey = signer.PublicKey()
	}
	if err = command.Sign(signer.PrivateKey()); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	return f.engine.Mutate(context.Background(), command)
}

func (f *elevatedKeyFixture) mustMutate(actor string, signer *controlplane.Signer, eventType, entityID string, payload any) controlplane.Event {
	f.t.Helper()
	event, _, err := f.mutate(actor, signer, eventType, entityID, payload)
	if err != nil {
		f.t.Fatalf("%s: %v", eventType, err)
	}
	return event
}

func (f *elevatedKeyFixture) register(id string, signer *controlplane.Signer, principal model.PrincipalType) {
	f.t.Helper()
	f.mustMutate(id, signer, "agent.register", id, model.AgentRegistered{
		PublicKey: signer.PublicKey(), PrincipalType: principal, DisplayName: id,
	})
}

// grantOrchestratorApproval drives the pre-existing two-step apply/approve
// gate (internal/protocol.OrchestratorGrantApprovalAction) that must already
// be satisfied before agent.activate(ORCHESTRATOR) is even attempted -- this
// fixture tests the elevated-key layer specifically, not that gate, so it
// clears it up front with the owner's primary key (approval.approve itself
// isn't classified as needing the elevated key unless its own Tier is
// HUMAN, which OrchestratorGrantApprovalAction's approvals always are).
func (f *elevatedKeyFixture) grantOrchestratorApproval(target string) {
	f.t.Helper()
	action := protocol.OrchestratorGrantApprovalAction(target)
	approvalID := target + "-approval"
	f.mustMutate("owner", f.owner, "approval.request", approvalID,
		model.ApprovalRequested{Tier: "HUMAN", Action: action, Reason: "test"})
	f.mustMutate("owner", f.owner, "approval.approve", approvalID, model.ApprovalResponse{})
}

func TestOrchestratorGrantFallsBackToPrimaryKeyWhenNoElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	f.grantOrchestratorApproval("candidate")
	if _, _, err := f.mutate("owner", f.owner, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err != nil {
		t.Fatalf("expected the grant to succeed against the primary key when no elevated key is registered: %v", err)
	}
}

func TestOrchestratorGrantRejectsPrimaryKeySignatureOnceElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	f.grantOrchestratorApproval("candidate")
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})

	// This is the regression test that matters: once an elevated key is on
	// record, a command signed with the everyday primary key must be
	// rejected outright as an integrity failure, not silently accepted.
	if _, _, err = f.mutate("owner", f.owner, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected once an elevated key is registered")
	}

	if _, _, err = f.mutate("owner", elevated, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}
	state, _, err := f.engine.State(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to end up ORCHESTRATOR: %+v", state.Agents["candidate"])
	}
}

func TestHumanTierApprovalRejectsPrimaryKeySignatureOnceElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	f.mustMutate("owner", f.owner, "approval.request", "release-approval",
		model.ApprovalRequested{Tier: "HUMAN", Action: "release.publish", Reason: "test"})

	if _, _, err = f.mutate("owner", f.owner, "approval.approve", "release-approval", model.ApprovalResponse{}); err == nil {
		t.Fatal("expected a primary-key signature on a HUMAN-tier approval to be rejected once an elevated key is registered")
	}
	if _, _, err = f.mutate("owner", elevated, "approval.approve", "release-approval", model.ApprovalResponse{}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}
}

func TestOrchestratorTierApprovalStillUsesPrimaryKeyEvenWithElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	f.mustMutate("owner", f.owner, "approval.request", "routine-approval",
		model.ApprovalRequested{Tier: "ORCHESTRATOR", Action: "task.takeover:t-1", Reason: "test"})

	// An ORCHESTRATOR-tier (not HUMAN) approval was never classified as
	// needing the elevated key -- confirms the elevated key doesn't leak
	// into unrelated approvals just because one is registered.
	if _, _, err = f.mutate("owner", f.owner, "approval.approve", "routine-approval", model.ApprovalResponse{}); err != nil {
		t.Fatalf("expected an ORCHESTRATOR-tier approval to still verify against the primary key: %v", err)
	}
}

// TestRevokeOfOrchestratorRejectsPrimaryKeySignatureOnceElevatedKeyRegistered
// closes the same class of gap the orchestrator-grant tests above cover, but
// for the revoke side: an agent.revoke targeting an ORCHESTRATOR or HUMAN
// principal is exactly as sensitive as granting the role in the first
// place, and had the identical credential-only weakness until now.
func TestRevokeOfOrchestratorRejectsPrimaryKeySignatureOnceElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	f.grantOrchestratorApproval("candidate")
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	f.mustMutate("owner", elevated, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	if _, _, err = f.mutate("owner", f.owner, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected revoking an orchestrator once an elevated key is registered")
	}
	if _, _, err = f.mutate("owner", elevated, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected the elevated-key signature to be accepted: %v", err)
	}
	state, _, err := f.engine.State(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["candidate"].Status != "REVOKED" {
		t.Fatalf("expected candidate to be revoked: %+v", state.Agents["candidate"])
	}
}

// TestRevokeOfPlainAgentStillUsesPrimaryKeyEvenWithElevatedKeyRegistered
// confirms the new protection doesn't overreach: revoking a plain
// AGENT-principal, non-orchestrator target is unaffected.
func TestRevokeOfPlainAgentStillUsesPrimaryKeyEvenWithElevatedKeyRegistered(t *testing.T) {
	f := newElevatedKeyFixture(t)
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	if _, _, err = f.mutate("owner", f.owner, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected revoking a plain agent to still verify against the primary key: %v", err)
	}
}

// TestSelfRevokeOfOrchestratorBypassesElevatedKeyRequirement mirrors the
// existing human-only check's self-revoke bypass: an AGENT-principal
// orchestrator can still voluntarily revoke itself without an elevated key,
// since self-revocation is not an escalation.
func TestSelfRevokeOfOrchestratorBypassesElevatedKeyRequirement(t *testing.T) {
	f := newElevatedKeyFixture(t)
	f.grantOrchestratorApproval("candidate")
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	f.mustMutate("owner", elevated, "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOrchestrator, Scopes: []string{"src"}})

	if _, _, err = f.mutate("candidate", f.candidate, "agent.revoke", "candidate", model.RuntimeStatusChanged{}); err != nil {
		t.Fatalf("expected self-revocation to bypass the elevated-key requirement: %v", err)
	}
}

func TestDeleteRequiresElevatedKeyAndAllowsIdentityReuse(t *testing.T) {
	f := newElevatedKeyFixture(t)
	elevated, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	f.mustMutate("owner", f.owner, "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: elevated.PublicKey()})
	f.mustMutate("owner", f.owner, "agent.revoke", "candidate",
		model.RuntimeStatusChanged{Reason: "retired"})

	if _, _, err = f.mutate("owner", f.owner, "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"}); err == nil {
		t.Fatal("expected a primary-key signature to be rejected deleting a principal once an elevated key is registered")
	}
	deleteEvent, _, err := f.mutate("owner", elevated, "agent.delete", "candidate",
		model.AgentDeleted{Reason: "remove retired identity"})
	if err != nil {
		t.Fatalf("expected elevated-key deletion to succeed: %v", err)
	}
	if deleteEvent.ActorKeyFingerprint != identity.Fingerprint(elevated.PublicKey()) {
		t.Fatalf("delete event fingerprint=%q want elevated key fingerprint", deleteEvent.ActorKeyFingerprint)
	}
	state, _, err := f.engine.State(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := state.Agents["candidate"]; exists {
		t.Fatal("deleted principal remained in the current projection")
	}

	replacement, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	registerEvent := f.mustMutate("candidate", replacement, "agent.register", "candidate",
		model.AgentRegistered{PublicKey: replacement.PublicKey(), PrincipalType: model.PrincipalAgent, DisplayName: "replacement"})
	if registerEvent.ActorKeyFingerprint != identity.Fingerprint(replacement.PublicKey()) {
		t.Fatalf("replacement registration fingerprint=%q want replacement key fingerprint", registerEvent.ActorKeyFingerprint)
	}
	if registerEvent.ActorKeyFingerprint == deleteEvent.ActorKeyFingerprint {
		t.Fatal("delete and replacement registration did not preserve distinct signing-key fingerprints")
	}
	page, err := f.engine.Events(context.Background(), f.projectID, controlplane.PageRequest{Limit: controlplane.MaxPageSize})
	if err != nil {
		t.Fatal(err)
	}
	seenDelete := false
	seenReplacement := false
	seenOriginal := false
	originalFingerprint := identity.Fingerprint(f.candidate.PublicKey())
	for _, record := range page.Items {
		if record.Event.Type == "agent.register" && record.Event.Actor == "candidate" &&
			record.Event.ActorKeyFingerprint == originalFingerprint {
			seenOriginal = true
		}
		switch record.Event.ID {
		case deleteEvent.ID:
			seenDelete = record.Event.ActorKeyFingerprint == deleteEvent.ActorKeyFingerprint
		case registerEvent.ID:
			seenReplacement = record.Event.ActorKeyFingerprint == registerEvent.ActorKeyFingerprint
		}
	}
	if !seenDelete || !seenOriginal || !seenReplacement || originalFingerprint == registerEvent.ActorKeyFingerprint {
		t.Fatalf("history did not retain the identity-reuse key boundary: delete=%t original=%t replacement=%t",
			seenDelete, seenOriginal, seenReplacement)
	}
}
