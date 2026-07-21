package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// handoffWorkerService mirrors workerService but lets the caller control the
// invocation instruction, so a manual smoke test can push the agent toward a
// specific behavior (delegate to DAMON, or run a shell command) rather than
// the generic "Review the implementation" workerService always uses.
func handoffWorkerService(t *testing.T, instruction, expectedResult string) (*service.Service, string) {
	t.Helper()
	root := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	instance := service.New(root)
	instance.Store.SetCredentialStore(identity.NewMemoryStore())
	if err := instance.Store.Init("owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Register("axiom", "AXIOM", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Register("damon", "DAMON", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "agent.activate", "axiom",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("axiom", "runtime.register", "runtime-axiom",
		model.RuntimeRegistered{AgentID: "axiom", Connector: "MCP", MaxConcurrent: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "agent.activate", "damon",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "invocation.policy.update", "damon",
		model.InvocationPolicyUpdated{
			Mode: "TRUSTED", TrustedActors: []string{"axiom"},
		}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "invocation.request", "inv-worker",
		model.InvocationRequested{
			Target: "axiom", Instruction: instruction,
			ExpectedResult: expectedResult, Priority: "NORMAL",
		}); err != nil {
		t.Fatal(err)
	}
	return instance, root
}

const handoffInstruction = `Do not perform any verification yourself and do not run any commands. ` +
	`Your only task is to delegate verification of this change to the agent ` +
	`whose exact registered agent ID is the lowercase string "damon" (this is ` +
	`the literal value the target field must contain — not a display name, ` +
	`not any other casing). ` +
	`Follow your operating instructions for creating exactly one follow-up ` +
	`action, with "target":"damon", instruction "Confirm the implementation ` +
	`compiles" and expected_result "Acknowledge receipt". Then end your turn ` +
	`with a short confirmation that you delegated the work.`

func runHandoffSmoke(t *testing.T, adapter string, timeout time.Duration) {
	t.Helper()
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against the real provider")
	}
	instance, root := handoffWorkerService(t, handoffInstruction, "Delegate to DAMON")
	worker, err := New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: adapter, WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: timeout, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runErr := worker.Run(context.Background())
	if runErr != nil {
		t.Logf("worker.Run error (non-fatal, inspecting state anyway): %v", runErr)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	t.Logf("invocation status: %s reason: %s", invocation.Status, invocation.Reason)
	if invocation.ResultMessageID != "" {
		t.Logf("result: %s", state.Messages[invocation.ResultMessageID].Body)
	}

	foundHandoff := false
	for id, inv := range state.Invocations {
		if id == "inv-worker" {
			continue
		}
		t.Logf("follow-up invocation %s: target=%s requestedBy=%s instruction=%q status=%s",
			id, inv.Target, inv.RequestedBy, inv.Instruction, inv.Status)
		if inv.RequestedBy == "axiom" && inv.Target == "damon" {
			foundHandoff = true
		}
	}
	if !foundHandoff {
		t.Errorf("no follow-up invocation from axiom to damon was created")
	}
}

func TestManualSmokeClaudeACPHandoff(t *testing.T) {
	runHandoffSmoke(t, "claude-acp", 90*time.Second)
}

func TestManualSmokeOpenCodeACPHandoff(t *testing.T) {
	runHandoffSmoke(t, "opencode-acp", 90*time.Second)
}

// TestManualSmokeOpenCodeACPExecuteDenied checks that a task genuinely
// needing a shell command fails cleanly rather than silently: acpclient's
// Governance tier (denyGovernance, wired in by every ACP adapter) rejects
// every execute-kind permission request outright, since this codebase has
// no blocking per-tool-call approval primitive to route it through instead.
// Before acpResult's empty-output-after-denial check, the agent producing
// no final text after the denial surfaced as a COMPLETED invocation with a
// meaningless placeholder result; this asserts it now routes to WAITING
// with a reason that names the denial, matching how a failed exec-based
// adapter already behaves.
func TestManualSmokeOpenCodeACPExecuteDenied(t *testing.T) {
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against the real provider")
	}
	instruction := `Run the shell command: echo hello-from-shell` +
		` — then report its exact output. This requires actually executing a` +
		` command, not just reading files.`
	instance, root := handoffWorkerService(t, instruction, "Report the command's output")
	worker, err := New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: "opencode-acp", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: 90 * time.Second, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runErr := worker.Run(context.Background())
	t.Logf("worker.Run error: %v", runErr)
	if runErr == nil {
		t.Fatal("expected the denied permission request to surface as a failed invocation, not a silent success")
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	t.Logf("invocation status: %s reason: %s", invocation.Status, invocation.Reason)
	if invocation.Status != "WAITING" {
		t.Fatalf("expected invocation to route to WAITING, got %s", invocation.Status)
	}
	if !strings.Contains(invocation.Reason, "permission request was denied") {
		t.Fatalf("expected the wait reason to name the denial, got %q", invocation.Reason)
	}
}
