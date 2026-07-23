package worker

import (
	"testing"
	"time"
)

func TestCodexLiveDefaultsSandbox(t *testing.T) {
	config := &Config{}
	if err := (codexLiveAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.Sandbox != "workspace-write" {
		t.Fatalf("sandbox = %q, want workspace-write", config.Sandbox)
	}
}

func TestCodexLiveRejectsInvalidSandbox(t *testing.T) {
	config := &Config{Sandbox: "danger-full-access"}
	if err := (codexLiveAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error for an unsupported sandbox value")
	}
}

func TestCodexLiveDoesNotRequireSessionID(t *testing.T) {
	// Unlike claude-live, codex-live mints and caches its own thread ID --
	// no upfront --session-id requirement.
	config := &Config{}
	if err := (codexLiveAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
}

func TestCodexLiveIsRegisteredWithoutExecutableRequirement(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "DAMON", RuntimeID: "runtime-damon",
		Adapter: "codex-live", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" || RequiresExecutable("codex-live") {
		t.Fatal("codex-live must resolve Codex inside the broker, not as a CLI adapter")
	}
	if _, ok := worker.adapter.(codexLiveAdapter); !ok {
		t.Fatalf("adapter type = %T, want codexLiveAdapter", worker.adapter)
	}
}

func TestCodexLiveThreadIDCacheRoundTrips(t *testing.T) {
	root := t.TempDir()
	if got := loadCodexLiveThreadID(root, "runtime-one"); got != "" {
		t.Fatalf("expected no cached thread before any save, got %q", got)
	}
	if err := saveCodexLiveThreadID(root, "runtime-one", "th_abc123"); err != nil {
		t.Fatal(err)
	}
	if got := loadCodexLiveThreadID(root, "runtime-one"); got != "th_abc123" {
		t.Fatalf("expected cached thread th_abc123, got %q", got)
	}
}

func TestCodexLiveThreadIDIsScopedPerRuntime(t *testing.T) {
	root := t.TempDir()
	if err := saveCodexLiveThreadID(root, "runtime-one", "th_one"); err != nil {
		t.Fatal(err)
	}
	if got := loadCodexLiveThreadID(root, "runtime-two"); got != "" {
		t.Fatalf("expected a different runtime ID to have no cached thread, got %q", got)
	}
}
