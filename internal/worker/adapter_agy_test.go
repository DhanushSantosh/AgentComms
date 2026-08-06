package worker

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestAgyAdapterArguments(t *testing.T) {
	adapter := agyAdapter{}

	config := Config{
		Actor:          "test-agent",
		Executable:     "/usr/local/bin/agy",
		PermissionMode: "auto",
		SessionID:      "conv-12345",
		Model:          "gemini-2.5-pro",
	}

	args := adapter.Arguments(config)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--print") {
		t.Errorf("expected --print in args, got %v", args)
	}
	if !strings.Contains(joined, "--output-format text") {
		t.Errorf("expected --output-format text in args, got %v", args)
	}
	if !strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Errorf("expected --dangerously-skip-permissions in args, got %v", args)
	}
	if !strings.Contains(joined, "--conversation conv-12345") {
		t.Errorf("expected --conversation conv-12345 in args, got %v", args)
	}
	if !strings.Contains(joined, "--model gemini-2.5-pro") {
		t.Errorf("expected --model gemini-2.5-pro in args, got %v", args)
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
