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

// This opt-in test verifies the real native exec adapter through the complete
// claim/execute/publish/complete pipeline in a disposable project.
func TestManualSmokeCodexExec(t *testing.T) {
	if os.Getenv("AGENTCOMMS_CODEX_EXEC_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_CODEX_EXEC_SMOKE=1 to use the configured Codex account")
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	const receipt = "AGC_CODEX_EXEC_SMOKE_OK"
	instance, root := workerServiceWithInstruction(t,
		"Synthetic communication test only. Do not use tools, read files, run commands or delegate. Reply with exactly "+receipt+".")
	// Native Codex requires a Git checkout; keep that provider restriction.
	if err := exec.Command("git", "init", "--quiet", root).Run(); err != nil {
		t.Fatal(err)
	}
	worker, err := New(Config{
		Service: instance, Actor: "claude-axiom", RuntimeID: "runtime-axiom",
		Adapter: "codex", Executable: executable, WorkDir: root,
		Sandbox: "read-only", CodexIgnoreUserConfig: true,
		Model:      os.Getenv("AGENTCOMMS_CODEX_SMOKE_MODEL"),
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
