package worker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if _, err := instance.Execute("owner", "agent.activate", "axiom",
		model.AgentActivated{Role: model.RoleAgent, Scopes: []string{"src"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("axiom", "runtime.register", "runtime-axiom",
		model.RuntimeRegistered{AgentID: "axiom", Connector: "MCP", MaxConcurrent: 1}); err != nil {
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
