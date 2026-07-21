package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestManualSmokeCodexACP is a throwaway, human-run verification against
// the real codex-acp package, not part of the normal suite: it runs a live
// invocation through the full Worker pipeline (claim, execute, publish
// result) using whatever Codex account is logged in locally. Skipped
// unless AGENTCOMMS_ACP_SMOKE=1, so it never runs in CI.
func TestManualSmokeCodexACP(t *testing.T) {
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against the real codex-acp package")
	}
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "codex-acp", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: 90 * time.Second, Once: true,
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
