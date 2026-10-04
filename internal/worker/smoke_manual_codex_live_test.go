package worker

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/codexserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestManualSmokeCodexLive is intentionally gated because it uses a real
// Codex subscription. It verifies project-scoped broker registration and
// two HTTP prompt turns sharing one persistent provider conversation.
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
	broker := codexserve.NewBroker()
	defer broker.Close()
	server := httptest.NewServer(broker.Handler())
	defer server.Close()
	client, err := codexserve.NewForProject(server.URL, "smoke-project-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	threadID, err := client.Register(ctx, runtimeID, codexserve.ProcessConfig{
		Executable: codex, WorkDir: root, Sandbox: "workspace-write",
		// Keep the user's normal config untouched when the local default
		// model is unavailable to the account used for this optional smoke.
		Model: os.Getenv("AGENTCOMMS_CODEX_SMOKE_MODEL"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if threadID == "" {
		t.Fatal("broker did not bind a provider thread")
	}
	first, err := client.Prompt(ctx, runtimeID, codexPrompt("SMOKE", model.Invocation{
		ID: "inv-one", RequestedBy: "TEST", Priority: "NORMAL",
		Instruction: "Do not use tools or read files. Remember the word BANANA. Reply only with REMEMBERED.",
	}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Prompt(ctx, runtimeID, codexPrompt("SMOKE", model.Invocation{
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
