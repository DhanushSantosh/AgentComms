package projection

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestLegacyInvocationAndRuntimeProjectToCompatibilityDefaults(t *testing.T) {
	state := model.State{}
	requestData, err := json.Marshal(model.InvocationRequested{
		Target: "builder", Instruction: "Review the project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-request", Time: time.Now().UTC(), Actor: "owner",
		Type: "invocation.request", EntityID: "invocation", Data: requestData,
	}); err != nil {
		t.Fatal(err)
	}
	runtimeData, err := json.Marshal(model.RuntimeRegistered{
		AgentID: "builder", Connector: "MCP", MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-runtime", Time: time.Now().UTC(), Actor: "builder",
		Type: "runtime.register", EntityID: "runtime", Data: runtimeData,
	}); err != nil {
		t.Fatal(err)
	}
	if state.Invocations["invocation"].ConsumerMode != model.ConsumerModeEither {
		t.Fatalf("legacy invocation consumer mode=%q, want EITHER",
			state.Invocations["invocation"].ConsumerMode)
	}
	if state.AgentRuntimes["runtime"].Kind != model.RuntimeKindWorker {
		t.Fatalf("legacy runtime kind=%q, want WORKER", state.AgentRuntimes["runtime"].Kind)
	}
}

func TestInteractiveRuntimeRegistersDefaultAutomaticPolicy(t *testing.T) {
	state := model.State{}
	runtimeData, err := json.Marshal(model.RuntimeRegistered{
		AgentID: "interactive-agent", Connector: "INTERACTIVE", Kind: model.RuntimeKindInteractive, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-runtime-interactive", Time: time.Now().UTC(), Actor: "interactive-agent",
		Type: "runtime.register", EntityID: "interactive-runtime", Data: runtimeData,
	}); err != nil {
		t.Fatal(err)
	}
	policy, ok := state.InvocationPolicies["interactive-agent"]
	if !ok {
		t.Fatal("expected default invocation policy to be set for interactive-agent, got none")
	}
	if policy.Mode != "AUTOMATIC" {
		t.Fatalf("expected policy mode AUTOMATIC, got %q", policy.Mode)
	}
}

func TestTaskTakeoverConsumesOneApproval(t *testing.T) {
	state := model.State{
		Tasks: map[string]model.Task{
			"task-1": {ID: "task-1", Status: "CLAIMED", Owner: "first"},
		},
		Approvals: map[string]model.Approval{
			"approval-b": {ID: "approval-b", Action: "task.takeover:task-1", Status: "APPROVED"},
			"approval-a": {ID: "approval-a", Action: "task.takeover:task-1", Status: "APPROVED"},
			"unrelated":  {ID: "unrelated", Action: "task.takeover:task-2", Status: "APPROVED"},
		},
	}
	data, err := json.Marshal(model.TaskStatus{})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-takeover", Time: time.Now().UTC(), Actor: "second",
		Type: "task.takeover", EntityID: "task-1", Data: data,
	}); err != nil {
		t.Fatal(err)
	}
	if got := state.Approvals["approval-a"].Status; got != "CONSUMED" {
		t.Fatalf("first approved takeover record status = %q, want CONSUMED", got)
	}
	if got := state.Approvals["approval-b"].Status; got != "APPROVED" {
		t.Fatalf("second independently approved takeover record status = %q, want APPROVED", got)
	}
	if got := state.Approvals["unrelated"].Status; got != "APPROVED" {
		t.Fatalf("unrelated approval status = %q, want APPROVED", got)
	}
}

// TestAgentRoleSwitchedOnlyChangesRole is the regression test for RFC
// 0018's core self-service invariant: unlike AgentActivated, applying
// AgentRoleSwitched must never touch Capabilities or Scopes -- a principal
// relabeling its own role cannot use this as a side door to grant itself
// new standing.
func TestAgentRoleSwitchedOnlyChangesRole(t *testing.T) {
	state := model.State{Agents: map[string]model.Agent{
		"builder": {
			ID: "builder", Status: "ACTIVE", Role: model.Role("MEMBER"),
			Capabilities: []string{"go", "test"}, Scopes: []string{"src"},
		},
	}}
	switchData, err := json.Marshal(model.AgentRoleSwitched{Role: model.Role("Tester")})
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyEvent(&state, model.Event{
		ID: "event-switch", Time: time.Now().UTC(), Actor: "builder",
		Type: "agent.switch-role", EntityID: "builder", Data: switchData,
	}); err != nil {
		t.Fatal(err)
	}
	got := state.Agents["builder"]
	if got.Role != "Tester" {
		t.Fatalf("expected role to become Tester, got %q", got.Role)
	}
	if len(got.Capabilities) != 2 || got.Capabilities[0] != "go" || got.Capabilities[1] != "test" {
		t.Fatalf("expected capabilities to be untouched, got %v", got.Capabilities)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "src" {
		t.Fatalf("expected scopes to be untouched, got %v", got.Scopes)
	}
}
