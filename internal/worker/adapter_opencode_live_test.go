package worker

import (
	"context"
	"testing"
	"time"
)

func TestOpenCodeLiveAdapterRejectsModelOverride(t *testing.T) {
	config := &Config{Model: "gpt-5"}
	if err := (openCodeLiveAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error when Model is set")
	}
}

func TestOpenCodeLiveAdapterDefaultsPermissionMode(t *testing.T) {
	config := &Config{}
	if err := (openCodeLiveAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.PermissionMode != "acceptEdits" {
		t.Fatalf("expected default permission mode acceptEdits, got %q", config.PermissionMode)
	}
}

func TestOpenCodeLiveAdapterRejectsBypassPermissions(t *testing.T) {
	config := &Config{PermissionMode: "bypassPermissions"}
	if err := (openCodeLiveAdapter{}).Validate(config); err == nil {
		t.Fatal("expected an error for a permission-bypassing mode")
	}
}

func TestOpenCodeLiveAdapterDoesNotRequireExecutable(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "opencode-live", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" {
		t.Fatalf("expected no executable requirement, got %q", worker.config.Executable)
	}
}

func TestOpenCodeLiveRegisteredUnderDistinctAdapterName(t *testing.T) {
	adapter, err := resolveAdapter("opencode-live")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(openCodeLiveAdapter); !ok {
		t.Fatalf("expected openCodeLiveAdapter, got %T", adapter)
	}
	// opencode-acp must remain untouched and registered under its own name.
	acpAdapter, err := resolveAdapter("opencode-acp")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := acpAdapter.(openCodeACPAdapter); !ok {
		t.Fatalf("expected openCodeACPAdapter under \"opencode-acp\", got %T", acpAdapter)
	}
}

func TestRequiresExecutableExcludesOpenCodeLive(t *testing.T) {
	if RequiresExecutable("opencode-live") {
		t.Fatal("opencode-live drives a persistent server over HTTP, it should not require a resolved Executable")
	}
}

func TestDenyGovernanceOpenCodeAlwaysDenies(t *testing.T) {
	approved, err := (denyGovernanceOpenCode{}).Approve(context.Background(), "session-1", "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("denyGovernanceOpenCode approved a request")
	}
}

func TestOpenCodeLiveSessionRoundTrips(t *testing.T) {
	root := t.TempDir()
	if got := loadOpenCodeLiveSessionID(root, "fixer-runtime-1"); got != "" {
		t.Fatalf("expected no cached session before any save, got %q", got)
	}
	if err := saveOpenCodeLiveSessionID(root, "fixer-runtime-1", "ses_abc123"); err != nil {
		t.Fatal(err)
	}
	if got := loadOpenCodeLiveSessionID(root, "fixer-runtime-1"); got != "ses_abc123" {
		t.Fatalf("expected cached session ses_abc123, got %q", got)
	}
}

func TestOpenCodeLiveSessionIsScopedPerRuntime(t *testing.T) {
	root := t.TempDir()
	if err := saveOpenCodeLiveSessionID(root, "fixer-runtime-1", "ses_one"); err != nil {
		t.Fatal(err)
	}
	if got := loadOpenCodeLiveSessionID(root, "fixer-runtime-2"); got != "" {
		t.Fatalf("expected a different runtime ID to have no cached session, got %q", got)
	}
}
