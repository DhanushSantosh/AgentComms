package daemon

import (
	"context"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func interactiveDispatcherState() model.State {
	return model.State{
		Invocations: map[string]model.Invocation{
			"inv-1": {ID: "inv-1", Target: "codex", Status: "PENDING", Priority: "NORMAL",
				ConsumerMode: model.ConsumerModeEither},
			"inv-2": {ID: "inv-2", Target: "codex", Status: "PENDING", Priority: "NORMAL",
				ConsumerMode: model.ConsumerModeEither},
		},
		InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes: map[string]model.AgentRuntime{
			"codex-live": {ID: "codex-live", AgentID: "codex", Kind: model.RuntimeKindInteractive,
				Connector: "INTERACTIVE", HostID: "host-1", Status: "ONLINE", MaxConcurrent: 1},
		},
	}
}

// A busy or unreachable interactive session is not ready yet. Recording each
// refusal as a failed attempt spent the whole retry budget within ~14
// minutes, so an invocation requested while an agent worked a longer task
// was never delivered at all.
func TestDispatcherLeavesBusyInteractiveSessionPendingWithoutSpendingAttempts(t *testing.T) {
	for _, tc := range []struct {
		name        string
		alive, busy bool
	}{
		{"busy", true, true},
		{"unreachable", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var submitted []submittedCommand
			dispatcher, err := NewDispatcher(nil, func(_ context.Context, _ string, actor, eventType, entityID string, payload any) error {
				submitted = append(submitted, submittedCommand{Actor: actor, EventType: eventType, EntityID: entityID, Payload: payload})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			dispatcher.SetLocalInteractive(t.TempDir(), "host-1")
			probes := 0
			dispatcher.probeInteractive = func(context.Context, string, string) (bool, bool) {
				probes++
				return tc.alive, tc.busy
			}
			delivered := 0
			dispatcher.deliver = func(context.Context, ConnectorConfig, InvocationEnvelope) (DeliveryResult, error) {
				delivered++
				return DeliveryResult{}, nil
			}
			if err = dispatcher.Dispatch(context.Background(), "project", interactiveDispatcherState()); err != nil {
				t.Fatal(err)
			}
			if len(submitted) != 0 || delivered != 0 {
				t.Fatalf("a %s session got delivery activity: submitted=%+v delivered=%d", tc.name, submitted, delivered)
			}
			if probes != 1 {
				t.Fatalf("probes=%d; the runtime should be probed once per pass, not once per invocation", probes)
			}
		})
	}
}

func TestDispatcherDeliversOnceTheInteractiveSessionIsReady(t *testing.T) {
	var submitted []submittedCommand
	dispatcher, err := NewDispatcher(nil, func(_ context.Context, _ string, actor, eventType, entityID string, payload any) error {
		submitted = append(submitted, submittedCommand{Actor: actor, EventType: eventType, EntityID: entityID, Payload: payload})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.SetLocalInteractive(t.TempDir(), "host-1")
	dispatcher.probeInteractive = func(context.Context, string, string) (bool, bool) { return true, false }
	dispatcher.deliver = func(_ context.Context, config ConnectorConfig, envelope InvocationEnvelope) (DeliveryResult, error) {
		if config.Type != "INTERACTIVE" || envelope.Runtime.ID != "codex-live" {
			t.Fatalf("unexpected delivery: config=%+v runtime=%s", config, envelope.Runtime.ID)
		}
		return DeliveryResult{}, nil
	}
	state := interactiveDispatcherState()
	delete(state.Invocations, "inv-2")
	if err = dispatcher.Dispatch(context.Background(), "project", state); err != nil {
		t.Fatal(err)
	}
	if len(submitted) != 2 || submitted[0].EventType != "invocation.delivery-attempt" ||
		submitted[1].EventType != "invocation.notify" {
		t.Fatalf("submitted=%+v, want one attempt and one notify", submitted)
	}
}
