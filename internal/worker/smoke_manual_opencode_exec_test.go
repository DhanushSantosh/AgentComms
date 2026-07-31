package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestManualSmokeOpenCodeExec is a throwaway, human-run verification
// against the real opencode binary's plain exec adapter, not part of the
// normal suite: it runs a live invocation through the full Worker pipeline
// (claim, execute, publish result) using whatever OpenCode provider
// credentials are configured locally. Skipped unless
// AGENTCOMMS_OPENCODE_SMOKE=1, so it never runs in CI.
func TestManualSmokeOpenCodeExec(t *testing.T) {
	if os.Getenv("AGENTCOMMS_OPENCODE_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_OPENCODE_SMOKE=1 to run this against the real opencode binary")
	}
	executable, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode binary not found on PATH")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "opencode", Executable: executable, WorkDir: root,
		// Confirmed live: a denied bash call can push a single response past
		// 90s waiting on the model, even though the common case finishes in
		// 15-35s — generous here since this is a manual smoke test, not CI.
		ListenWait: time.Second, ExecutionTimeout: 150 * time.Second, Once: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	if invocation.Status != "COMPLETED" || invocation.ResultMessageID == "" {
		t.Fatalf("invocation was not completed with evidence: %+v", invocation)
	}
	result := state.Messages[invocation.ResultMessageID]
	t.Logf("result: %s", result.Body)
}
