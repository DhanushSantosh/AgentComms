package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
)

// A command can commit even when its response is lost. Dispatch sees an
// independent cache snapshot, not the authority's mutable projection. Recovery
// must expire that reservation without skipping its newly recorded backoff.
func TestDispatcherLostReservationResponseRecoversWithBackoff(t *testing.T) {
	now := time.Now().UTC()
	state := dispatcherState()
	state.Agents = map[string]model.Agent{
		"claude-builder": {ID: "claude-builder", Status: "ACTIVE", PrincipalType: model.PrincipalAgent},
	}
	loseResponse := true
	var firstID string
	dispatcher, err := NewDispatcher(dispatcherConfigs(t), func(_ context.Context, _ string, actor, typ, id string, payload any) error {
		normalized, err := protocol.ValidateTransition(state, actor, typ, id, payload, now)
		if err != nil {
			return err
		}
		raw, err := model.EncodePayload(typ, normalized)
		if err != nil {
			return err
		}
		if err := projection.ApplyEvent(&state, model.Event{Actor: actor, Type: typ, EntityID: id, Time: now, Data: raw}); err != nil {
			return err
		}
		if typ == "invocation.delivery-attempt" && loseResponse {
			loseResponse = false
			firstID = normalized.(model.InvocationDeliveryAttempted).DeliveryID
			return context.DeadlineExceeded
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.now = func() time.Time { return now }
	launches := 0
	dispatcher.deliver = func(context.Context, ConnectorConfig, InvocationEnvelope) (DeliveryResult, error) {
		launches++
		return DeliveryResult{Evidence: []model.DeliveryEvidence{{Stage: "CONNECTOR_ACCEPTED", At: now}}}, nil
	}
	dispatch := func() {
		t.Helper()
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot model.State
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		if err := dispatcher.Dispatch(context.Background(), "project", snapshot); err != nil {
			t.Fatal(err)
		}
	}
	dispatch()
	first := state.InvocationDeliveries[firstID]
	if firstID == "" || first.Status != "ATTEMPTED" || first.AttemptUntil == nil || launches != 0 {
		t.Fatalf("lost response: delivery=%+v launches=%d", first, launches)
	}
	now = first.AttemptUntil.Add(-time.Nanosecond)
	dispatch()
	if len(state.InvocationDeliveries) != 1 || launches != 0 {
		t.Fatal("launched a duplicate before the reservation expired")
	}
	now = *first.AttemptUntil
	dispatch()
	first = state.InvocationDeliveries[firstID]
	if first.Status != "FAILED" || first.Error != "delivery attempt lease expired" || first.NextRetryAt == nil {
		t.Fatalf("expired reservation was not recorded: %+v", first)
	}
	if !first.NextRetryAt.Equal(now.Add(deliveryBackoff(first.Attempt))) {
		t.Fatalf("incorrect recovery backoff: %+v", first)
	}
	if len(state.InvocationDeliveries) != 1 || launches != 0 {
		t.Fatalf("expiry skipped backoff: deliveries=%d launches=%d", len(state.InvocationDeliveries), launches)
	}
	now = first.NextRetryAt.Add(-time.Nanosecond)
	dispatch()
	if launches != 0 {
		t.Fatal("retry launched before the recorded backoff")
	}
	now = *first.NextRetryAt
	dispatch()
	if launches != 1 || len(state.InvocationDeliveries) != 2 || state.Invocations["inv-1"].Status != "NOTIFIED" {
		t.Fatalf("recovery: launches=%d deliveries=%+v invocation=%+v", launches, state.InvocationDeliveries, state.Invocations["inv-1"])
	}
}

func TestDispatcherExpiryStillDispatchesUnrelatedInvocation(t *testing.T) {
	now := time.Now().UTC()
	state := dispatcherState()
	other := state.Invocations["inv-1"]
	other.ID = "inv-2"
	state.Invocations[other.ID] = other
	state.InvocationDeliveries["expired"] = model.InvocationDelivery{
		ID: "expired", InvocationID: "inv-1", RuntimeID: "runtime-1",
		Status: "ATTEMPTED", Attempt: 1, AttemptUntil: &now,
	}
	var submitted []submittedCommand
	dispatcher, err := NewDispatcher(dispatcherConfigs(t), func(_ context.Context, _ string, actor, typ, id string, payload any) error {
		submitted = append(submitted, submittedCommand{Actor: actor, EventType: typ, EntityID: id, Payload: payload})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher.now = func() time.Time { return now }
	launches := 0
	dispatcher.launch = func(_ context.Context, _ ConnectorConfig, envelope InvocationEnvelope) error {
		launches++
		if envelope.Invocation.ID != "inv-2" {
			t.Fatalf("launched invocation still in recovery backoff: %s", envelope.Invocation.ID)
		}
		return nil
	}
	if err := dispatcher.Dispatch(context.Background(), "project", state); err != nil {
		t.Fatal(err)
	}
	if launches != 1 || len(submitted) != 3 ||
		submitted[0].EventType != "invocation.delivery-failed" || submitted[0].EntityID != "inv-1" ||
		submitted[1].EventType != "invocation.delivery-attempt" || submitted[1].EntityID != "inv-2" ||
		submitted[2].EventType != "invocation.notify" || submitted[2].EntityID != "inv-2" {
		t.Fatalf("recovery prevented unrelated work: launches=%d submitted=%+v", launches, submitted)
	}
}
