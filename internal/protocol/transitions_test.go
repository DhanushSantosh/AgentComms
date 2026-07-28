package protocol

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRequiresElevatedKeyClassifiesOrchestratorGrant(t *testing.T) {
	st := model.State{}
	if RequiresElevatedKey(st, "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator}) != true {
		t.Fatal("expected an ORCHESTRATOR grant to require the elevated key")
	}
	if RequiresElevatedKey(st, "agent.activate", "candidate", model.AgentActivated{Role: model.RoleAgent}) != false {
		t.Fatal("expected a plain AGENT-role activation not to require the elevated key")
	}
	if RequiresElevatedKey(st, "agent.activate", "candidate", "not-a-payload") != false {
		t.Fatal("expected a malformed payload to fail closed to false, not panic")
	}
}

func TestRequiresElevatedKeyClassifiesHumanTierApproval(t *testing.T) {
	st := model.State{Approvals: map[string]model.Approval{
		"approval-1": {ID: "approval-1", Tier: "HUMAN"},
		"approval-2": {ID: "approval-2", Tier: "ORCHESTRATOR"},
	}}
	if RequiresElevatedKey(st, "approval.approve", "approval-1", model.ApprovalResponse{}) != true {
		t.Fatal("expected a HUMAN-tier approval to require the elevated key")
	}
	if RequiresElevatedKey(st, "approval.approve", "approval-2", model.ApprovalResponse{}) != false {
		t.Fatal("expected an ORCHESTRATOR-tier approval not to require the elevated key")
	}
	if RequiresElevatedKey(st, "approval.approve", "unknown-id", model.ApprovalResponse{}) != false {
		t.Fatal("expected a missing approval to fail closed to false, not panic on a nil map read")
	}
}

func TestRequiresElevatedKeyFalseForUnrelatedTransitions(t *testing.T) {
	st := model.State{}
	for _, typ := range []string{"task.create", "message.post", "agent.register", "agent.rotate-key", "agent.elevate-key"} {
		if RequiresElevatedKey(st, typ, "x", nil) {
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
