package worker

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/codexserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestManualSmokeCodexLive is intentionally gated because it uses a real
// Codex subscription. It verifies two turns share one persistent process;
// attach behavior is exercised by running `agent-comms codex attach` while
// this test is active.
func TestManualSmokeCodexLive(t *testing.T) {
	if os.Getenv("AGENTCOMMS_CODEX_LIVE_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_CODEX_LIVE_SMOKE=1 to run the real Codex live smoke test")
	}
	codex, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	runtimeID := "codex-live-smoke-" + uuid.NewString()
	process, err := codexserve.Start(context.Background(), codexserve.ProcessConfig{
		Executable: codex, WorkDir: root, Sandbox: "workspace-write",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	t.Logf("runtime %s bound to thread %s", runtimeID, process.ThreadID())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	first, err := process.Send(ctx, codexPrompt("SMOKE", model.Invocation{
		ID: "inv-one", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "Remember the word BANANA. Reply only with REMEMBERED.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := process.Send(ctx, codexPrompt("SMOKE", model.Invocation{
		ID: "inv-two", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "What word did I ask you to remember? Reply with only that word.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second != "BANANA" {
		t.Fatalf("runtime %s did not preserve context: first=%q second=%q", runtimeID, first, second)
	}
}
