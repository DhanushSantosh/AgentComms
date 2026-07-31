package worker

import (
	"context"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestClaudeACPAdapterRejectsModelOverride(t *testing.T) {
	config := &Config{Model: "opus"}
	if err := (claudeACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error when Model is set")
	}
}

func TestClaudeACPAdapterDefaultsPermissionMode(t *testing.T) {
	config := &Config{}
	if err := (claudeACPAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.PermissionMode != "acceptEdits" {
		t.Fatalf("expected default permission mode acceptEdits, got %q", config.PermissionMode)
	}
}

func TestClaudeACPAdapterRejectsBypassPermissions(t *testing.T) {
	config := &Config{PermissionMode: "bypassPermissions"}
	if err := (claudeACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error for a permission-bypassing mode")
	}
}

func TestDenyGovernanceAlwaysDenies(t *testing.T) {
	approved, err := (denyGovernance{}).Approve(context.Background(), "session-1", acpsdk.ToolCallUpdate{ToolCallId: "tc-1"})
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("denyGovernance approved a tool call")
	}
}

func TestClaudeACPAdapterDoesNotRequireExecutable(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "claude-acp", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" {
		t.Fatalf("expected no executable requirement, got %q", worker.config.Executable)
	}
}

func TestRequiresExecutableDistinguishesCLIFromACPAdapters(t *testing.T) {
	cases := map[string]bool{
		"claude":     true,
		"codex":      true,
		"claude-acp": false,
		"unknown":    false,
	}
	for name, want := range cases {
		if got := RequiresExecutable(name); got != want {
			t.Errorf("RequiresExecutable(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestClaudeACPRegisteredUnderDistinctAdapterName(t *testing.T) {
	adapter, err := resolveAdapter("claude-acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(claudeACPAdapter); !ok {
		t.Fatalf("expected claudeACPAdapter, got %T", adapter)
	}
	// The exec-based claude adapter must remain untouched and registered
	// under its own name.
	execAdapter, err := resolveAdapter("claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := execAdapter.(claudeAdapter); !ok {
		t.Fatalf("expected claudeAdapter under \"claude\", got %T", execAdapter)
	}
}
