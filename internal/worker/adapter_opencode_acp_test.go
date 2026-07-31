package worker

import (
	"testing"
	"time"
)

func TestOpenCodeACPAdapterRejectsModelOverride(t *testing.T) {
	config := &Config{Model: "gpt-5"}
	if err := (openCodeACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error when Model is set")
	}
}

func TestOpenCodeACPAdapterDefaultsPermissionMode(t *testing.T) {
	config := &Config{}
	if err := (openCodeACPAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.PermissionMode != "acceptEdits" {
		t.Fatalf("expected default permission mode acceptEdits, got %q", config.PermissionMode)
	}
}

func TestOpenCodeACPAdapterRejectsBypassPermissions(t *testing.T) {
	config := &Config{PermissionMode: "bypassPermissions"}
	if err := (openCodeACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error for a permission-bypassing mode")
	}
}

func TestOpenCodeACPAdapterDoesNotRequireExecutable(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "opencode-acp", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" {
		t.Fatalf("expected no executable requirement, got %q", worker.config.Executable)
	}
}

func TestOpenCodeACPRegisteredUnderDistinctAdapterName(t *testing.T) {
	adapter, err := resolveAdapter("opencode-acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(openCodeACPAdapter); !ok {
		t.Fatalf("expected openCodeACPAdapter, got %T", adapter)
	}
}

func TestRequiresExecutableExcludesOpenCodeACP(t *testing.T) {
	if RequiresExecutable("opencode-acp") {
		t.Fatal("opencode-acp spawns its own process, it should not require a resolved Executable")
	}
}
