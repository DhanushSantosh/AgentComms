package worker

import (
	"context"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestFollowUpActionRejectsTrailingContent(t *testing.T) {
	const action = `{"target":"claude-damon","instruction":"Verify the synthetic result"}`
	for _, suffix := range []string{` {"target":"other","instruction":"Another action"}`, ` trailing garbage`, ` null`} {
		t.Run(suffix, func(t *testing.T) {
			if _, err := decodeInvocationAction(action + suffix); err == nil {
				t.Fatal("accepted content after the one permitted follow-up action")
			}
		})
	}
	if parsed, err := decodeInvocationAction(action + " \t\r\n"); err != nil || parsed.Target != "claude-damon" {
		t.Fatalf("valid action with trailing whitespace: parsed=%+v err=%v", parsed, err)
	}
}

func TestWorkerMalformedFollowUpDoesNotPublishOrInvoke(t *testing.T) {
	instance, root := workerService(t)
	worker := newTestWorker(t, instance, root)
	worker.run = func(context.Context, model.Invocation) (string, error) {
		return `Synthetic result.
AGENT_COMMS_INVOKE: {"target":"claude-damon","instruction":"Verify the result"} {"target":"other","instruction":"Second action"}`, nil
	}
	if err := worker.Run(context.Background()); err == nil {
		t.Fatal("malformed follow-up should fail the invocation")
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-worker"]
	if invocation.Status != "WAITING" || invocation.Reason == "" || invocation.ResultMessageID != "" || len(state.Invocations) != 1 {
		t.Fatalf("malformed action produced side effects: invocation=%+v invocations=%d", invocation, len(state.Invocations))
	}
}
