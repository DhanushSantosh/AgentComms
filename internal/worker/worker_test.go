package worker

import (
	"context"
	"errors"
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

func TestWorkerExecutesPublishesAndCompletesInvocation(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	worker.run = func(context.Context, model.Invocation) (string, error) {
		return "Reviewed the implementation and verified the requested behavior.", nil
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	if invocation.Status != "COMPLETED" || invocation.ResultMessageID == "" {
		t.Fatalf("invocation was not completed with evidence: %+v", invocation)
	}
	result, exists := state.Messages[invocation.ResultMessageID]
	if !exists || result.From != "axiom" || len(result.To) != 1 || result.To[0] != "owner" {
		t.Fatalf("unexpected worker result message: %+v", result)
	}
}

func TestWorkerMovesFailedExecutionToWaiting(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	worker.run = func(context.Context, model.Invocation) (string, error) {
		return "", errors.New("agent process exited")
	}
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("failed autonomous execution returned success")
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if invocation := state.Invocations["inv-worker"]; invocation.Status != "WAITING" ||
		invocation.Reason == "" {
		t.Fatalf("failed invocation was not moved to waiting: %+v", invocation)
	}
}

func TestWorkerCreatesStructuredFollowUpInvocation(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	worker.run = func(context.Context, model.Invocation) (string, error) {
		return `Handing verification to DAMON.
AGENT_COMMS_INVOKE: {"target":"damon","instruction":"Verify the result","expected_result":"Return an acknowledgement","priority":"NORMAL","expires_in_seconds":600}`, nil
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, invocation := range state.Invocations {
		if invocation.RequestedBy == "axiom" && invocation.Target == "damon" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("worker did not create the structured follow-up invocation")
	}
}

func TestWorkerRejectsUnsafeAgentConfiguration(t *testing.T) {
	instance, root := workerService(t)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: "claude", Executable: executable, WorkDir: root,
		PermissionMode: "bypassPermissions", ClaudeBudgetUSD: 1,
		ListenWait: time.Second, ExecutionTimeout: time.Minute,
	})
	if err == nil {
		t.Fatal("unsafe Claude permission bypass was accepted")
	}
}

func TestWorkerResumesBoundClaudeSession(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	worker.config.SessionID = "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475"
	worker.config.AgentCommsPath = worker.config.Executable
	arguments := worker.arguments()
	assertArgumentsContain(t, arguments, "--resume", worker.config.SessionID)
	assertArgumentsContain(t, arguments, "--allowedTools", "Bash("+worker.config.Executable+" *)")
	assertArgumentsExclude(t, arguments, "--no-session-persistence")
}

func TestWorkerResumesBoundCodexSession(t *testing.T) {
	instance, root := workerService(t)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		SessionID: "019e5408-3ef4-7db3-b584-03ad8f399199",
		Adapter:   "codex", Executable: executable, WorkDir: root,
		Sandbox: "workspace-write", ListenWait: time.Second,
		CodexAddDirs: []string{root}, CodexIgnoreUserConfig: true,
		ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := worker.arguments()
	assertArgumentsContain(t, arguments, "resume", worker.config.SessionID)
	assertArgumentsContain(t, arguments, "--add-dir", root)
	assertArgumentsContain(t, arguments, "--ignore-user-config")
	assertArgumentsExclude(t, arguments, "--ephemeral")
}

func TestWorkerClaudeCarriesRuntimeFramingOnSystemPrompt(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	arguments := worker.arguments()
	found := false
	for index, argument := range arguments {
		if argument != "--append-system-prompt" {
			continue
		}
		found = true
		if index+1 >= len(arguments) {
			t.Fatal("--append-system-prompt is missing its value")
		}
		systemPrompt := arguments[index+1]
		if !strings.Contains(systemPrompt, "axiom") || !strings.Contains(systemPrompt, actionLinePrefix) {
			t.Fatalf("system prompt missing expected runtime framing: %q", systemPrompt)
		}
	}
	if !found {
		t.Fatal("claude worker did not append a system prompt")
	}
}

func TestWorkerCodexOmitsAppendSystemPrompt(t *testing.T) {
	instance, root := workerService(t)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: "codex", Executable: executable, WorkDir: root,
		Sandbox: "workspace-write", ListenWait: time.Second,
		ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertArgumentsExclude(t, worker.arguments(), "--append-system-prompt")
}

func TestClaudeUserPromptOmitsRuntimeFraming(t *testing.T) {
	invocation := model.Invocation{ID: "inv-1", RequestedBy: "owner", Priority: "NORMAL", Instruction: "Do the thing"}
	prompt := claudeUserPrompt(invocation)
	if strings.Contains(prompt, "autonomous Agent Comms runtime") || strings.Contains(prompt, actionLinePrefix) {
		t.Fatalf("claude user prompt leaked runtime framing: %q", prompt)
	}
	if !strings.Contains(prompt, "inv-1") || !strings.Contains(prompt, "Do the thing") {
		t.Fatalf("claude user prompt missing invocation details: %q", prompt)
	}
}

func TestWorkerRejectsUnknownAdapter(t *testing.T) {
	instance, root := workerService(t)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: "opencode", Executable: executable, WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute,
	})
	if err == nil {
		t.Fatal("unregistered adapter was accepted")
	}
}

func TestWorkerRejectsInvalidSessionID(t *testing.T) {
	instance, root := workerService(t)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		SessionID: "most-recent", Adapter: "claude", Executable: executable,
		WorkDir: root, PermissionMode: "acceptEdits", ClaudeBudgetUSD: 1,
		ListenWait: time.Second, ExecutionTimeout: time.Minute,
	})
	if err == nil {
		t.Fatal("ambiguous session selector was accepted")
	}
}

func assertArgumentsContain(t *testing.T, arguments []string, expected ...string) {
	t.Helper()
	for index := 0; index <= len(arguments)-len(expected); index++ {
		matches := true
		for offset := range expected {
			if arguments[index+offset] != expected[offset] {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("arguments %v do not contain %v", arguments, expected)
}

func assertArgumentsExclude(t *testing.T, arguments []string, excluded string) {
	t.Helper()
	for _, argument := range arguments {
		if argument == excluded {
			t.Fatalf("arguments %v contain excluded value %q", arguments, excluded)
		}
	}
}

func newTestWorker(t *testing.T, instance *service.Service, root string) *Worker {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(Config{
		Service: instance, Actor: "axiom", RuntimeID: "runtime-axiom",
		Adapter: "claude", Executable: executable, WorkDir: root,
		PermissionMode: "acceptEdits", ClaudeBudgetUSD: 1,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func workerService(t *testing.T) (*service.Service, string) {
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
			Target: "axiom", Instruction: "Review the implementation",
			ExpectedResult: "Post a verified result", Priority: "NORMAL",
		}); err != nil {
		t.Fatal(err)
	}
	return instance, root
}
