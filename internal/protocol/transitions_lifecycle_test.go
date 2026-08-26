package protocol

import (
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// This file closes the direct-unit-test gap docs/backlog.md names under "Test
// / CI infrastructure": ValidateTransition's task, message, approval,
// decision, document, runtime, and invocation-policy branches previously had
// only indirect coverage through internal/service/internal/app/internal/mcp/
// internal/tui integration tests. It complements transitions_test.go (which
// already covers the elevated-key/orchestrator-grant paths) rather than
// duplicating it.

// -- pure helpers ------------------------------------------------------

func TestOverlapMatchesExactAndPrefixedResources(t *testing.T) {
	cases := []struct {
		name     string
		a, b     []string
		expected bool
	}{
		{"exact match", []string{"repo/main"}, []string{"repo/main"}, true},
		{"wildcard on either side", []string{"*"}, []string{"repo/main"}, true},
		{"a prefixes b", []string{"repo"}, []string{"repo/sub"}, true},
		{"b prefixes a", []string{"repo/sub"}, []string{"repo"}, true},
		{"disjoint", []string{"repo/one"}, []string{"repo/two"}, false},
		{"empty sets", nil, []string{"repo"}, false},
	}
	for _, tc := range cases {
		if got := overlap(tc.a, tc.b); got != tc.expected {
			t.Errorf("%s: overlap(%v, %v) = %v, want %v", tc.name, tc.a, tc.b, got, tc.expected)
		}
	}
}

func TestScopeAllowsRequiresEveryResourceCovered(t *testing.T) {
	if !scopeAllows([]string{"*"}, []string{"repo/a", "repo/b"}) {
		t.Fatal("expected a wildcard scope to allow any resource set")
	}
	if !scopeAllows([]string{"repo"}, []string{"repo/a"}) {
		t.Fatal("expected a parent scope to allow a nested resource")
	}
	if scopeAllows([]string{"repo/a"}, []string{"repo/a", "repo/b"}) {
		t.Fatal("expected a partial scope match to reject when any resource is uncovered")
	}
	if !scopeAllows([]string{"repo/a", "repo/b"}, nil) {
		t.Fatal("expected no required resources to always be allowed")
	}
}

func TestHasApproval(t *testing.T) {
	st := model.State{Approvals: map[string]model.Approval{
		"a1": {Action: "do-thing", Status: "APPROVED", Tier: "ORCHESTRATOR"},
		"a2": {Action: "do-other", Status: "PENDING", Tier: "HUMAN"},
	}}
	if !hasApproval(st, "do-thing") {
		t.Fatal("expected an APPROVED approval to satisfy hasApproval")
	}
	if hasApproval(st, "do-other") {
		t.Fatal("expected a PENDING approval not to satisfy hasApproval")
	}
	if hasApproval(st, "missing") {
		t.Fatal("expected a missing action not to satisfy hasApproval")
	}
}

// TestHasOrchestratorGrantApprovalIsIDScoped guards RFC 0023's fix: a
// HUMAN-tier, APPROVED approval only satisfies hasOrchestratorGrantApproval
// when it lives at the exact conventional ID
// (OrchestratorGrantApprovalID(principalID)) -- a different, even
// genuinely-APPROVED approval that merely happens to share the same action
// string must never substitute for it. Reproduces the exact live scenario
// found in a real project's approval history: two independently-approved
// records ("grant-orchestrator-THOR", "grant-capabilities-THOR") sharing
// action "agent.activate:THOR".
func TestHasOrchestratorGrantApprovalIsIDScoped(t *testing.T) {
	st := model.State{Approvals: map[string]model.Approval{
		"grant-capabilities-THOR": {
			Action: "agent.activate:THOR", Status: "APPROVED", Tier: "HUMAN",
		},
	}}
	if hasOrchestratorGrantApproval(st, "THOR") {
		t.Fatal("expected an APPROVED approval under the wrong ID not to satisfy hasOrchestratorGrantApproval, even though its action string matches")
	}

	st.Approvals["grant-orchestrator-THOR"] = model.Approval{
		Action: "agent.activate:THOR", Status: "APPROVED", Tier: "HUMAN",
	}
	if !hasOrchestratorGrantApproval(st, "THOR") {
		t.Fatal("expected an APPROVED approval at the conventional ID to satisfy hasOrchestratorGrantApproval")
	}

	st.Approvals["grant-orchestrator-THOR"] = model.Approval{
		Action: "agent.activate:THOR", Status: "APPROVED", Tier: "ORCHESTRATOR",
	}
	if hasOrchestratorGrantApproval(st, "THOR") {
		t.Fatal("expected an ORCHESTRATOR-tier approval not to satisfy hasOrchestratorGrantApproval")
	}

	st.Approvals["grant-orchestrator-THOR"] = model.Approval{
		Action: "agent.activate:THOR", Status: "PENDING", Tier: "HUMAN",
	}
	if hasOrchestratorGrantApproval(st, "THOR") {
		t.Fatal("expected a PENDING approval not to satisfy hasOrchestratorGrantApproval")
	}

	st.Approvals["grant-orchestrator-THOR"] = model.Approval{
		Action: "agent.activate:someone-else", Status: "APPROVED", Tier: "HUMAN",
	}
	if hasOrchestratorGrantApproval(st, "THOR") {
		t.Fatal("expected an approval at the right ID but wrong action not to satisfy hasOrchestratorGrantApproval")
	}
}

func TestOrchestratorGrantApprovalHelpersAreCopyPasteable(t *testing.T) {
	if got, want := OrchestratorGrantApprovalAction("target"), "agent.activate:target"; got != want {
		t.Fatalf("OrchestratorGrantApprovalAction = %q, want %q", got, want)
	}
	if got, want := OrchestratorGrantApprovalID("target"), "grant-orchestrator-target"; got != want {
		t.Fatalf("OrchestratorGrantApprovalID = %q, want %q", got, want)
	}
}

func TestNextAndAutomaticDeliveryAttempts(t *testing.T) {
	st := model.State{InvocationDeliveries: map[string]model.InvocationDelivery{
		"d1": {InvocationID: "inv", Attempt: 1, Manual: false},
		"d2": {InvocationID: "inv", Attempt: 2, Manual: true},
		"d3": {InvocationID: "other", Attempt: 5, Manual: false},
	}}
	if got := nextDeliveryAttempt(st, "inv"); got != 3 {
		t.Fatalf("nextDeliveryAttempt = %d, want 3", got)
	}
	if got := nextDeliveryAttempt(st, "unrelated"); got != 1 {
		t.Fatalf("nextDeliveryAttempt for an invocation with no deliveries = %d, want 1", got)
	}
	if got := automaticDeliveryAttempts(st, "inv"); got != 1 {
		t.Fatalf("automaticDeliveryAttempts = %d, want 1 (manual attempts excluded)", got)
	}
}

func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b"}, "b") {
		t.Fatal("expected containsString to find a present value")
	}
	if containsString([]string{"a", "b"}, "c") {
		t.Fatal("expected containsString to reject an absent value")
	}
	if containsString(nil, "a") {
		t.Fatal("expected containsString to fail closed on a nil slice")
	}
}

func TestInvocationIsSensitive(t *testing.T) {
	st := model.State{Tasks: map[string]model.Task{
		"risky":   {ID: "risky", Risk: "HIGH"},
		"routine": {ID: "routine", Risk: "ROUTINE"},
	}}
	if !invocationIsSensitive(st, model.InvocationRequested{Priority: "URGENT"}) {
		t.Fatal("expected an URGENT-priority invocation to be sensitive regardless of task")
	}
	if invocationIsSensitive(st, model.InvocationRequested{Priority: "NORMAL"}) {
		t.Fatal("expected a normal-priority invocation with no task to be non-sensitive")
	}
	if !invocationIsSensitive(st, model.InvocationRequested{Priority: "NORMAL", TaskID: "risky"}) {
		t.Fatal("expected a non-ROUTINE task's invocation to be sensitive")
	}
	if invocationIsSensitive(st, model.InvocationRequested{Priority: "NORMAL", TaskID: "routine"}) {
		t.Fatal("expected a ROUTINE task's invocation not to be sensitive")
	}
	if invocationIsSensitive(st, model.InvocationRequested{Priority: "NORMAL", TaskID: "missing"}) {
		t.Fatal("expected an unresolvable task reference to fail closed to non-sensitive, not panic")
	}
}

func TestRefreshRuntimePresenceOfflinesExpiredHeartbeats(t *testing.T) {
	now := time.Now().UTC()
	st := &model.State{AgentRuntimes: map[string]model.AgentRuntime{
		"stale": {
			ID: "stale", Status: "ONLINE", Health: "HEALTHY",
			LastSeenAt: now.Add(-time.Hour), EndpointID: "ep",
			ActiveInvocations: []string{"inv-1"},
		},
		"fresh": {
			ID: "fresh", Status: "ONLINE", Health: "HEALTHY",
			LastSeenAt: now.Add(-time.Second),
		},
		"already-offline": {ID: "already-offline", Status: "OFFLINE"},
	}}
	RefreshRuntimePresence(st, now)
	stale := st.AgentRuntimes["stale"]
	if stale.Status != "OFFLINE" || stale.Health != "UNKNOWN" || stale.EndpointID != "" || stale.ActiveInvocations != nil {
		t.Fatalf("expected a stale heartbeat to be fully cleared, got %+v", stale)
	}
	if st.AgentRuntimes["fresh"].Status != "ONLINE" {
		t.Fatal("expected a recently-seen runtime to remain online")
	}
	if st.AgentRuntimes["already-offline"].Status != "OFFLINE" {
		t.Fatal("expected an already-offline runtime to be left alone")
	}
}

// -- agent.register / agent.rename --------------------------------------

func TestAgentRegisterRejectsDuplicateAndInvalidPayload(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{"existing": {ID: "existing"}}}
	if _, err := ValidateTransition(st, "new", "agent.register", "existing",
		model.AgentRegistered{PublicKey: "pk", PrincipalType: model.PrincipalHuman}, time.Now()); err == nil {
		t.Fatal("expected registering an existing ID to be rejected")
	}
	if _, err := ValidateTransition(st, "new", "agent.register", "fresh",
		model.AgentRegistered{PublicKey: "", PrincipalType: model.PrincipalHuman}, time.Now()); err == nil {
		t.Fatal("expected registration without a public key to be rejected")
	}
	if _, err := ValidateTransition(st, "new", "agent.register", "fresh",
		"not-a-payload", time.Now()); err == nil {
		t.Fatal("expected a malformed registration payload to be rejected, not panic")
	}
	if _, err := ValidateTransition(st, "new", "agent.register", "fresh",
		model.AgentRegistered{PublicKey: "pk", PrincipalType: model.PrincipalHuman}, time.Now()); err != nil {
		t.Fatalf("expected a fresh, valid registration to succeed: %v", err)
	}
}

func TestAgentRenameRequiresExistingActiveTargetAndNonEmptyName(t *testing.T) {
	st := model.State{Agents: map[string]model.Agent{
		"owner":   humanAgent("owner"),
		"revoked": {ID: "revoked", Status: "REVOKED"},
	}}
	if _, err := ValidateTransition(st, "owner", "agent.rename", "missing",
		model.AgentRenamed{DisplayName: "New"}, time.Now()); err == nil {
		t.Fatal("expected renaming a nonexistent principal to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "agent.rename", "revoked",
		model.AgentRenamed{DisplayName: "New"}, time.Now()); err == nil {
		t.Fatal("expected renaming a revoked principal to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "agent.rename", "owner",
		model.AgentRenamed{DisplayName: "  "}, time.Now()); err == nil {
		t.Fatal("expected an empty (whitespace-only) display name to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "agent.rename", "owner",
		model.AgentRenamed{DisplayName: "Renamed Owner"}, time.Now()); err != nil {
		t.Fatalf("expected a valid rename to succeed: %v", err)
	}
}

// -- task lifecycle -------------------------------------------------------

func taskState(extra ...func(*model.State)) model.State {
	st := model.State{
		Agents: map[string]model.Agent{
			"owner":   humanAgent("owner"),
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent, Scopes: []string{"*"}},
			"other":   {ID: "other", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent, Scopes: []string{"*"}},
		},
		Tasks: map[string]model.Task{},
	}
	for _, f := range extra {
		f(&st)
	}
	return st
}

func TestTaskCreateValidatesRequiredFieldsAndDuplicateID(t *testing.T) {
	st := taskState()
	if _, err := ValidateTransition(st, "owner", "task.create", "t1",
		model.TaskCreated{Title: "", Repository: "r", Branch: "b", Resources: []string{"r/a"}}, time.Now()); err == nil {
		t.Fatal("expected a task without a title to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "task.create", "t1",
		model.TaskCreated{Title: "T", Repository: "r", Branch: "b", Resources: nil}, time.Now()); err == nil {
		t.Fatal("expected a task without resources to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "task.create", "t1",
		model.TaskCreated{Title: "T", Repository: "r", Branch: "b", Resources: []string{"r/a"}}, time.Now()); err != nil {
		t.Fatalf("expected a valid task create to succeed: %v", err)
	}
	st.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Resources: []string{"r/a"}}
	if _, err := ValidateTransition(st, "owner", "task.create", "t1",
		model.TaskCreated{Title: "T", Repository: "r", Branch: "b", Resources: []string{"r/a"}}, time.Now()); err == nil {
		t.Fatal("expected creating a task with an already-existing ID to be rejected")
	}
}

func TestTaskClaimRejectsScopeOverrunAndOwnedTasks(t *testing.T) {
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Resources: []string{"repo/a"}}
		s.Tasks["owned"] = model.Task{ID: "owned", Status: "CLAIMED", Owner: "builder", Resources: []string{"repo/b"}}
	})
	if _, err := ValidateTransition(st, "owner", "task.claim", "owned", model.TaskClaimed{}, time.Now()); err == nil {
		t.Fatal("expected claiming an already-owned task to be rejected")
	}
	scoped := taskState(func(s *model.State) {
		s.Agents["builder"] = model.Agent{ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent, Scopes: []string{"repo/only-this"}}
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Resources: []string{"repo/other"}}
	})
	if _, err := ValidateTransition(scoped, "builder", "task.claim", "t1", model.TaskClaimed{}, time.Now()); err == nil {
		t.Fatal("expected a claim exceeding the principal's scopes to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "task.claim", "t1", model.TaskClaimed{}, time.Now()); err != nil {
		t.Fatalf("expected a valid claim to succeed: %v", err)
	}
}

func TestTaskClaimRejectsOverlappingWriteLeaseWithoutSharedWriteApproval(t *testing.T) {
	now := time.Now()
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Resources: []string{"repo/shared"}}
		s.Tasks["t2"] = model.Task{
			ID: "t2", Status: "CLAIMED", Owner: "other", Resources: []string{"repo/shared"},
			LeaseUntil: now.Add(time.Hour),
		}
	})
	if _, err := ValidateTransition(st, "builder", "task.claim", "t1", model.TaskClaimed{}, now); err == nil {
		t.Fatal("expected an overlapping write lease to be rejected without a shared-write approval")
	}
	st.Approvals = map[string]model.Approval{
		"shared": {Action: "shared-write:t1:t2", Status: "APPROVED"},
	}
	if _, err := ValidateTransition(st, "builder", "task.claim", "t1", model.TaskClaimed{}, now); err != nil {
		t.Fatalf("expected a shared-write approval to permit the overlapping claim: %v", err)
	}
}

func TestTaskClaimRejectsConflictingWorktreeLease(t *testing.T) {
	now := time.Now()
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Resources: []string{"repo/a"}, Worktree: "wt"}
		s.Tasks["t2"] = model.Task{
			ID: "t2", Status: "CLAIMED", Owner: "other", Resources: []string{"repo/b"},
			Worktree: "wt", LeaseUntil: now.Add(time.Hour),
		}
	})
	if _, err := ValidateTransition(st, "builder", "task.claim", "t1", model.TaskClaimed{}, now); err == nil {
		t.Fatal("expected a worktree already leased by a different owner to be rejected")
	}
}

func TestTaskRenewRequiresOwnerAndProgress(t *testing.T) {
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "CLAIMED", Owner: "builder", Resources: []string{"repo/a"}}
	})
	if _, err := ValidateTransition(st, "other", "task.renew", "t1",
		model.TaskRenewed{Progress: "still working"}, time.Now()); err == nil {
		t.Fatal("expected a non-owner renewal to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "task.renew", "t1",
		model.TaskRenewed{Progress: ""}, time.Now()); err == nil {
		t.Fatal("expected a renewal without a progress summary to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "task.renew", "t1",
		model.TaskRenewed{Progress: "still working"}, time.Now()); err != nil {
		t.Fatalf("expected a valid renewal to succeed: %v", err)
	}
}

func TestTaskHandoffAndAcceptRequireCorrectParty(t *testing.T) {
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "IN_PROGRESS", Owner: "builder", Resources: []string{"repo/a"}, HandoffTo: "other"}
	})
	if _, err := ValidateTransition(st, "other", "task.handoff", "t1",
		model.TaskHandoff{To: "other", Summary: "handing off"}, time.Now()); err == nil {
		t.Fatal("expected handoff initiated by a non-owner to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "task.handoff", "t1",
		model.TaskHandoff{To: "other", Summary: "handing off"}, time.Now()); err != nil {
		t.Fatalf("expected the task owner to hand off successfully: %v", err)
	}
	if _, err := ValidateTransition(st, "builder", "task.handoff.accept", "t1",
		model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected acceptance by anyone other than the handoff target to be rejected")
	}
	if _, err := ValidateTransition(st, "other", "task.handoff.accept", "t1",
		model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected the handoff target to accept successfully: %v", err)
	}
}

func TestTaskStatusTransitionsEnforceAllowedSourceStatus(t *testing.T) {
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "OPEN", Owner: "", Resources: []string{"repo/a"}}
	})
	if _, err := ValidateTransition(st, "builder", "task.start", "t1", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected task.start on an OPEN (unclaimed) task to be rejected")
	}
	st.Tasks["t1"] = model.Task{ID: "t1", Status: "CLAIMED", Owner: "builder", Resources: []string{"repo/a"}}
	if _, err := ValidateTransition(st, "builder", "task.start", "t1", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected task.start on a CLAIMED task owned by the actor to succeed: %v", err)
	}
	st.Tasks["t1"] = model.Task{ID: "t1", Status: "IN_PROGRESS", Owner: "builder", Resources: []string{"repo/a"}}
	if _, err := ValidateTransition(st, "other", "task.block", "t1", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected blocking someone else's task by a non-owner, non-orchestrator actor to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "task.block", "t1", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected an owner-role actor to block another principal's task: %v", err)
	}
}

func TestTaskCompleteRequiresReviewForNonRoutineOrRequireReviewSettings(t *testing.T) {
	now := time.Now()
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "IN_PROGRESS", Owner: "builder", Resources: []string{"repo/a"}, Risk: "HIGH"}
	})
	if _, err := ValidateTransition(st, "builder", "task.complete", "t1", model.TaskStatus{}, now); err == nil {
		t.Fatal("expected completing a non-ROUTINE task straight from IN_PROGRESS to be rejected")
	}
	st.Tasks["t1"] = model.Task{ID: "t1", Status: "REVIEW", Owner: "builder", Resources: []string{"repo/a"}, Risk: "HIGH"}
	if _, err := ValidateTransition(st, "builder", "task.complete", "t1", model.TaskStatus{}, now); err == nil {
		t.Fatal("expected the task owner (not an eligible reviewer) to be rejected completing their own reviewed task")
	}
	if _, err := ValidateTransition(st, "owner", "task.complete", "t1", model.TaskStatus{}, now); err != nil {
		t.Fatalf("expected an owner-role reviewer to complete the task: %v", err)
	}
	routine := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "IN_PROGRESS", Owner: "builder", Resources: []string{"repo/a"}, Risk: "ROUTINE"}
	})
	if _, err := ValidateTransition(routine, "builder", "task.complete", "t1", model.TaskStatus{}, now); err != nil {
		t.Fatalf("expected a ROUTINE task to skip the review gate by default: %v", err)
	}
}

func TestTaskCancelAllowedFromAnyOpenStatus(t *testing.T) {
	for _, status := range []string{"OPEN", "OFFERED", "CLAIMED", "IN_PROGRESS", "BLOCKED", "REVIEW"} {
		st := taskState(func(s *model.State) {
			s.Tasks["t1"] = model.Task{ID: "t1", Status: status, Owner: "builder", Resources: []string{"repo/a"}}
		})
		if _, err := ValidateTransition(st, "builder", "task.cancel", "t1", model.TaskStatus{}, time.Now()); err != nil {
			t.Fatalf("expected task.cancel to succeed from status %s: %v", status, err)
		}
	}
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "COMPLETED", Owner: "builder", Resources: []string{"repo/a"}}
	})
	if _, err := ValidateTransition(st, "builder", "task.cancel", "t1", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected task.cancel on an already-COMPLETED task to be rejected")
	}
}

func TestTaskTakeoverRequiresApproval(t *testing.T) {
	st := taskState(func(s *model.State) {
		s.Tasks["t1"] = model.Task{ID: "t1", Status: "CLAIMED", Owner: "builder", Resources: []string{"repo/a"}}
	})
	if _, err := ValidateTransition(st, "other", "task.takeover", "t1", model.TaskStatus{}, time.Now()); err == nil {
		t.Fatal("expected a takeover without an approved takeover record to be rejected")
	}
	st.Approvals = map[string]model.Approval{"a1": {Action: "task.takeover:t1", Status: "APPROVED"}}
	if _, err := ValidateTransition(st, "other", "task.takeover", "t1", model.TaskStatus{}, time.Now()); err != nil {
		t.Fatalf("expected an approved takeover to succeed: %v", err)
	}
}

// -- message lifecycle -----------------------------------------------------

func messageState() model.State {
	return model.State{
		Agents: map[string]model.Agent{
			"owner":      humanAgent("owner"),
			"agent-orch": agentOrchestrator("agent-orch"),
			"recipient":  {ID: "recipient", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		Messages: map[string]model.Message{},
	}
}

func TestMessagePostValidatesKindRecipientsAndBodyLength(t *testing.T) {
	st := messageState()
	if _, err := ValidateTransition(st, "owner", "message.post", "m1",
		model.MessagePosted{Kind: "NOT_A_KIND", To: []string{"recipient"}, Subject: "s"}, time.Now()); err == nil {
		t.Fatal("expected an invalid message kind to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "message.post", "m1",
		model.MessagePosted{Kind: "FYI", To: []string{"unknown"}, Subject: "s"}, time.Now()); err == nil {
		t.Fatal("expected posting to an unregistered recipient to be rejected")
	}
	longBody := make([]byte, 1201)
	if _, err := ValidateTransition(st, "owner", "message.post", "m1",
		model.MessagePosted{Kind: "FYI", To: []string{"recipient"}, Subject: "s", Body: string(longBody)}, time.Now()); err == nil {
		t.Fatal("expected a message body over 1200 characters to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "message.post", "m1",
		model.MessagePosted{Kind: "FYI", To: []string{"recipient"}, Subject: "s"}, time.Now()); err != nil {
		t.Fatalf("expected a valid FYI message to succeed: %v", err)
	}
}

func TestMessagePostContractKindRequiresElevationOrApproval(t *testing.T) {
	st := messageState()
	if _, err := ValidateTransition(st, "recipient", "message.post", "m1",
		model.MessagePosted{Kind: "CONTRACT", To: []string{"owner"}, Subject: "s"}, time.Now()); err == nil {
		t.Fatal("expected a CONTRACT message from a non-elevated, non-approved actor to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "message.post", "m1",
		model.MessagePosted{Kind: "CONTRACT", To: []string{"recipient"}, Subject: "s"}, time.Now()); err != nil {
		t.Fatalf("expected an owner to post a CONTRACT message directly: %v", err)
	}
	st.Approvals = map[string]model.Approval{"a1": {Action: "contract:m2", Status: "APPROVED"}}
	if _, err := ValidateTransition(st, "recipient", "message.post", "m2",
		model.MessagePosted{Kind: "CONTRACT", To: []string{"owner"}, Subject: "s"}, time.Now()); err != nil {
		t.Fatalf("expected an approved contract publication to succeed: %v", err)
	}
}

func TestMessageResponseTransitionsEnforceRecipientStatus(t *testing.T) {
	st := messageState()
	st.Messages["m1"] = model.Message{
		ID: "m1", Kind: "ACTION", From: "owner", To: []string{"recipient"},
		Recipients: []model.RecipientState{{Principal: "recipient", Status: "PENDING"}},
	}
	if _, err := ValidateTransition(st, "owner", "message.ack", "m1", model.MessageResponse{}, time.Now()); err == nil {
		t.Fatal("expected message.ack by a non-recipient to be rejected")
	}
	normalized, err := ValidateTransition(st, "recipient", "message.ack", "m1", model.MessageResponse{}, time.Now())
	if err != nil {
		t.Fatalf("expected a pending recipient to ack an ACTION message: %v", err)
	}
	if normalized.(model.MessageResponse).Response != "ACCEPTED" {
		t.Fatalf("expected ack of an ACTION message to normalize to ACCEPTED, got %+v", normalized)
	}
	st.Messages["m1"].Recipients[0].Status = "ACCEPTED"
	if _, err := ValidateTransition(st, "recipient", "message.complete", "m1", model.MessageResponse{}, time.Now()); err != nil {
		t.Fatalf("expected completing an ACCEPTED ACTION message to succeed: %v", err)
	}

	st.Messages["blocker"] = model.Message{
		ID: "blocker", Kind: "BLOCKER", From: "owner", To: []string{"recipient"},
		Recipients: []model.RecipientState{{Principal: "recipient", Status: "PENDING"}},
	}
	if _, err := ValidateTransition(st, "recipient", "message.resolve", "blocker", model.MessageResponse{}, time.Now()); err == nil {
		t.Fatal("expected resolving a not-yet-acknowledged BLOCKER to be rejected")
	}
	st.Messages["blocker"].Recipients[0].Status = "ACKNOWLEDGED"
	if _, err := ValidateTransition(st, "recipient", "message.resolve", "blocker", model.MessageResponse{}, time.Now()); err != nil {
		t.Fatalf("expected resolving an acknowledged BLOCKER to succeed: %v", err)
	}

	st.Messages["reject-me"] = model.Message{
		ID: "reject-me", Kind: "FYI", From: "owner", To: []string{"recipient"},
		Recipients: []model.RecipientState{{Principal: "recipient", Status: "PENDING"}},
	}
	if _, err := ValidateTransition(st, "recipient", "message.reject", "reject-me", model.MessageResponse{}, time.Now()); err != nil {
		t.Fatalf("expected rejecting a pending message to succeed: %v", err)
	}
}

// -- approval / decision / document -----------------------------------------

func TestApprovalRequestValidatesTierAndDuplicateID(t *testing.T) {
	st := model.State{
		Agents:    map[string]model.Agent{"owner": humanAgent("owner")},
		Approvals: map[string]model.Approval{"existing": {}},
	}
	if _, err := ValidateTransition(st, "owner", "approval.request", "a1",
		model.ApprovalRequested{Tier: "NOT_A_TIER", Action: "do-thing"}, time.Now()); err == nil {
		t.Fatal("expected an invalid approval tier to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "approval.request", "a1",
		model.ApprovalRequested{Tier: "HUMAN", Action: ""}, time.Now()); err == nil {
		t.Fatal("expected an approval request without an action to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "approval.request", "existing",
		model.ApprovalRequested{Tier: "HUMAN", Action: "do-thing"}, time.Now()); err == nil {
		t.Fatal("expected requesting an approval with an already-existing ID to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "approval.request", "a1",
		model.ApprovalRequested{Tier: "HUMAN", Action: "do-thing"}, time.Now()); err != nil {
		t.Fatalf("expected a valid approval request to succeed: %v", err)
	}
}

func TestApprovalApproveAndRejectRequirePending(t *testing.T) {
	st := model.State{
		Agents: map[string]model.Agent{"owner": humanAgent("owner")},
		Approvals: map[string]model.Approval{
			"pending":  {Status: "PENDING"},
			"resolved": {Status: "APPROVED"},
		},
	}
	if _, err := ValidateTransition(st, "owner", "approval.approve", "resolved", model.ApprovalResponse{}, time.Now()); err == nil {
		t.Fatal("expected approving an already-resolved approval to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "approval.approve", "missing", model.ApprovalResponse{}, time.Now()); err == nil {
		t.Fatal("expected approving a nonexistent approval to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "approval.approve", "pending", model.ApprovalResponse{}, time.Now()); err != nil {
		t.Fatalf("expected approving a pending approval to succeed: %v", err)
	}
	st.Approvals["pending2"] = model.Approval{Status: "PENDING"}
	if _, err := ValidateTransition(st, "owner", "approval.reject", "pending2", model.ApprovalResponse{}, time.Now()); err != nil {
		t.Fatalf("expected rejecting a pending approval to succeed: %v", err)
	}
}

func TestDecisionCreateValidatesRequiredFieldsAndDuplicateID(t *testing.T) {
	st := model.State{
		Agents:    map[string]model.Agent{"owner": humanAgent("owner")},
		Decisions: map[string]model.Decision{"existing": {}},
	}
	if _, err := ValidateTransition(st, "owner", "decision.create", "d1",
		model.DecisionPayload{Title: "", Statement: "s"}, time.Now()); err == nil {
		t.Fatal("expected a decision without a title to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "decision.create", "existing",
		model.DecisionPayload{Title: "T", Statement: "s"}, time.Now()); err == nil {
		t.Fatal("expected creating a decision with an already-existing ID to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "decision.create", "d1",
		model.DecisionPayload{Title: "T", Statement: "s"}, time.Now()); err != nil {
		t.Fatalf("expected a valid decision create to succeed: %v", err)
	}
}

func TestDocumentLifecycleCreateUpdateSupersede(t *testing.T) {
	st := model.State{
		Agents:    map[string]model.Agent{"owner": humanAgent("owner")},
		Documents: map[string]model.Document{},
	}
	if _, err := ValidateTransition(st, "owner", "document.create", "doc1",
		model.DocumentPayload{Title: "", Body: "b"}, time.Now()); err == nil {
		t.Fatal("expected creating a document without a title to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "document.create", "doc1",
		model.DocumentPayload{Title: "T", Body: "b"}, time.Now()); err != nil {
		t.Fatalf("expected a valid document create to succeed: %v", err)
	}
	st.Documents["doc1"] = model.Document{ID: "doc1"}
	if _, err := ValidateTransition(st, "owner", "document.create", "doc1",
		model.DocumentPayload{Title: "T", Body: "b"}, time.Now()); err == nil {
		t.Fatal("expected creating a document with an already-existing ID to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "document.update", "missing",
		model.DocumentPayload{Title: "T", Body: "b"}, time.Now()); err == nil {
		t.Fatal("expected updating a nonexistent document to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "document.update", "doc1",
		model.DocumentPayload{Title: "T2", Body: "b2"}, time.Now()); err != nil {
		t.Fatalf("expected a valid document update to succeed: %v", err)
	}
	if _, err := ValidateTransition(st, "owner", "document.supersede", "doc1",
		model.DocumentPayload{ReplacementID: "doc1"}, time.Now()); err == nil {
		t.Fatal("expected a document to be rejected superseding itself")
	}
	if _, err := ValidateTransition(st, "owner", "document.supersede", "doc1",
		model.DocumentPayload{ReplacementID: "missing"}, time.Now()); err == nil {
		t.Fatal("expected superseding with a nonexistent replacement to be rejected")
	}
	st.Documents["doc2"] = model.Document{ID: "doc2"}
	if _, err := ValidateTransition(st, "owner", "document.supersede", "doc1",
		model.DocumentPayload{ReplacementID: "doc2"}, time.Now()); err != nil {
		t.Fatalf("expected a valid supersede to succeed: %v", err)
	}
}

// -- runtime lifecycle -------------------------------------------------

func runtimeAgentState() model.State {
	return model.State{
		Agents: map[string]model.Agent{
			"owner":   humanAgent("owner"),
			"builder": {ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent},
		},
		AgentRuntimes: map[string]model.AgentRuntime{},
		Invocations:   map[string]model.Invocation{},
	}
}

func TestRuntimeRegisterValidatesOwnerDuplicateAndDefinition(t *testing.T) {
	st := runtimeAgentState()
	if _, err := ValidateTransition(st, "builder", "runtime.register", "r1",
		model.RuntimeRegistered{AgentID: "builder", Connector: "MCP", MaxConcurrent: 1}, time.Now()); err != nil {
		t.Fatalf("expected a self-registered runtime to succeed: %v", err)
	}
	// ValidateTransition is a pure validator -- it never mutates st itself,
	// that's the projection's job (internal/projection/apply.go). Simulate
	// that persisted effect here so the next call actually observes the
	// runtime as already registered.
	st.AgentRuntimes["r1"] = model.AgentRuntime{ID: "r1", AgentID: "builder"}
	if _, err := ValidateTransition(st, "builder", "runtime.register", "r1",
		model.RuntimeRegistered{AgentID: "builder", Connector: "MCP", MaxConcurrent: 1}, time.Now()); err == nil {
		t.Fatal("expected registering an already-existing runtime ID to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.register", "r2",
		model.RuntimeRegistered{AgentID: "owner", Connector: "MCP", MaxConcurrent: 1}, time.Now()); err == nil {
		t.Fatal("expected an agent to be rejected registering a runtime on someone else's behalf without elevation")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.register", "r3",
		model.RuntimeRegistered{AgentID: "builder", Connector: "NOT_A_CONNECTOR", MaxConcurrent: 1}, time.Now()); err == nil {
		t.Fatal("expected an invalid connector to be rejected")
	}
}

func TestRuntimeHeartbeatValidatesHealthCapacityAndAssignedInvocations(t *testing.T) {
	now := time.Now().UTC()
	st := runtimeAgentState()
	st.AgentRuntimes["r1"] = model.AgentRuntime{
		ID: "r1", AgentID: "builder", Kind: model.RuntimeKindWorker,
		Connector: "MCP", Status: "ONLINE", MaxConcurrent: 2,
	}
	if _, err := ValidateTransition(st, "builder", "runtime.heartbeat", "r1",
		model.RuntimeHeartbeat{Health: "NOT_A_STATUS"}, now); err == nil {
		t.Fatal("expected an invalid health value to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "runtime.heartbeat", "r1",
		model.RuntimeHeartbeat{Health: "HEALTHY"}, now); err == nil {
		t.Fatal("expected a heartbeat from a non-owning actor to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.heartbeat", "r1",
		model.RuntimeHeartbeat{Health: "HEALTHY", ActiveInvocations: []string{"ghost"}}, now); err == nil {
		t.Fatal("expected an active invocation not assigned to this runtime to be rejected")
	}
	st.Invocations["inv1"] = model.Invocation{ID: "inv1", Target: "builder", RuntimeID: "r1"}
	if _, err := ValidateTransition(st, "builder", "runtime.heartbeat", "r1",
		model.RuntimeHeartbeat{Health: "HEALTHY", ActiveInvocations: []string{"inv1", "inv1"}}, now); err == nil {
		t.Fatal("expected duplicate active invocation IDs to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.heartbeat", "r1",
		model.RuntimeHeartbeat{Health: "HEALTHY", ActiveInvocations: []string{"inv1"}}, now); err != nil {
		t.Fatalf("expected a valid heartbeat to succeed: %v", err)
	}
}

func TestRuntimeStatusChangeTransitionsEnforceLifecycleOrder(t *testing.T) {
	now := time.Now()
	st := runtimeAgentState()
	st.AgentRuntimes["r1"] = model.AgentRuntime{
		ID: "r1", AgentID: "builder", Kind: model.RuntimeKindWorker,
		Connector: "MCP", Status: "ONLINE", MaxConcurrent: 1,
	}
	if _, err := ValidateTransition(st, "builder", "runtime.resume", "r1", model.RuntimeStatusChanged{}, now); err == nil {
		t.Fatal("expected resuming a non-draining runtime to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.drain", "r1", model.RuntimeStatusChanged{}, now); err != nil {
		t.Fatalf("expected draining an online runtime to succeed: %v", err)
	}
	st.AgentRuntimes["r1"] = model.AgentRuntime{ID: "r1", AgentID: "builder", Status: "DRAINING", MaxConcurrent: 1}
	if _, err := ValidateTransition(st, "builder", "runtime.drain", "r1", model.RuntimeStatusChanged{}, now); err == nil {
		t.Fatal("expected draining an already-draining runtime to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "runtime.resume", "r1", model.RuntimeStatusChanged{}, now); err != nil {
		t.Fatalf("expected resuming a draining runtime to succeed: %v", err)
	}
	if _, err := ValidateTransition(st, "owner", "runtime.revoke", "r1", model.RuntimeStatusChanged{}, now); err != nil {
		t.Fatalf("expected an owner to revoke a runtime: %v", err)
	}
	if _, err := ValidateTransition(st, "builder", "runtime.revoke", "r1", model.RuntimeStatusChanged{}, now); err == nil {
		t.Fatal("expected a plain agent (not owner/orchestrator) to be rejected revoking a runtime")
	}
	st.AgentRuntimes["r1"] = model.AgentRuntime{ID: "r1", AgentID: "builder", Status: "REVOKED"}
	if _, err := ValidateTransition(st, "builder", "runtime.delete", "r1", model.RuntimeStatusChanged{}, now); err != nil {
		t.Fatalf("expected the runtime owner to delete a revoked runtime: %v", err)
	}
	st.AgentRuntimes["r2"] = model.AgentRuntime{ID: "r2", AgentID: "builder", Status: "ONLINE"}
	if _, err := ValidateTransition(st, "builder", "runtime.delete", "r2", model.RuntimeStatusChanged{}, now); err == nil {
		t.Fatal("expected deleting a non-revoked runtime to be rejected")
	}
}

// -- invocation.policy.update -------------------------------------------

func TestInvocationPolicyUpdateValidatesModeActorsAndConsumerModes(t *testing.T) {
	st := runtimeAgentState()
	if _, err := ValidateTransition(st, "builder", "invocation.policy.update", "builder",
		model.InvocationPolicyUpdated{Mode: "AUTOMATIC"}, time.Now()); err == nil {
		t.Fatal("expected a non-elevated actor to be rejected updating an invocation policy")
	}
	if _, err := ValidateTransition(st, "owner", "invocation.policy.update", "builder",
		model.InvocationPolicyUpdated{Mode: "NOT_A_MODE"}, time.Now()); err == nil {
		t.Fatal("expected an invalid policy mode to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "invocation.policy.update", "builder",
		model.InvocationPolicyUpdated{Mode: "TRUSTED", TrustedActors: []string{"missing"}}, time.Now()); err == nil {
		t.Fatal("expected an unknown trusted actor to be rejected")
	}
	normalized, err := ValidateTransition(st, "owner", "invocation.policy.update", "builder",
		model.InvocationPolicyUpdated{Mode: "AUTOMATIC"}, time.Now())
	if err != nil {
		t.Fatalf("expected a valid policy update to succeed: %v", err)
	}
	policy := normalized.(model.InvocationPolicyUpdated)
	if policy.DefaultConsumerMode != model.ConsumerModeEither || len(policy.AllowedConsumerModes) != 3 {
		t.Fatalf("expected default consumer mode/allowed modes to be filled with sane defaults, got %+v", policy)
	}
	if _, err := ValidateTransition(st, "owner", "invocation.policy.update", "builder",
		model.InvocationPolicyUpdated{
			Mode: "AUTOMATIC", DefaultConsumerMode: model.ConsumerModeWorkerOnly,
			AllowedConsumerModes: []model.ConsumerMode{model.ConsumerModeInteractiveOnly},
		}, time.Now()); err == nil {
		t.Fatal("expected a default consumer mode absent from the allowed list to be rejected")
	}
}

// -- invocation.request policy-gated branches ----------------------------

// requesterAgentState returns a state with a plain, non-elevated AGENT
// principal ("requester") -- invocation.request's target-policy gate
// (MANUAL/TRUSTED/DISABLED) is only reachable for a non-elevated requester,
// since actorElevated(actor) (owner/orchestrator) bypasses it entirely by
// design; using "owner" here would silently skip every branch this exercises.
func requesterAgentState() model.State {
	st := runtimeAgentState()
	st.Agents["requester"] = model.Agent{ID: "requester", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent}
	return st
}

func TestInvocationRequestManualPolicyRequiresPriorApproval(t *testing.T) {
	st := requesterAgentState()
	st.InvocationPolicies = map[string]model.InvocationPolicy{
		"builder": {AgentID: "builder", Mode: "MANUAL"},
	}
	if _, err := ValidateTransition(st, "requester", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it"}, time.Now()); err == nil {
		t.Fatal("expected a MANUAL-policy target to require a pre-existing approval")
	}
	st.Approvals = map[string]model.Approval{"a1": {Action: "invocation:inv1", Status: "APPROVED"}}
	if _, err := ValidateTransition(st, "requester", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it"}, time.Now()); err != nil {
		t.Fatalf("expected an approved MANUAL-policy invocation to succeed: %v", err)
	}
}

func TestInvocationRequestDisabledPolicyAlwaysRejects(t *testing.T) {
	st := requesterAgentState()
	st.InvocationPolicies = map[string]model.InvocationPolicy{"builder": {AgentID: "builder", Mode: "DISABLED"}}
	if _, err := ValidateTransition(st, "requester", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it"}, time.Now()); err == nil {
		t.Fatal("expected a DISABLED target policy to always reject an invocation request")
	}
}

func TestInvocationRequestTrustedPolicyRequiresListedRequester(t *testing.T) {
	st := requesterAgentState()
	st.InvocationPolicies = map[string]model.InvocationPolicy{
		"builder": {AgentID: "builder", Mode: "TRUSTED", TrustedActors: []string{"someone-else"}},
	}
	if _, err := ValidateTransition(st, "requester", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it"}, time.Now()); err == nil {
		t.Fatal("expected an untrusted requester to be rejected under a TRUSTED policy")
	}
	st.InvocationPolicies["builder"] = model.InvocationPolicy{AgentID: "builder", Mode: "TRUSTED", TrustedActors: []string{"requester"}}
	if _, err := ValidateTransition(st, "requester", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it"}, time.Now()); err != nil {
		t.Fatalf("expected a trusted requester to succeed under a TRUSTED policy: %v", err)
	}
}

func TestInvocationRequestDeadlineMustBeInFutureAndWithinTTL(t *testing.T) {
	now := time.Now().UTC()
	st := runtimeAgentState()
	past := now.Add(-time.Hour)
	if _, err := ValidateTransition(st, "owner", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Deadline: &past}, now); err == nil {
		t.Fatal("expected a past deadline to be rejected")
	}
	tooFar := now.Add(30 * 24 * time.Hour)
	if _, err := ValidateTransition(st, "owner", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Deadline: &tooFar}, now); err == nil {
		t.Fatal("expected a deadline beyond the max invocation TTL to be rejected")
	}
	ok := now.Add(time.Hour)
	if _, err := ValidateTransition(st, "owner", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Deadline: &ok}, now); err != nil {
		t.Fatalf("expected a reasonable future deadline to succeed: %v", err)
	}
}

func TestInvocationRequestSensitiveRequiresHumanOrApproval(t *testing.T) {
	st := runtimeAgentState()
	st.Agents["requester-agent"] = model.Agent{ID: "requester-agent", Status: "ACTIVE", Role: model.Role("MEMBER"), PrincipalType: model.PrincipalAgent}
	st.InvocationPolicies = map[string]model.InvocationPolicy{
		"builder": {AgentID: "builder", Mode: "AUTOMATIC", RequireHumanForSensitive: true},
	}
	if _, err := ValidateTransition(st, "requester-agent", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Priority: "URGENT"}, time.Now()); err == nil {
		t.Fatal("expected a sensitive (URGENT) invocation from a non-human requester to require human approval")
	}
	if _, err := ValidateTransition(st, "owner", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Priority: "URGENT"}, time.Now()); err != nil {
		t.Fatalf("expected a human requester to bypass the sensitive-invocation approval gate: %v", err)
	}
}

func TestInvocationRequestRejectsInvalidPriority(t *testing.T) {
	st := runtimeAgentState()
	if _, err := ValidateTransition(st, "owner", "invocation.request", "inv1",
		model.InvocationRequested{Target: "builder", Instruction: "do it", Priority: "SUPER_URGENT"}, time.Now()); err == nil {
		t.Fatal("expected an invalid priority value to be rejected")
	}
}

func TestInvocationClaimRejectsPastDeadline(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	st := runtimeAgentState()
	st.Invocations["inv1"] = model.Invocation{ID: "inv1", Target: "builder", Status: "PENDING", Deadline: &past}
	st.AgentRuntimes["r1"] = model.AgentRuntime{
		ID: "r1", AgentID: "builder", Kind: model.RuntimeKindWorker,
		Connector: "MCP", Status: "ONLINE", Health: "HEALTHY", MaxConcurrent: 1,
	}
	if _, err := ValidateTransition(st, "builder", "invocation.claim", "inv1",
		model.InvocationClaimed{RuntimeID: "r1"}, now); err == nil {
		t.Fatal("expected claiming an invocation past its deadline to be rejected")
	}
}

func TestInvocationRejectExpireCancelEnforceActorAndStatus(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Minute)
	st := runtimeAgentState()
	st.Invocations["open"] = model.Invocation{ID: "open", Target: "builder", RequestedBy: "owner", Status: "PENDING"}
	if _, err := ValidateTransition(st, "owner", "invocation.reject", "open",
		model.InvocationRejected{Reason: "not now"}, now); err == nil {
		t.Fatal("expected invocation.reject by someone other than the target to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "invocation.reject", "open",
		model.InvocationRejected{Reason: ""}, now); err == nil {
		t.Fatal("expected invocation.reject without a reason to be rejected")
	}
	if _, err := ValidateTransition(st, "builder", "invocation.reject", "open",
		model.InvocationRejected{Reason: "not now"}, now); err != nil {
		t.Fatalf("expected the target to reject a pending invocation: %v", err)
	}

	st.Invocations["expireme"] = model.Invocation{ID: "expireme", Target: "builder", RequestedBy: "owner", Status: "PENDING", Deadline: &past}
	if _, err := ValidateTransition(st, "builder", "invocation.expire", "expireme",
		model.InvocationRejected{Reason: "expired"}, now); err == nil {
		t.Fatal("expected invocation.expire by someone other than the requester/owner/orchestrator to be rejected")
	}
	if _, err := ValidateTransition(st, "owner", "invocation.expire", "expireme",
		model.InvocationRejected{Reason: "expired"}, now); err != nil {
		t.Fatalf("expected the requester to expire a past-deadline invocation: %v", err)
	}

	st.Invocations["cancelme"] = model.Invocation{ID: "cancelme", Target: "builder", RequestedBy: "owner", Status: "RUNNING"}
	if _, err := ValidateTransition(st, "owner", "invocation.cancel", "cancelme",
		model.InvocationRejected{Reason: "changed my mind"}, now); err != nil {
		t.Fatalf("expected the requester to cancel a running invocation: %v", err)
	}
	st.Invocations["done"] = model.Invocation{ID: "done", Target: "builder", RequestedBy: "owner", Status: "COMPLETED"}
	if _, err := ValidateTransition(st, "owner", "invocation.cancel", "done",
		model.InvocationRejected{Reason: "too late"}, now); err == nil {
		t.Fatal("expected cancelling an already-COMPLETED invocation to be rejected")
	}
}

func TestInvocationDeliveryFailedRequiresMatchingAttemptAndRetryTime(t *testing.T) {
	now := time.Now().UTC()
	attemptedAt := now.Add(-time.Second)
	attemptUntil := now.Add(time.Minute)
	st := runtimeAgentState()
	st.Invocations["inv1"] = model.Invocation{ID: "inv1", Target: "builder", RequestedBy: "owner", Status: "PENDING"}
	st.InvocationDeliveries = map[string]model.InvocationDelivery{
		"d1": {
			ID: "d1", InvocationID: "inv1", RuntimeID: "r1", Attempt: 1, Status: "ATTEMPTED",
			AttemptedAt: &attemptedAt, AttemptUntil: &attemptUntil,
		},
	}
	if _, err := ValidateTransition(st, "owner", "invocation.delivery-failed", "inv1",
		model.InvocationDeliveryFailed{DeliveryID: "d1", Error: "boom", Final: false}, now); err == nil {
		t.Fatal("expected a non-final delivery failure without a future retry time to be rejected")
	}
	future := now.Add(time.Minute)
	normalized, err := ValidateTransition(st, "owner", "invocation.delivery-failed", "inv1",
		model.InvocationDeliveryFailed{DeliveryID: "d1", Error: "boom", NextRetry: &future}, now)
	if err != nil {
		t.Fatalf("expected a valid retryable delivery failure to succeed: %v", err)
	}
	if normalized.(model.InvocationDeliveryFailed).RuntimeID != "r1" {
		t.Fatalf("expected the failure to be bound to the matching delivery's runtime, got %+v", normalized)
	}
	if _, err := ValidateTransition(st, "owner", "invocation.delivery-failed", "inv1",
		model.InvocationDeliveryFailed{DeliveryID: "missing", Error: "boom", Final: true}, now); err == nil {
		t.Fatal("expected a delivery failure referencing an unmatched delivery ID to be rejected")
	}
}
