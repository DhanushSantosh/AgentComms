package main

import "testing"

// TestSeedDemoProjectLeavesAPendingApprovalAndInvocation proves
// seedDemoProject drives real state -- not mocked -- into the in-process
// demo service: a pending HUMAN-tier approval and enough seeded agents to
// match the AXIOM/DAMON/GORGE story, read back via a real service.State()
// call through the in-process daemon.
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

	// AXIOM, DAMON, GORGE, plus the owner itself.
	if len(state.Agents) < 4 {
		t.Errorf("expected at least 4 seeded agents (owner + AXIOM/DAMON/GORGE), got %d", len(state.Agents))
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

	axiom, ok := state.Agents["AXIOM"]
	if !ok || string(axiom.Role) != "Release-Coordinator" {
		t.Errorf("expected AXIOM to be activated as Release-Coordinator, got %+v", axiom)
	}
	damon, ok := state.Agents["DAMON"]
	if !ok || string(damon.Role) != "Frontend-Architect" {
		t.Errorf("expected DAMON to have switched to Frontend-Architect, got %+v", damon)
	}
	gorge, ok := state.Agents["GORGE"]
	if !ok || string(gorge.Role) != "Tester" {
		t.Errorf("expected GORGE to be activated as Tester, got %+v", gorge)
	}
	if _, online := state.AgentRuntimes["gorge-runtime-1"]; online {
		t.Error("expected GORGE to have no registered runtime (stays offline)")
	}

	task, ok := state.Tasks["task-auth-session"]
	if !ok || task.Owner != "DAMON" {
		t.Errorf("expected DAMON to have claimed the test/auth task, got %+v", task)
	}
}
