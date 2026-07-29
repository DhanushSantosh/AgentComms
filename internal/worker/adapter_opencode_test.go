package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func openCodeAdapterWithOutputs(outputs ...string) openCodeAdapter {
	outputIndex := 0
	return openCodeAdapter{execute: func(
		_ context.Context,
		_ Config,
		_ model.Invocation,
		_ string,
	) (openCodeRunResult, string, error) {
		if outputIndex >= len(outputs) {
			outputIndex = len(outputs) - 1
		}
		output := outputs[outputIndex]
		outputIndex++
		return parseOpenCodeOutput(output)
	}}
}

func testOpenCodeInvocation() model.Invocation {
	return model.Invocation{ID: "inv-1", RequestedBy: "tester", Instruction: "say hi", Priority: "NORMAL"}
}

func TestOpenCodeExecuteExtractsTextResult(t *testing.T) {
	dir := t.TempDir()
	output := `{"type":"step_start","sessionID":"ses_abc"}
{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"PONG"}}
{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}
`
	config := Config{
		Executable: "unused", WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapterWithOutputs(output).Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "PONG" {
		t.Fatalf("got %q, want %q", result, "PONG")
	}
}

func TestOpenCodeExecuteSkipsNonJSONLines(t *testing.T) {
	dir := t.TempDir()
	output := `! permission requested: edit (some/file); auto-rejecting
{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"STILL_OK"}}
{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}
`
	config := Config{
		Executable: "unused", WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapterWithOutputs(output).Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "STILL_OK" {
		t.Fatalf("got %q, want %q", result, "STILL_OK")
	}
}

func TestOpenCodeExecuteReportsPermissionDenial(t *testing.T) {
	dir := t.TempDir()
	output := `{"type":"tool_use","sessionID":"ses_abc","part":{"type":"tool","tool":"bash","state":{"status":"error","error":"The user rejected permission to use this specific tool call."}}}
{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"tool-calls"}}
`
	config := Config{
		Executable: "unused", WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := openCodeAdapterWithOutputs(output).Execute(ctx, config, testOpenCodeInvocation())
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
	output := `{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"First turn.","messageID":"msg_1"}}
{"type":"text","sessionID":"ses_abc","part":{"type":"text","text":"Second turn.","messageID":"msg_2"}}
{"type":"step_finish","sessionID":"ses_abc","part":{"type":"step-finish","reason":"stop"}}
`
	config := Config{
		Executable: "unused", WorkDir: dir, RuntimeID: "runtime-1",
		PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapterWithOutputs(output).Execute(ctx, config, testOpenCodeInvocation())
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
	missingSessionOutput := "Error: Session not found\n"
	freshSessionOutput := `{"type":"text","sessionID":"ses_fresh","part":{"type":"text","text":"FRESH_OK"}}
{"type":"step_finish","sessionID":"ses_fresh","part":{"type":"step-finish","reason":"stop"}}
`
	config := Config{
		Executable: "unused", WorkDir: dir, RuntimeID: "runtime-1",
		SessionID: "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", PermissionMode: "acceptEdits",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := openCodeAdapterWithOutputs(missingSessionOutput, freshSessionOutput).
		Execute(ctx, config, testOpenCodeInvocation())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "FRESH_OK" {
		t.Fatalf("got %q, want %q (expected a retry without --session)", result, "FRESH_OK")
	}
}
