package worker

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestManualSmokeClaudeLive is intentionally gated because it uses a real
// Claude subscription. It verifies project-scoped broker registration and
// two HTTP prompt turns sharing one persistent provider conversation.
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
	broker := claudeserve.NewBroker()
	defer broker.Close()
	server := httptest.NewServer(broker.Handler())
	defer server.Close()
	client, err := claudeserve.NewForProject(server.URL, "smoke-project-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	err = client.Register(ctx, runtimeID, claudeserve.ProcessConfig{
		Executable: claude, WorkDir: root, PermissionMode: "dontAsk",
		SystemPrompt: claudeSystemPrompt("SMOKE"), SessionID: uuid.NewString(), MaxBudgetUSD: 0.50,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.Prompt(ctx, runtimeID, claudeUserPrompt(model.Invocation{
		ID: "inv-one", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "Do not use tools or read files. Remember the word BANANA. Reply only with REMEMBERED.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Prompt(ctx, runtimeID, claudeUserPrompt(model.Invocation{
		ID: "inv-two", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "Do not use tools or read files. What word did I ask you to remember? Reply with only that word.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second != "BANANA" {
		t.Fatalf("runtime %s did not preserve context: first=%q second=%q", runtimeID, first, second)
	}
}
