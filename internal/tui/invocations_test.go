package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// TestDeliveryPipelineChipsMatchRealDeliveryStatusValues is a regression
// test for a bug caught before it shipped: a delivery's own Status is
// "SUCCEEDED" on notify (internal/projection/apply.go's InvocationNotified
// case) -- "NOTIFIED" is the broader invocation's status, a different
// field entirely. The first version of deliveryPipelineChips checked for
// "NOTIFIED" on the delivery, which would have never matched any real
// delivery and always rendered a successful delivery as still pending.
func TestDeliveryPipelineChipsMatchRealDeliveryStatusValues(t *testing.T) {
	p := colors(false)
	succeeded := deliveryPipelineChips(p, model.InvocationDelivery{
		RuntimeID: "runtime-1", Status: "SUCCEEDED",
		Evidence: []model.DeliveryEvidence{{Stage: "PTY_TEXT_ECHOED"}, {Stage: "PTY_ENTER_SENT"}},
	})
	if !strings.Contains(succeeded, "✓notified") {
		t.Fatalf("SUCCEEDED delivery should render notified as done, got: %q", succeeded)
	}
	if strings.Contains(succeeded, "○notify") {
		t.Fatalf("SUCCEEDED delivery should not still show notify as pending, got: %q", succeeded)
	}

	failed := deliveryPipelineChips(p, model.InvocationDelivery{
		RuntimeID: "runtime-1", Status: "FAILED", Error: "target never echoed the sent text back",
	})
	if !strings.Contains(failed, "✕failed") {
		t.Fatalf("FAILED delivery should render a failed chip, got: %q", failed)
	}

	pending := deliveryPipelineChips(p, model.InvocationDelivery{RuntimeID: "runtime-1", Status: "ATTEMPTED"})
	if !strings.Contains(pending, "○notify") {
		t.Fatalf("in-flight ATTEMPTED delivery should render notify as pending, got: %q", pending)
	}
	if !strings.Contains(pending, "○transport") {
		t.Fatalf("an attempt with no evidence yet should render transport as pending, got: %q", pending)
	}
}

// TestInvocationDeliveryDetailsRendersPipelineForNotifiedInvocation
// exercises the full panel render (not just the isolated chip function)
// against a delivery in the real "SUCCEEDED" shape
// internal/projection/apply.go actually produces. Driving this through the
// real daemon connector pipeline just to get a signed SUCCEEDED delivery
// would need a real LOCAL_PROCESS connector subprocess (see
// internal/service/service_test.go's setupWithLocalConnector) -- out of
// proportion for a rendering test, so this constructs model.State directly,
// the same way internal/service/runtime_test.go already does for its own
// delivery-summary tests.
func TestInvocationDeliveryDetailsRendersPipelineForNotifiedInvocation(t *testing.T) {
	s := newTestService(t)
	view, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	claimedAt := time.Now().UTC().Add(-2 * time.Minute)
	view.state = model.State{
		Agents: view.state.Agents,
		Invocations: map[string]model.Invocation{
			"inv-pipeline": {
				ID: "inv-pipeline", Target: "builder", RequestedBy: "owner",
				Instruction: "Review this", Status: "CLAIMED", ClaimedAt: &claimedAt,
			},
		},
		InvocationDeliveries: map[string]model.InvocationDelivery{
			"delivery-pipeline": {
				ID: "delivery-pipeline", InvocationID: "inv-pipeline",
				RuntimeID: "builder-runtime", Transport: "MCP", Attempt: 1, Status: "SUCCEEDED",
				Evidence: []model.DeliveryEvidence{{Stage: "CONNECTOR_ACCEPTED", At: time.Now().UTC()}},
			},
		},
	}
	view.openView("Invocations")
	view.rowFocus = true
	view.invocationList.Refresh(view.state, view.actor)
	rendered := view.View().Content
	for _, expected := range []string{"DELIVERY PIPELINE", "✓notified", "Attempt #1", "Target acknowledged"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("delivery pipeline view missing %q:\n%s", expected, rendered)
		}
	}
}
