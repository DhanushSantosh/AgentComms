package acpclient

import (
	"context"
	"os"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// TestManualSmokeClaudeACP is a throwaway, human-run verification against
// the real npm package, not part of the normal suite: it spawns
// npx @agentclientprotocol/claude-agent-acp for real and sends one live
// prompt through whatever Claude account is logged in locally. Skipped
// unless AGENTCOMMS_ACP_SMOKE=1 is set, so it never runs in CI.
func TestManualSmokeClaudeACP(t *testing.T) {
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against the real npm package")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cwd := t.TempDir()

	session, err := Dial(ctx, Config{
		Command:        "npx",
		Args:           []string{"-y", "@agentclientprotocol/claude-agent-acp"},
		Env:            os.Environ(),
		Dir:            cwd,
		Cwd:            cwd,
		AllowEdits:     func() bool { return false },
		Governance:     &fixedApprover{approve: false},
		MaxOutputBytes: 4096,
		SessionMeta: map[string]any{
			"systemPrompt": map[string]any{"append": "You are a smoke test. Follow instructions exactly."},
		},
	}, "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = session.Close() }()
	t.Logf("session id: %s", session.SessionID())

	output, stopReason, err := session.Prompt(ctx, "Reply with exactly the single word: OK")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	t.Logf("stop reason: %s", stopReason)
	t.Logf("output: %q", output)
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("unexpected stop reason: %s", stopReason)
	}
	if output == "" {
		t.Fatal("expected non-empty output")
	}
}
