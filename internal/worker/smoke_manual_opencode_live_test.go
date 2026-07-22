package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestManualSmokeOpenCodeLive is a throwaway, human-run verification
// against a real, persistent opencode serve instance, not part of the
// normal suite: it runs a live invocation through the full Worker
// pipeline (claim, execute, publish result) and confirms the session is
// genuinely watchable afterward via the server's own REST API — the
// actual test of the "live broadcast" this adapter exists for. Skipped
// unless AGENTCOMMS_ACP_SMOKE=1, so it never runs in CI, and leaves a
// real opencode serve process running afterward (by design — that's the
// whole point) for manual browser inspection if desired.
func TestManualSmokeOpenCodeLive(t *testing.T) {
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against a real opencode serve instance")
	}
	instance, root := workerService(t)
	worker, err := New(Config{
		Service: instance, Actor: "AXIOM", RuntimeID: "runtime-axiom",
		Adapter: "opencode-live", WorkDir: root,
		ListenWait: time.Second, ExecutionTimeout: 90 * time.Second, Once: true,
		Status: func(s string) { t.Log("status:", s) },
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
