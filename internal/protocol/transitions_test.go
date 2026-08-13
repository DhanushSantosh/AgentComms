package protocol

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRequiresElevatedKeyClassifiesOrchestratorGrant(t *testing.T) {
	st := model.State{}
	if RequiresElevatedKey(st, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.RoleOrchestrator}) != true {
		t.Fatal("expected an ORCHESTRATOR grant to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.activate", "candidate", model.AgentActivated{Role: model.Role("MEMBER")}) != false {
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
		"human-agent": {ID: "human-agent", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalHuman},
		"plain-agent": {ID: "plain-agent", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
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

func TestRequiresElevatedKeyClassifiesAgentDeletion(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"revoked": {ID: "revoked", Status: "REVOKED", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		"active":  {ID: "active", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
	}}
	if !RequiresElevatedKey(st, "owner", "agent.delete", "revoked", model.AgentDeleted{Reason: "cleanup"}) {
		t.Fatal("expected deletion of a revoked principal to require the elevated key")
	}
	if RequiresElevatedKey(st, "owner", "agent.delete", "active", model.AgentDeleted{Reason: "cleanup"}) {
		t.Fatal("expected an invalid active-target deletion not to be classified as an elevated transition")
	}
}

func TestAgentDeleteRequiresRevokedTargetAndHumanActor(t *testing.T) {
	humanOwner := humanAgent("owner")
	agentLead := agentOrchestrator("agent-lead")
	activeTarget := model.Agent{ID: "target", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent}
	st := model.State{Agents: map[string]model.Agent{
		"owner": humanOwner, "agent-lead": agentLead, "target": activeTarget,
	}}
	if _, err := ValidateTransition(st, "owner", "agent.delete", "target",
		model.AgentDeleted{Reason: "cleanup"}, time.Now()); err == nil {
		t.Fatal("expected deletion of an active principal to be rejected")
	}
	pendingTarget := activeTarget
	pendingTarget.Status = "PENDING"
	st.Agents["target"] = pendingTarget
	if _, err := ValidateTransition(st, "owner", "agent.delete", "target",
		model.AgentDeleted{Reason: "cleanup"}, time.Now()); err == nil {
		t.Fatal("expected deletion of a pending principal to be rejected")
	}
	revokedTarget := activeTarget
	revokedTarget.Status = "REVOKED"
	st.Agents["target"] = revokedTarget
	if _, err := ValidateTransition(st, "agent-lead", "agent.delete", "target",
		model.AgentDeleted{Reason: "cleanup"}, time.Now()); err == nil {
		t.Fatal("expected an agent principal to be rejected deleting a principal")
	}
	if _, err := ValidateTransition(st, "owner", "agent.delete", "target",
		model.AgentDeleted{}, time.Now()); err == nil {
		t.Fatal("expected deletion without an audit reason to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "agent.delete", "target",
		model.AgentDeleted{Reason: "cleanup"}, time.Now()); err != nil {
		t.Fatalf("expected a human owner to delete a revoked principal: %v", err)
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
	agentPrincipal := model.Agent{ID: "worker", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent}
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
	plainHuman := model.Agent{ID: "human-agent", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalHuman}
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
// TestElevationRejectionNamesTheResolvedActorAndItsRole is the regression
// test for a real, confirmed live gap: "owner or orchestrator role
// required" gave no way to tell whether the actor genuinely lacked
// standing or (just as common in practice) simply resolved to the wrong
// actor in the first place -- a stale `profile use`, a leftover env var,
// or picking the wrong identity from the TUI's actor switcher. The
// rejection looked identical either way. Naming the actor and its actual
// role turns that into a one-line diagnosis.
func TestElevationRejectionNamesTheResolvedActorAndItsRole(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"plain-agent": {ID: "plain-agent", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
	}}
	_, err := ValidateTransition(st, "plain-agent", "agent.activate", "target",
		model.AgentActivated{Role: model.Role("MEMBER")}, time.Now())
	if err == nil {
		t.Fatal("expected a non-elevated actor to be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "plain-agent") {
		t.Fatalf("error %q does not name the actor that was actually resolved", msg)
	}
	if !strings.Contains(msg, "role is member") {
		t.Fatalf("error %q does not name the actor's actual current role", msg)
	}
}

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
		"human-non-owner": {ID: "human-non-owner", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalHuman},
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
		"bystander":  {ID: "bystander", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
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
		"victim": {ID: "victim", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
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
		"bystander": {ID: "bystander", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "bystander", "agent.rotate-key", "bystander",
		model.AgentKeyRotated{PublicKey: "new-key"}, time.Now()); err != nil {
		t.Fatalf("expected self key rotation to still succeed: %v", err)
	}
}

func TestInvocationRequestNormalizesConsumerRoutingFromPolicy(t *testing.T) {
	state := model.State{
		Agents: map[string]model.Agent{
			"owner":   humanAgent("owner"),
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		AgentRuntimes: map[string]model.AgentRuntime{
			"builder-interactive": {
				ID: "builder-interactive", AgentID: "builder",
				Kind: model.RuntimeKindInteractive, Connector: "INTERACTIVE",
				HostID: "host", Status: "OFFLINE", MaxConcurrent: 1,
			},
		},
		InvocationPolicies: map[string]model.InvocationPolicy{
			"builder": {
				AgentID: "builder", Mode: "AUTOMATIC",
				DefaultConsumerMode:           model.ConsumerModeInteractiveOnly,
				AllowedConsumerModes:          []model.ConsumerMode{model.ConsumerModeInteractiveOnly},
				PreferredInteractiveRuntimeID: "builder-interactive",
			},
		},
	}
	normalized, err := ValidateTransition(state, "owner", "invocation.request", "invocation",
		model.InvocationRequested{Target: "builder", Instruction: "Review the change"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	request := normalized.(model.InvocationRequested)
	if request.ConsumerMode != model.ConsumerModeInteractiveOnly ||
		request.PreferredRuntimeID != "builder-interactive" {
		t.Fatalf("unexpected normalized routing: %+v", request)
	}
}

func TestInvocationClaimEnforcesKindPreferredRuntimeAndCapacity(t *testing.T) {
	now := time.Now().UTC()
	state := model.State{
		Agents: map[string]model.Agent{
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		Invocations: map[string]model.Invocation{
			"invocation": {
				ID: "invocation", Target: "builder", RequestedBy: "owner", Status: "PENDING",
				ConsumerMode: model.ConsumerModeInteractiveOnly, PreferredRuntimeID: "interactive",
			},
		},
		AgentRuntimes: map[string]model.AgentRuntime{
			"worker": {
				ID: "worker", AgentID: "builder", Kind: model.RuntimeKindWorker,
				Connector: "MCP", Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
			},
			"other-interactive": {
				ID: "other-interactive", AgentID: "builder", Kind: model.RuntimeKindInteractive,
				Connector: "INTERACTIVE", HostID: "host", EndpointID: "other-endpoint",
				Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
			},
			"interactive": {
				ID: "interactive", AgentID: "builder", Kind: model.RuntimeKindInteractive,
				Connector: "INTERACTIVE", HostID: "host", EndpointID: "endpoint",
				Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
			},
		},
	}
	for _, runtimeID := range []string{"worker", "other-interactive"} {
		if _, err := ValidateTransition(state, "builder", "invocation.claim", "invocation",
			model.InvocationClaimed{RuntimeID: runtimeID}, now); err == nil {
			t.Fatalf("ineligible runtime %s claimed the invocation", runtimeID)
		}
	}
	if _, err := ValidateTransition(state, "builder", "invocation.claim", "invocation",
		model.InvocationClaimed{RuntimeID: "interactive"}, now); err != nil {
		t.Fatalf("preferred interactive runtime could not claim: %v", err)
	}
	state.Invocations["active"] = model.Invocation{
		ID: "active", Target: "builder", RuntimeID: "interactive", Status: "RUNNING",
	}
	if _, err := ValidateTransition(state, "builder", "invocation.claim", "invocation",
		model.InvocationClaimed{RuntimeID: "interactive"}, now); err == nil {
		t.Fatal("capacity-exhausted runtime claimed another invocation")
	}
}

func TestDeliveryAttemptAndEvidenceAreStrictlyBound(t *testing.T) {
	now := time.Now().UTC()
	attemptedAt := now.Add(-time.Second)
	attemptUntil := now.Add(time.Minute)
	state := model.State{
		Agents: map[string]model.Agent{
			"owner":   humanAgent("owner"),
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		Invocations: map[string]model.Invocation{
			"invocation": {
				ID: "invocation", Target: "builder", RequestedBy: "owner",
				Status: "PENDING", ConsumerMode: model.ConsumerModeInteractiveOnly,
			},
		},
		AgentRuntimes: map[string]model.AgentRuntime{
			"interactive": {
				ID: "interactive", AgentID: "builder", Kind: model.RuntimeKindInteractive,
				Connector: "INTERACTIVE", HostID: "host", EndpointID: "endpoint",
				Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
			},
		},
		InvocationDeliveries: map[string]model.InvocationDelivery{
			"active-attempt": {
				ID: "active-attempt", InvocationID: "invocation", RuntimeID: "interactive",
				Transport: "INTERACTIVE", HostID: "host", EndpointID: "endpoint",
				Attempt: 1, Status: "ATTEMPTED",
				AttemptedAt: &attemptedAt, AttemptUntil: &attemptUntil,
			},
		},
	}
	if _, err := ValidateTransition(state, "owner", "invocation.delivery-attempt", "invocation",
		model.InvocationDeliveryAttempted{
			DeliveryID: "duplicate", RuntimeID: "interactive", Transport: "INTERACTIVE",
		}, now); err == nil {
		t.Fatal("second unexpired attempt for the same invocation/runtime was accepted")
	}
	if _, err := ValidateTransition(state, "owner", "invocation.notify", "invocation",
		model.InvocationNotified{DeliveryID: "active-attempt"}, now); err == nil {
		t.Fatal("interactive success without PTY evidence was accepted")
	}
	normalized, err := ValidateTransition(state, "owner", "invocation.notify", "invocation",
		model.InvocationNotified{
			DeliveryID: "active-attempt", EndpointID: "endpoint",
			Evidence: []model.DeliveryEvidence{
				{Stage: "PTY_TEXT_ECHOED", At: attemptedAt.Add(time.Millisecond)},
				{Stage: "PTY_ENTER_SENT", At: attemptedAt.Add(2 * time.Millisecond)},
			},
		}, now)
	if err != nil {
		t.Fatal(err)
	}
	notification := normalized.(model.InvocationNotified)
	if notification.RuntimeID != "interactive" || notification.Transport != "INTERACTIVE" ||
		notification.Attempt != 1 {
		t.Fatalf("notification was not bound to the reserved attempt: %+v", notification)
	}
}

func TestRuntimeConfigureRequiresInactiveOfflineRuntime(t *testing.T) {
	now := time.Now().UTC()
	state := model.State{
		Agents: map[string]model.Agent{
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		AgentRuntimes: map[string]model.AgentRuntime{
			"runtime": {
				ID: "runtime", AgentID: "builder", Kind: model.RuntimeKindWorker,
				Connector: "MCP", Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
			},
		},
		Invocations: map[string]model.Invocation{},
	}
	configure := model.RuntimeConfigured{
		Kind: model.RuntimeKindWorker, Connector: "MCP", MaxConcurrent: 1,
	}
	if _, err := ValidateTransition(state, "builder", "runtime.configure", "runtime", configure, now); err == nil {
		t.Fatal("online runtime was reconfigured")
	}
	runtimeState := state.AgentRuntimes["runtime"]
	runtimeState.Status = "OFFLINE"
	runtimeState.Health = "UNKNOWN"
	state.AgentRuntimes["runtime"] = runtimeState
	state.Invocations["active"] = model.Invocation{
		ID: "active", Target: "builder", RuntimeID: "runtime", Status: "WAITING",
	}
	if _, err := ValidateTransition(state, "builder", "runtime.configure", "runtime", configure, now); err == nil {
		t.Fatal("runtime with an authoritative active assignment was reconfigured")
	}
	delete(state.Invocations, "active")
	if _, err := ValidateTransition(state, "builder", "runtime.configure", "runtime", configure, now); err != nil {
		t.Fatalf("inactive offline runtime could not be repaired: %v", err)
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
// whatsoever, so any non-owner/non-orchestrator principal could write or
// delete arbitrary key/value data into the shared, append-only signed log,
// with no way to truly remove it afterward (only hide it from current
// projected state).
func TestEnvSetAndDeleteRequireElevation(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":  humanAgent("owner"),
		"member": {ID: "member", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "member", "env.set", "key1", model.EnvSetPayload{Key: "key1", Value: "v"}, time.Now()); err == nil {
		t.Fatal("expected a non-elevated principal to be rejected setting an env value")
	}
	if _, err := ValidateTransition(st, "owner", "env.set", "key1", model.EnvSetPayload{Key: "key1", Value: "v"}, time.Now()); err != nil {
		t.Fatalf("expected an owner to set an env value: %v", err)
	}
	if _, err := ValidateTransition(st, "member", "env.delete", "key1", model.EnvDeletePayload{Key: "key1"}, time.Now()); err == nil {
		t.Fatal("expected a non-elevated principal to be rejected deleting an env value")
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

// -- agent.switch-role (RFC 0018) ------------------------------------------

func switchRoleState(extra ...func(*model.State)) model.State {
	st := model.State{Agents: map[string]model.Agent{
		"owner":        humanAgent("owner"),
		"human-member": {ID: "human-member", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalHuman},
		"agent-member": {ID: "agent-member", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
	}}
	for _, f := range extra {
		f(&st)
	}
	return st
}

func TestSwitchRoleIsSelfOnly(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "human-member",
		model.AgentRoleSwitched{Role: model.Role("Tester")}, time.Now()); err == nil {
		t.Fatal("expected switching a DIFFERENT principal's role to be rejected")
	}
}

func TestSwitchRoleNeverTargetsOwner(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.RoleOwner}, time.Now()); err == nil {
		t.Fatal("expected switching to OWNER to be rejected")
	}
	// Case-insensitive too -- normalizeRole canonicalizes before the check.
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.Role("owner")}, time.Now()); err == nil {
		t.Fatal("expected switching to owner (lowercase) to be rejected too")
	}
}

func TestSwitchRoleRejectsWhenCurrentRoleIsOwner(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "owner", "agent.switch-role", "owner",
		model.AgentRoleSwitched{Role: model.Role("Tester")}, time.Now()); err == nil {
		t.Fatal("expected the owner principal to be rejected switching its own role, even to something harmless")
	}
}

func TestSwitchRoleToCustomLabelNeedsNoElevationAndPreservesCasing(t *testing.T) {
	st := switchRoleState()
	payload, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.Role("Frontend-Architect")}, time.Now())
	if err != nil {
		t.Fatalf("expected a non-elevated principal to switch its own role freely: %v", err)
	}
	switched, ok := payload.(model.AgentRoleSwitched)
	if !ok || switched.Role != "Frontend-Architect" {
		t.Fatalf("expected the custom label's casing to be preserved exactly, got %#v", payload)
	}
}

func TestSwitchRoleRejectsEmptyOrOverlongRole(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.Role("   ")}, time.Now()); err == nil {
		t.Fatal("expected a whitespace-only role to be rejected")
	}
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.Role(strings.Repeat("x", 65))}, time.Now()); err == nil {
		t.Fatal("expected a role over 64 characters to be rejected")
	}
}

func TestSwitchRoleToOrchestratorRequiresHumanPrincipal(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "agent-member", "agent.switch-role", "agent-member",
		model.AgentRoleSwitched{Role: model.RoleOrchestrator}, time.Now()); err == nil {
		t.Fatal("expected an AGENT principal to be rejected self-switching to orchestrator")
	}
}

func TestSwitchRoleToOrchestratorRequiresApproval(t *testing.T) {
	st := switchRoleState()
	if _, err := ValidateTransition(st, "human-member", "agent.switch-role", "human-member",
		model.AgentRoleSwitched{Role: model.RoleOrchestrator}, time.Now()); err == nil {
		t.Fatal("expected switching to orchestrator without a prior HUMAN-tier approval to be rejected")
	}
	approved := switchRoleState(func(s *model.State) {
		s.Approvals = map[string]model.Approval{
			"approval-1": {
				Action: OrchestratorGrantApprovalAction("human-member"), Status: "APPROVED", Tier: "HUMAN",
			},
		}
	})
	if _, err := ValidateTransition(approved, "human-member", "agent.switch-role", "human-member",
		model.AgentRoleSwitched{Role: model.RoleOrchestrator}, time.Now()); err != nil {
		t.Fatalf("expected switching to orchestrator with an approved HUMAN-tier approval to succeed: %v", err)
	}
}

func TestRequiresElevatedKeyClassifiesSwitchRoleToOrchestrator(t *testing.T) {
	st := model.State{}
	if RequiresElevatedKey(st, "human-member", "agent.switch-role", "human-member", model.AgentRoleSwitched{Role: model.RoleOrchestrator}) != true {
		t.Fatal("expected a self-switch to ORCHESTRATOR to require the elevated key")
	}
	if RequiresElevatedKey(st, "agent-member", "agent.switch-role", "agent-member", model.AgentRoleSwitched{Role: model.Role("Tester")}) != false {
		t.Fatal("expected a self-switch to a custom label not to require the elevated key")
	}
	if RequiresElevatedKey(st, "agent-member", "agent.switch-role", "agent-member", "not-a-payload") != false {
		t.Fatal("expected a malformed payload to fail closed to false, not panic")
	}
}

// TestAgentActivateNeverGrantsOwnerOutsideBootstrap closes the gap RFC 0018
// identified: the general agent.activate path (this validator) previously
// accepted RoleOwner for any target, silently -- unintended, since OWNER
// is meant to be reachable only through the one special bootstrap event
// (sequence == 1), handled entirely outside this validator by
// internal/personalauthority/engine.go and internal/authority/postgres.go.
func TestAgentActivateNeverGrantsOwnerOutsideBootstrap(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":     humanAgent("owner"),
		"candidate": {ID: "candidate", Status: "PENDING", PrincipalType: model.PrincipalAgent},
	}}
	if _, err := ValidateTransition(st, "owner", "agent.activate", "candidate",
		model.AgentActivated{Role: model.RoleOwner}, time.Now()); err == nil {
		t.Fatal("expected agent.activate to reject OWNER as a target outside the bootstrap event")
	}
}
