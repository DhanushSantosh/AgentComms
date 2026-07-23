package worker

import (
	"testing"
	"time"
)

const testClaudeLiveSession = "6fe7b44b-806b-4f2c-a3de-c4ac00ab867a"

func TestClaudeLiveRequiresSessionID(t *testing.T) {
	config := &Config{ClaudeBudgetUSD: 1}
	if err := (claudeLiveAdapter{}).Validate(config); err == nil {
		t.Fatal("expected claude-live to require a restart-safe session ID")
	}
}

func TestClaudeLiveDefaultsPermissionMode(t *testing.T) {
	config := &Config{SessionID: testClaudeLiveSession, ClaudeBudgetUSD: 1}
	if err := (claudeLiveAdapter{}).Validate(config); err != nil {
		t.Fatal(err)
	}
	if config.PermissionMode != "acceptEdits" {
		t.Fatalf("permission mode = %q, want acceptEdits", config.PermissionMode)
	}
}

func TestClaudeLiveIsRegisteredWithoutExecutableRequirement(t *testing.T) {
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		SessionID: testClaudeLiveSession, Adapter: "claude-live", WorkDir: root,
		ClaudeBudgetUSD: 1, ListenWait: time.Second, ExecutionTimeout: time.Minute, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if worker.config.Executable != "" || RequiresExecutable("claude-live") {
		t.Fatal("claude-live must resolve Claude inside the broker, not as a CLI adapter")
	}
	if _, ok := worker.adapter.(claudeLiveAdapter); !ok {
		t.Fatalf("adapter type = %T, want claudeLiveAdapter", worker.adapter)
	}
}
