package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// writeFakeOpenCode writes a throwaway shell script standing in for the
// real `opencode` binary, the same pattern
// internal/interactiveserve/serve_test.go uses for a scripted fake child —
// deterministic output instead of a real, slow, network-backed CLI.
func writeFakeOpenCode(t *testing.T, dir, script string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-opencode.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func testOpenCodeInvocation() model.Invocation {
	return model.Invocation{ID: "inv-1", RequestedBy: "tester", Instruction: "say hi", Priority: "NORMAL"}
}

func TestOpenCodeExecuteExtractsTextResult(t *testing.T) {
	dir := t.TempDir()
	script := `echo '{"type":"step_start","sessionID":"ses_abc"}'
echo '{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"PONG"}}'
echo '{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}'
`
	executable := writeFakeOpenCode(t, dir, script)
	config := Config{
		Executable: executable, WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapter{}.Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "PONG" {
		t.Fatalf("got %q, want %q", result, "PONG")
	}
}

func TestOpenCodeExecuteSkipsNonJSONLines(t *testing.T) {
	dir := t.TempDir()
	script := `echo "! permission requested: edit (some/file); auto-rejecting"
echo '{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"STILL_OK"}}'
echo '{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}'
`
	executable := writeFakeOpenCode(t, dir, script)
	config := Config{
		Executable: executable, WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapter{}.Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "STILL_OK" {
		t.Fatalf("got %q, want %q", result, "STILL_OK")
	}
}

func TestOpenCodeExecuteReportsPermissionDenial(t *testing.T) {
	dir := t.TempDir()
	script := `echo '{"type":"tool_use","sessionID":"ses_abc","part":{"type":"tool","tool":"bash","state":{"status":"error","error":"The user rejected permission to use this specific tool call."}}}'
echo '{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"tool-calls"}}'
`
	executable := writeFakeOpenCode(t, dir, script)
	config := Config{
		Executable: executable, WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := openCodeAdapter{}.Execute(ctx, config, testOpenCodeInvocation())
	if err == nil {
		t.Fatal("expected an error after a denied permission request produced no text result")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Fatalf("expected the denied tool name in the error, got: %v", err)
	}
}

// TestOpenCodeExecuteSeparatesDistinctMessages guards a real live-observed
// glitch: separate assistant turns (distinct messageID) arrive as separate
// "text" events with no whitespace of their own between them, so naive
// concatenation reads as one glued-together sentence ("...first.Let me
// run...").
func TestOpenCodeExecuteSeparatesDistinctMessages(t *testing.T) {
	dir := t.TempDir()
	script := `echo '{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"First turn.","messageID":"msg_1"}}'
echo '{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"Second turn.","messageID":"msg_2"}}'
echo '{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}'
`
	executable := writeFakeOpenCode(t, dir, script)
	config := Config{
		Executable: executable, WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapter{}.Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "First turn.\n\nSecond turn."
	if result != want {
		t.Fatalf("got %q, want %q", result, want)
	}
}

// TestOpenCodeExecuteRetriesOnSessionNotFound guards the fallback every
// other OpenCode adapter already has for the same underlying limitation
// (OpenCode mints its own session IDs; there's no way to create one at a
// caller-chosen ID): a stale/invalid --session must not fail the
// invocation outright, it must retry once with no --session at all.
func TestOpenCodeExecuteRetriesOnSessionNotFound(t *testing.T) {
	dir := t.TempDir()
	script := `for arg in "$@"; do
  if [ "$arg" = "--session" ]; then
    echo "Error: Session not found"
    exit 0
  fi
done
echo '{"type":"text","sessionID":"ses_fresh","part":{"type":"text","text":"FRESH_OK"}}'
echo '{"type":"step_finish","sessionID":"ses_fresh","part":{"type":"step-finish","reason":"stop"}}'
`
	executable := writeFakeOpenCode(t, dir, script)
	config := Config{
		Executable: executable, WorkDir: dir, RuntimeID: "runtime-1",
		SessionID: "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapter{}.Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "FRESH_OK" {
		t.Fatalf("got %q, want %q (expected a retry without --session)", result, "FRESH_OK")
	}
}
