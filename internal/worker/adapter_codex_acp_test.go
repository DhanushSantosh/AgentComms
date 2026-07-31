package worker

import (
	"testing"
	"time"
)

func TestCodexACPAdapterRejectsModelOverride(t *testing.T) {
	config := &Config{Model: "o3"}
	if err := (codexACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error when Model is set")
	}
}

func TestCodexACPAdapterDefaultsSandbox(t *testing.T) {
	config := &Config{}
	if err := (codexACPAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.Sandbox != "workspace-write" {
		t.Fatalf("expected default sandbox workspace-write, got %q", config.Sandbox)
	}
}

func TestCodexACPAdapterRejectsInvalidSandbox(t *testing.T) {
	config := &Config{Sandbox: "danger-full-access"}
	if err := (codexACPAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error for a non-read-only, non-workspace-write sandbox")
	}
}

func TestCodexACPAdapterDoesNotRequireExecutable(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "codex-acp", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" {
		t.Fatalf("expected no executable requirement, got %q", worker.config.Executable)
	}
}

func TestCodexACPRegisteredUnderDistinctAdapterName(t *testing.T) {
	adapter, err := resolveAdapter("codex-acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(codexACPAdapter); !ok {
		t.Fatalf("expected codexACPAdapter, got %T", adapter)
	}
	// The exec-based codex adapter must remain untouched and registered
	// under its own name.
	execAdapter, err := resolveAdapter("codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := execAdapter.(codexAdapter); !ok {
		t.Fatalf("expected codexAdapter under \"codex\", got %T", execAdapter)
	}
}

func TestRequiresExecutableExcludesCodexACP(t *testing.T) {
	if RequiresExecutable("codex-acp") {
		t.Fatal("codex-acp spawns its own process, it should not require a resolved Executable")
	}
}
