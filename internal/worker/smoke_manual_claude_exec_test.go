package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in test uses the locally authenticated Claude account, a disposable
// project, a bounded budget and a no-tool instruction. Unlike the live broker
// smoke, it exercises signed claim/execute/publish/complete through Worker.
func TestManualSmokeClaudeExec(t *testing.T) {
	if os.Getenv("AGENTCOMMS_CLAUDE_EXEC_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_CLAUDE_EXEC_SMOKE=1 to use the configured Claude account")
	}
	executable, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	const receipt = "AGC_CLAUDE_EXEC_SMOKE_OK"
	instance, root := workerServiceWithInstruction(t,
		"Synthetic communication test only. Do not use tools, read files, run commands or delegate. Reply with exactly "+receipt+".")
	worker, err := New(Config{
		Service: instance, Actor: "claude-axiom", RuntimeID: "runtime-axiom",
		Adapter: "claude", Executable: executable, WorkDir: root,
		PermissionMode: "dontAsk", ClaudeBudgetUSD: 0.50,
		ListenWait: time.Second, ExecutionTimeout: 150 * time.Second, Once: true,
	})
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("invocation lacks completion evidence: %+v", invocation)
	}
	result, exists := state.Messages[invocation.ResultMessageID]
	if !exists || result.From != "claude-axiom" || !strings.Contains(result.Body, receipt) {
		t.Fatal("native execution did not publish the requested receipt under the worker identity")
	}
}
