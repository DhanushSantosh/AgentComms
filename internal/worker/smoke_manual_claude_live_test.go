package worker

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestManualSmokeClaudeLive is intentionally gated because it uses a real
// Claude subscription. It verifies two turns share one persistent process;
// attach behavior is exercised by running `agent-comms claude attach` while
// this test is active.
func TestManualSmokeClaudeLive(t *testing.T) {
	if os.Getenv("AGENTCOMMS_CLAUDE_LIVE_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_CLAUDE_LIVE_SMOKE=1 to run the real Claude live smoke test")
	}
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	runtimeID := "claude-live-smoke-" + uuid.NewString()
	process, err := claudeserve.Start(context.Background(), claudeserve.ProcessConfig{
		Executable: claude, WorkDir: root, PermissionMode: "dontAsk",
		SystemPrompt: claudeSystemPrompt("SMOKE"), SessionID: uuid.NewString(), MaxBudgetUSD: 0.50,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	first, err := process.Send(ctx, claudeUserPrompt(model.Invocation{
		ID: "inv-one", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "Remember the word BANANA. Reply only with REMEMBERED.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := process.Send(ctx, claudeUserPrompt(model.Invocation{
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
