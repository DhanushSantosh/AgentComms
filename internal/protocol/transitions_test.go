package protocol

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRequiresElevatedKeyClassifiesOrchestratorGrant(t *testing.T) {
	st := model.State{}
	if RequiresElevatedKey(st, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator}) != true {
		t.Fatal("expected an ORCHESTRATOR grant to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleAgent}) != false {
		t.Fatal("expected a plain AGENT-role activation not to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.activate", "candidate", "not-a-payload") != false {
		t.Fatal("expected a malformed payload to fail closed to false, not panic")
	}
}

func TestRequiresElevatedKeyClassifiesHumanTierApproval(t *testing.T) {
	st := model.State{Approvals: map[string]model.Approval{
		"approval-1": {ID: "approval-1", Tier: "HUMAN"},
		"approval-2": {ID: "approval-2", Tier: "ORCHESTRATOR"},
	}}
	if RequiresElevatedKey(st, "owner", "approval.approve", "approval-1", model.ApprovalResponse{}) != true {
		t.Fatal("expected a HUMAN-tier approval to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "approval.approve", "approval-2", model.ApprovalResponse{}) != false {
		t.Fatal("expected an ORCHESTRATOR-tier approval not to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "approval.approve", "unknown-id", model.ApprovalResponse{}) != false {
		t.Fatal("expected a missing approval to fail closed to false, not panic on a nil map read")
	}
}

func TestRequiresElevatedKeyClassifiesRevokeOfOrchestratorOrHuman(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"agent-orch":  agentOrchestrator("agent-orch"),
		"human-agent": {ID: "human-agent", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalHuman},
		"plain-agent": {ID: "plain-agent", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
	}}
	if RequiresElevatedKey(st, "owner", "agent.revoke", "agent-orch", model.RuntimeStatusChanged{}) != true {
		t.Fatal("expected revoking an orchestrator to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.revoke", "human-agent", model.RuntimeStatusChanged{}) != true {
		t.Fatal("expected revoking a human principal to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.revoke", "plain-agent", model.RuntimeStatusChanged{}) != false {
		t.Fatal("expected revoking a plain agent principal not to require the elevated key")
	}
	if RequiresElevatedKey(st, "agent-orch", "agent.revoke", "agent-orch", model.RuntimeStatusChanged{}) != false {
		t.Fatal("expected self-revocation to bypass the elevated-key requirement, mirroring the human-only check's self-bypass")
	}
}

func TestRequiresElevatedKeyFalseForUnrelatedTransitions(t *testing.T) {
	st := model.State{}
	for _, typ := range []string{"task.create", "message.post", "agent.register", "agent.rotate-key", "agent.elevate-key"} {
		if RequiresElevatedKey(st, "owner", typ, "x", nil) {
			t.Fatalf("expected %q not to require the elevated key", typ)
		}
	}
}

func humanAgent(id string) model.Agent {
	return model.Agent{ID: id, Status: "ACTIVE", Role: model.RoleOwner, PrincipalType: model.PrincipalHuman}
}

func TestValidateTransitionElevateKeyRequiresSelf(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"owner": humanAgent("owner")}}
	if _, err := ValidateTransition(st, "owner", "agent.elevate-key", "someone-else",
		model.AgentElevatedKeyRegistered{PublicKey: "pk"}, time.Now()); err == nil {
		t.Fatal("expected registering an elevated key for a different id to be rejected")
	}
}

func TestValidateTransitionElevateKeyRequiresHumanPrincipal(t *testing.T) {
	agentPrincipal := model.Agent{ID: "worker", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent}
	st := model.State{Agents: map[string]model.Agent{"worker": agentPrincipal}}
	if _, err := ValidateTransition(st, "worker", "agent.elevate-key", "worker",
		model.AgentElevatedKeyRegistered{PublicKey: "pk"}, time.Now()); err == nil {
		t.Fatal("expected an AGENT-principal to be rejected registering an elevated key")
	}
}

func TestValidateTransitionElevateKeyRequiresPublicKey(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"owner": humanAgent("owner")}}
	if _, err := ValidateTransition(st, "owner", "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{}, time.Now()); err == nil {
		t.Fatal("expected an empty public key to be rejected")
	}
}

func TestValidateTransitionElevateKeySucceedsForSelfHuman(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"owner": humanAgent("owner")}}
	if _, err := ValidateTransition(st, "owner", "agent.elevate-key", "owner",
		model.AgentElevatedKeyRegistered{PublicKey: "pk"}, time.Now()); err != nil {
		t.Fatalf("expected a human principal registering its own elevated key to succeed: %v", err)
	}
}

// TestValidateTransitionElevateKeyIsNotElevationGated proves agent.elevate-key
// is deliberately absent from elevated() -- an ordinary, non-owner,
// non-orchestrator human principal can still register its own elevated key,
// since this is inherently self-scoped and grants no authority over anyone
// else.
func TestValidateTransitionElevateKeyIsNotElevationGated(t *testing.T) {
	plainHuman := model.Agent{ID: "human-agent", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalHuman}
	st := model.State{Agents: map[string]model.Agent{"human-agent": plainHuman}}
	if _, err := ValidateTransition(st, "human-agent", "agent.elevate-key", "human-agent",
		model.AgentElevatedKeyRegistered{PublicKey: "pk"}, time.Now()); err != nil {
		t.Fatalf("expected a non-elevated human principal to register its own elevated key: %v", err)
	}
}

func agentOrchestrator(id string) model.Agent {
	return model.Agent{ID: id, Status: "ACTIVE", Role: model.RoleOrchestrator, PrincipalType: model.PrincipalAgent}
}

// TestAgentSuspendNeverPermitsOwnerTarget is the suspend-side sibling of the
// existing agent.revoke owner protection: a suspended principal fails
// active() on every subsequent action, including trying to reactivate
// itself, so an unprotected owner-target is a full lockout primitive for a
// project with only one human -- arguably worse than revoke, since revoke
// was already guarded and suspend wasn't.
func TestAgentSuspendNeverPermitsOwnerTarget(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":      humanAgent("owner"),
		"agent-orch": agentOrchestrator("agent-orch"),
	}}
	if _, err := ValidateTransition(st, "agent-orch", "agent.suspend", "owner", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected suspending the owner to be rejected, even by an elevated actor")
	}
}

// TestAgentSuspendOfOrchestratorRequiresHumanActor mirrors
// TestAgentRevokeOfOrchestratorRequiresHumanActor: an AGENT-principal
// orchestrator must not be able to unilaterally suspend a different
// orchestrator or any human principal, only a human actor can.
func TestAgentSuspendOfOrchestratorRequiresHumanActor(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":           humanAgent("owner"),
		"agent-orch":      agentOrchestrator("agent-orch"),
		"other-orch":      agentOrchestrator("other-orch"),
		"human-non-owner": {ID: "human-non-owner", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalHuman},
	}}
	if _, err := ValidateTransition(st, "agent-orch", "agent.suspend", "other-orch", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected an agent-principal orchestrator to be rejected suspending another orchestrator")
	}
	if _, err := ValidateTransition(st, "agent-orch", "agent.suspend", "human-non-owner", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected an agent-principal orchestrator to be rejected suspending a human principal")
	}
	if _, err := ValidateTransition(st, "owner", "agent.suspend", "other-orch", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected a human owner to be permitted suspending an orchestrator: %v", err)
	}
}

// TestAgentSuspendSelfBypassesHumanGate proves self-suspension bypasses the
// human-only gate, mirroring agent.revoke's self-revoke bypass -- an
// AGENT-principal orchestrator can voluntarily suspend itself.
func TestAgentSuspendSelfBypassesHumanGate(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"agent-orch": agentOrchestrator("agent-orch")}}
	if _, err := ValidateTransition(st, "agent-orch", "agent.suspend", "agent-orch", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected self-suspension to bypass the human-only gate: %v", err)
	}
}

// TestAgentSuspendOfPlainAgentNeedsOnlyOrdinaryElevation confirms the new
// protection doesn't overreach: suspending a plain AGENT- or OBSERVER-role
// principal still needs only the ordinary owner-or-orchestrator elevation
// this transition already required.
func TestAgentSuspendOfPlainAgentNeedsOnlyOrdinaryElevation(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"agent-orch": agentOrchestrator("agent-orch"),
		"bystander":  {ID: "bystander", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "agent-orch", "agent.suspend", "bystander", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected an agent-principal orchestrator to suspend a plain agent: %v", err)
	}
}

// TestAgentRotateKeyRejectsCrossActorTarget closes a latent identity-hijack
// primitive: rotating a DIFFERENT principal's key had no consent check at
// all -- the reducer applies the new public key to the target
// unconditionally. Even the project owner (maximal elevation) must not be
// able to do this to someone else; there is no legitimate use case for it
// anywhere in the shipped system, so it's removed outright rather than
// gated.
func TestAgentRotateKeyRejectsCrossActorTarget(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":  humanAgent("owner"),
		"victim": {ID: "victim", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "owner", "agent.rotate-key", "victim",
		model.AgentKeyRotated{PublicKey: "attacker-controlled-key"}, time.Now()); err == nil {
		t.Fatal("expected rotating a different principal's key to be rejected, even by the owner")
	}
}

// TestAgentRotateKeySelfStillWorks confirms the new protection doesn't
// overreach: ordinary self-service key rotation, the only case any shipped
// interface has ever used, is unaffected.
func TestAgentRotateKeySelfStillWorks(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"bystander": {ID: "bystander", Status: "ACTIVE", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "bystander", "agent.rotate-key", "bystander",
		model.AgentKeyRotated{PublicKey: "new-key"}, time.Now()); err != nil {
		t.Fatalf("expected self key rotation to still succeed: %v", err)
	}
}

func validSettings() model.ProjectSettingsUpdated {
	return model.ProjectSettingsUpdated{
		DefaultLease: "90m", StaleGrace: "30m", ActiveRetention: "720h",
		SummaryLimit: 2048, ArtifactLimitBytes: 8 * 1024 * 1024, RequireReview: true,
	}
}

// TestProjectSettingsUpdateRequiresHumanPrincipal closes a gap where an
// AGENT-principal orchestrator could unilaterally weaken project-wide safety
// settings (e.g. disabling RequireReview) -- combined with task.create's
// self-declared, ungated Risk field, enough to let an orchestrator agent
// label its own work ROUTINE and turn off the review requirement that would
// otherwise have caught it.
func TestProjectSettingsUpdateRequiresHumanPrincipal(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":      humanAgent("owner"),
		"agent-orch": agentOrchestrator("agent-orch"),
	}}
	if _, err := ValidateTransition(st, "agent-orch", "project.settings.update", "project", validSettings(), time.Now()); err == nil {
		t.Fatal("expected an agent-principal orchestrator to be rejected updating project settings")
	}
	if _, err := ValidateTransition(st, "owner", "project.settings.update", "project", validSettings(), time.Now()); err != nil {
		t.Fatalf("expected a human owner to update project settings: %v", err)
	}
}

// TestEnvSetAndDeleteRequireElevation closes a real gap this package had
// zero test coverage of at all: env.set/env.delete had no role gate
// whatsoever, so even an OBSERVER-role principal (intended read-only) could
// write or delete arbitrary key/value data into the shared, append-only
// signed log, with no way to truly remove it afterward (only hide it from
// current projected state).
func TestEnvSetAndDeleteRequireElevation(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":    humanAgent("owner"),
		"observer": {ID: "observer", Status: "ACTIVE", Role: model.RoleObserver, PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "observer", "env.set", "key1", model.EnvSetPayload{Key: "key1", Value: "v"}, time.Now()); err == nil {
		t.Fatal("expected an OBSERVER-role principal to be rejected setting an env value")
	}
	if _, err := ValidateTransition(st, "owner", "env.set", "key1", model.EnvSetPayload{Key: "key1", Value: "v"}, time.Now()); err != nil {
		t.Fatalf("expected an owner to set an env value: %v", err)
	}
	if _, err := ValidateTransition(st, "observer", "env.delete", "key1", model.EnvDeletePayload{Key: "key1"}, time.Now()); err == nil {
		t.Fatal("expected an OBSERVER-role principal to be rejected deleting an env value")
	}
	if _, err := ValidateTransition(st, "owner", "env.delete", "key1", model.EnvDeletePayload{Key: "key1"}, time.Now()); err != nil {
		t.Fatalf("expected an owner to delete an env value: %v", err)
	}
}

// TestEnvSetRejectsEmptyKey confirms the pre-existing validation (not new)
// still runs after the new elevation check.
func TestEnvSetRejectsEmptyKey(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"owner": humanAgent("owner")}}
	if _, err := ValidateTransition(st, "owner", "env.set", "", model.EnvSetPayload{Key: "", Value: "v"}, time.Now()); err == nil {
		t.Fatal("expected an empty key to be rejected")
	}
}
