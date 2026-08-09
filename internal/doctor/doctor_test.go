package doctor_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/doctor"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

func setup(t *testing.T) *service.Service {
	t.Helper()
	s, _ := testsupport.StartPersonalProject(t)
	return s
}

func must(t *testing.T, s *service.Service, actor, eventType, entityID string, payload any) {
	t.Helper()
	if _, e := s.Execute(actor, eventType, entityID, payload); e != nil {
		t.Fatalf("%s %s: %v", eventType, entityID, e)
	}
}

func activateAgent(t *testing.T, s *service.Service, id string) {
	t.Helper()
	if _, e := s.Register(id, id, model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	must(t, s, "owner", "agent.activate", id, model.AgentActivated{
		Role: model.RoleAgent, Scopes: []string{"src"},
	})
}

func findFinding(findings []doctor.Finding, code string) (doctor.Finding, bool) {
	for _, f := range findings {
		if f.Code == code {
			return f, true
		}
	}
	return doctor.Finding{}, false
}

func TestRevokedAgentHasOpenWork_WithTerminalInvocationsOnly(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Create an invocation targeting alpha, then cancel it (terminal).
	must(t, s, "owner", "invocation.request", "inv-1", model.InvocationRequested{
		Target: "alpha", Instruction: "do something",
	})
	must(t, s, "owner", "invocation.cancel", "inv-1", model.InvocationRejected{Reason: "done"})

	// Revoke alpha after the invocation is terminal.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should not fire when all invocations are terminal")
	}
}

func TestRevokedAgentHasOpenWork_WithNonTerminalInvocation(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Create an invocation targeted at alpha but leave it pending.
	must(t, s, "owner", "invocation.request", "inv-2", model.InvocationRequested{
		Target: "alpha", Instruction: "do something",
	})

	// Revoke alpha while invocation is still open.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	finding, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK")
	if !found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should fire when revoked agent has non-terminal invocations")
	}
	if finding.Message == "" {
		t.Fatal("finding message should not be empty")
	}
}

func TestRevokedAgentHasOpenWork_RequestedByMatches(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Set alpha's invocation policy to AUTO so owner can request directly.
	must(t, s, "owner", "invocation.policy.update", "alpha", model.InvocationPolicyUpdated{
		Mode: "AUTOMATIC",
	})

	// alpha requests an invocation targeting itself, then cancel it.
	must(t, s, "alpha", "invocation.request", "inv-3", model.InvocationRequested{
		Target: "alpha", Instruction: "self work",
	})
	must(t, s, "owner", "invocation.cancel", "inv-3", model.InvocationRejected{Reason: "done"})

	// Revoke alpha — no open work, should not fire.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should not fire when revoked agent's requested invocations are all terminal")
	}
}

func TestRevokedAgentHasOpenWork_RequestedByOpen(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Set alpha's invocation policy to AUTO so owner can request directly.
	must(t, s, "owner", "invocation.policy.update", "alpha", model.InvocationPolicyUpdated{
		Mode: "AUTOMATIC",
	})

	// alpha requests an invocation targeting itself but doesn't cancel it.
	must(t, s, "alpha", "invocation.request", "inv-4", model.InvocationRequested{
		Target: "alpha", Instruction: "self work",
	})

	// Revoke alpha while it still has an open requested invocation.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); !found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should fire when revoked agent requested an open invocation")
	}
}

func TestRevokedAgentHasOpenWork_WithTerminalTasksOnly(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Create a task, offer to alpha, let alpha claim and complete it.
	must(t, s, "owner", "task.create", "task-1", model.TaskCreated{
		Title: "a task", Repository: "local", Branch: "main", Resources: []string{"src/a"},
	})
	must(t, s, "owner", "task.offer", "task-1", model.TaskOffered{
		To: "alpha", ExpiresAt: time.Now().Add(time.Hour),
	})
	must(t, s, "alpha", "task.claim", "task-1", model.TaskClaimed{})
	must(t, s, "alpha", "task.start", "task-1", model.TaskStatus{})
	must(t, s, "alpha", "task.complete", "task-1", model.TaskStatus{Summary: "done"})

	// Revoke alpha.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should not fire when all tasks are terminal")
	}
}

func TestRevokedAgentHasOpenWork_WithNonTerminalTask(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Create a task, offer to alpha, let alpha claim it but don't complete.
	must(t, s, "owner", "task.create", "task-2", model.TaskCreated{
		Title: "open task", Repository: "local", Branch: "main", Resources: []string{"src/b"},
	})
	must(t, s, "owner", "task.offer", "task-2", model.TaskOffered{
		To: "alpha", ExpiresAt: time.Now().Add(time.Hour),
	})
	must(t, s, "alpha", "task.claim", "task-2", model.TaskClaimed{})
	must(t, s, "alpha", "task.start", "task-2", model.TaskStatus{})

	// Revoke alpha while task is still in progress.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); !found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should fire when revoked agent has open tasks")
	}
}

func TestRevokedAgentHasOpenWork_NoWorkAtAll(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// Revoke alpha immediately with no work at all.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should not fire when revoked agent has no work")
	}
}

func TestRevokedAgentHasOpenWork_MultipleTerminalStatuses(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")

	// CANCELLED invocation.
	must(t, s, "owner", "invocation.request", "inv-cancel", model.InvocationRequested{
		Target: "alpha", Instruction: "cancel me",
	})
	must(t, s, "owner", "invocation.cancel", "inv-cancel", model.InvocationRejected{Reason: "nope"})

	// REJECTED invocation (target rejects).
	must(t, s, "owner", "invocation.request", "inv-r", model.InvocationRequested{
		Target: "alpha", Instruction: "reject me",
	})
	must(t, s, "alpha", "invocation.reject", "inv-r", model.InvocationRejected{Reason: "no"})

	// REJECTED invocation (another terminal status).
	must(t, s, "owner", "invocation.request", "inv-x", model.InvocationRequested{
		Target: "alpha", Instruction: "reject me too",
	})
	must(t, s, "alpha", "invocation.reject", "inv-x", model.InvocationRejected{Reason: "cannot do"})

	// Revoke alpha with all invocations in different terminal states.
	must(t, s, "owner", "agent.revoke", "alpha", model.RuntimeStatusChanged{Reason: "gone"})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findFinding(findings, "REVOKED_AGENT_HAS_OPEN_WORK"); found {
		t.Fatal("REVOKED_AGENT_HAS_OPEN_WORK should not fire when all invocations are terminal (mixed statuses)")
	}
}

// TestInteractiveRuntimeUnsupportedOnWindows covers both directions of the
// same check in one OS-portable test (this suite runs on all three CI
// platforms per .github/workflows/ci.yml): the finding must fire on
// Windows, where interactive-serve/--takeover-pid can never come online
// (internal/interactiveserve/serve_windows.go, takeover_windows.go), and
// must not fire anywhere else.
func TestInteractiveRuntimeUnsupportedOnWindows(t *testing.T) {
	s := setup(t)
	activateAgent(t, s, "alpha")
	hostID, err := identity.LoadHostID()
	if err != nil {
		t.Fatal(err)
	}
	must(t, s, "owner", "runtime.register", "rt-1", model.RuntimeRegistered{
		AgentID: "alpha", Kind: model.RuntimeKindInteractive, Connector: "INTERACTIVE",
		HostID: hostID, MaxConcurrent: 1,
	})

	findings, err := doctor.Findings(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	_, found := findFinding(findings, "INTERACTIVE_RUNTIME_UNSUPPORTED_ON_WINDOWS")
	if runtime.GOOS == "windows" && !found {
		t.Fatal("INTERACTIVE_RUNTIME_UNSUPPORTED_ON_WINDOWS should fire for an interactive runtime on Windows")
	}
	if runtime.GOOS != "windows" && found {
		t.Fatal("INTERACTIVE_RUNTIME_UNSUPPORTED_ON_WINDOWS should not fire outside Windows")
	}
}
