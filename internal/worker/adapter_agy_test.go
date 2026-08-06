package worker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// TestAgyAdapterArgumentsPermissionModes is the regression test for a real
// gap: validateAgyConfig accepts "acceptEdits", "accept-edits", "auto",
// "dontAsk", "manual", and "plan" as PermissionMode -- including
// defaulting to "acceptEdits" when unset, the single most common case in
// practice -- but Arguments() used to only branch on "auto"/"dontAsk" and
// "plan", silently passing neither --mode nor
// --dangerously-skip-permissions for "acceptEdits", "accept-edits", or
// "manual". The real agy CLI (agy --help) documents --mode as accepting
// exactly "(accept-edits, plan)", so the default, most common invocation
// never actually told agy to auto-accept edits at all. Table-driven over
// every accepted PermissionMode so a future value added to the allowlist
// without a corresponding Arguments() case shows up here too.
func TestAgyAdapterArgumentsPermissionModes(t *testing.T) {
	adapter := agyAdapter{}
	base := Config{
		Actor:          "test-agent",
		Executable:     "/usr/local/bin/agy",
		SessionID:      "conv-12345",
		Model:          "gemini-2.5-pro",
	}

	cases := []struct {
		mode        string
		wantFlags   []string // substrings that must appear, joined by " "
		forbidFlags []string // substrings that must NOT appear
	}{
		{mode: "acceptEdits", wantFlags: []string{"--mode accept-edits"}, forbidFlags: []string{"--dangerously-skip-permissions"}},
		{mode: "accept-edits", wantFlags: []string{"--mode accept-edits"}, forbidFlags: []string{"--dangerously-skip-permissions"}},
		{mode: "auto", wantFlags: []string{"--dangerously-skip-permissions"}, forbidFlags: []string{"--mode"}},
		{mode: "dontAsk", wantFlags: []string{"--dangerously-skip-permissions"}, forbidFlags: []string{"--mode"}},
		{mode: "plan", wantFlags: []string{"--mode plan"}, forbidFlags: []string{"--dangerously-skip-permissions"}},
		{mode: "manual", wantFlags: nil, forbidFlags: []string{"--mode", "--dangerously-skip-permissions"}},
	}

	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			config := base
			config.PermissionMode = tc.mode
			args := adapter.Arguments(config)
			joined := " " + strings.Join(args, " ") + " "

			if !strings.Contains(joined, " --print ") {
				t.Errorf("mode %q: expected --print in args, got %v", tc.mode, args)
			}
			if !strings.Contains(joined, "--output-format text") {
				t.Errorf("mode %q: expected --output-format text in args, got %v", tc.mode, args)
			}
			for _, want := range tc.wantFlags {
				if !strings.Contains(joined, want) {
					t.Errorf("mode %q: expected %q in args, got %v", tc.mode, want, args)
				}
			}
			for _, forbid := range tc.forbidFlags {
				// Exact element match, not substring: "--mode" is itself a
				// substring of "--model", which every case here legitimately
				// carries.
				for _, arg := range args {
					if arg == forbid {
						t.Errorf("mode %q: did not expect %q in args, got %v", tc.mode, forbid, args)
					}
				}
			}
			if !strings.Contains(joined, "--conversation conv-12345") {
				t.Errorf("mode %q: expected --conversation conv-12345 in args, got %v", tc.mode, args)
			}
			if !strings.Contains(joined, "--model gemini-2.5-pro") {
				t.Errorf("mode %q: expected --model gemini-2.5-pro in args, got %v", tc.mode, args)
			}
		})
	}
}

func TestAgyAdapterPrompt(t *testing.T) {
	adapter := agyAdapter{}
	invocation := model.Invocation{
		ID:          "inv-001",
		RequestedBy: "HUMAN",
		Priority:    "NORMAL",
		Instruction: "Check system health",
	}

	prompt := adapter.Prompt("HULK", invocation)

	if !strings.Contains(prompt, "HULK") {
		t.Errorf("expected actor HULK in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Check system health") {
		t.Errorf("expected instruction in prompt, got %q", prompt)
	}
	if !strings.Contains(prompt, "Invocation ID: inv-001") {
		t.Errorf("expected invocation ID in prompt, got %q", prompt)
	}
}

func TestAgyAdapterValidate(t *testing.T) {
	adapter := agyAdapter{}
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "agy")

	config := Config{
		Executable: execPath,
	}

	// Should fail on non-existent executable
	if err := adapter.Validate(&config); err == nil {
		t.Error("expected error for non-existent executable, got nil")
	}
}

func TestAgyAdapterValidateDefaultsPermissionMode(t *testing.T) {
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	config := Config{Executable: execPath}
	if err := (agyAdapter{}).Validate(&config); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.PermissionMode != "acceptEdits" {
		t.Fatalf("expected PermissionMode to default to acceptEdits, got %q", config.PermissionMode)
	}
}

func TestAgyAdapterValidateRejectsBypassPermissionMode(t *testing.T) {
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	config := Config{Executable: execPath, PermissionMode: "bypassPermissions"}
	if err := (agyAdapter{}).Validate(&config); err == nil {
		t.Fatal("expected an error for an unrecognized permission mode, got nil")
	}
}

// TestAgyAdapterValidateRejectsAgentCommsPath is the regression test for a
// real gap: agy's CLI has no per-tool permission-scoping flag equivalent to
// claudeAdapter's --allowedTools "Bash(<path> *)", so validateAgyConfig
// used to silently accept and validate AgentCommsPath as a real executable
// path -- passing --claude-allow-agent-comms alongside --adapter agy looked
// like it worked, but Arguments() never referenced the value at all. It
// must now fail validation outright instead of accepting a flag combination
// that can never do what it implies.
func TestAgyAdapterValidateRejectsAgentCommsPath(t *testing.T) {
	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "agy")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentCommsPath := filepath.Join(tmpDir, "agent-comms")
	if err := os.WriteFile(agentCommsPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	config := Config{Executable: execPath, AgentCommsPath: agentCommsPath}
	err := (agyAdapter{}).Validate(&config)
	if err == nil {
		t.Fatal("expected an error when AgentCommsPath is set for the agy adapter, got nil")
	}
	if !strings.Contains(err.Error(), "per-tool") {
		t.Fatalf("expected the error to explain agy has no per-tool scoping, got: %v", err)
	}
}
