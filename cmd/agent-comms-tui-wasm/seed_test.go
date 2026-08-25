package main

import "testing"

// TestSeedDemoProjectLeavesAPendingApprovalAndInvocation proves
// seedDemoProject drives real state -- not mocked -- into the in-process
// demo service: a pending HUMAN-tier approval and enough seeded agents to
// match the reviewer/developer/tester story, read back via
// a real service.State() call through the in-process daemon.
func TestSeedDemoProjectLeavesAPendingApprovalAndInvocation(t *testing.T) {
	svc, err := bootstrapDemoService()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedDemoProject(svc); err != nil {
		t.Fatal(err)
	}
	state, err := svc.State()
	if err != nil {
		t.Fatal(err)
	}

	foundPendingApproval := false
	for _, approval := range state.Approvals {
		if approval.Status == "PENDING" {
			foundPendingApproval = true
		}
	}
	if !foundPendingApproval {
		t.Error("expected the seeded demo to leave a pending approval for the visitor to resolve")
	}

	// reviewer, developer, tester, plus the owner itself.
	if len(state.Agents) < 4 {
		t.Errorf("expected at least 4 seeded agents (owner + reviewer/developer/tester), got %d", len(state.Agents))
	}

	foundActiveInvocation := false
	for _, invocation := range state.Invocations {
		if invocation.Status == "RUNNING" {
			foundActiveInvocation = true
		}
	}
	if !foundActiveInvocation {
		t.Error("expected the seeded demo to leave a running invocation for the visitor to act on")
	}

	reviewer, ok := state.Agents["reviewer"]
	if !ok || string(reviewer.Role) != "Release-Coordinator" {
		t.Errorf("expected reviewer to be activated as Release-Coordinator, got %+v", reviewer)
	}
	developer, ok := state.Agents["developer"]
	if !ok || string(developer.Role) != "Frontend-Architect" {
		t.Errorf("expected developer to have switched to Frontend-Architect, got %+v", developer)
	}
	tester, ok := state.Agents["tester"]
	if !ok || string(tester.Role) != "Tester" {
		t.Errorf("expected tester to be activated as Tester, got %+v", tester)
	}
	if _, online := state.AgentRuntimes["tester-runtime-1"]; online {
		t.Error("expected tester to have no registered runtime (stays offline)")
	}

	task, ok := state.Tasks["task-auth-session"]
	if !ok || task.Owner != "developer" {
		t.Errorf("expected developer to have claimed the test/auth task, got %+v", task)
	}
}
