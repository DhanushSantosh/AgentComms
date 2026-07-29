package service_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestInvocationLifecycle(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	deadline := time.Now().UTC().Add(time.Hour)

	must(t, instance, "owner", "invocation.request", "inv-1", model.InvocationRequested{
		Target: "builder", Instruction: "Review the current task state",
		ExpectedResult: "Post a concise review", Priority: "high", Deadline: &deadline,
	})
	registerOnlineDeliverableWorker(t, instance, "builder", "runtime-1")
	_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-1",
		model.InvocationDeliveryAttempted{
			DeliveryID: "delivery-1", RuntimeID: "runtime-1", Transport: "LOCAL_PROCESS",
		})
	if err != nil {
		t.Fatal(err)
	}
	must(t, instance, "builder", "invocation.claim", "inv-1", model.InvocationClaimed{
		RuntimeID: "runtime-1",
	})
	must(t, instance, "builder", "invocation.start", "inv-1", model.InvocationProgress{
		Summary: "Review started",
	})
	nextAttempt := time.Now().UTC().Add(10 * time.Minute)
	must(t, instance, "builder", "invocation.wait", "inv-1", model.InvocationWaiting{
		Reason: "Waiting for test results", NextAttemptAt: &nextAttempt,
	})
	must(t, instance, "builder", "invocation.resume", "inv-1", model.InvocationProgress{
		Summary: "Test results received",
	})
	must(t, instance, "builder", "invocation.complete", "inv-1", model.InvocationCompleted{
		Summary: "Review completed successfully",
	})

	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	invocation := state.Invocations["inv-1"]
	if invocation.Status != "COMPLETED" || invocation.Priority != "HIGH" ||
		invocation.RuntimeID != "runtime-1" || invocation.CompletedAt == nil {
		t.Fatalf("unexpected invocation projection: %+v", invocation)
	}
	delivery := state.InvocationDeliveries["delivery-1"]
	if delivery.Status != "SUCCEEDED" || delivery.InvocationID != "inv-1" {
		t.Fatalf("unexpected delivery projection: %+v", delivery)
	}
	if err = instance.Verify(0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestInvocationClaimIsExclusive(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	registerOnlineWorker(t, instance, "builder", "runtime-a", 1)
	registerOnlineWorker(t, instance, "builder", "runtime-b", 1)
	must(t, instance, "owner", "invocation.request", "inv-exclusive", model.InvocationRequested{
		Target: "builder", Instruction: "Perform one exclusive action",
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, runtimeID := range []string{"runtime-a", "runtime-b"} {
		wait.Add(1)
		go func(runtimeID string) {
			defer wait.Done()
			<-start
			_, err := instance.Execute("builder", "invocation.claim", "inv-exclusive", model.InvocationClaimed{
				RuntimeID: runtimeID,
			})
			results <- err
		}(runtimeID)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("exclusive invocation claims succeeded %d times, want 1", successes)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-exclusive"].Status != "CLAIMED" {
		t.Fatalf("invocation was not claimed: %+v", state.Invocations["inv-exclusive"])
	}
}

func TestInvocationNotificationReservationIsExclusive(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-notify", model.InvocationRequested{
		Target: "builder", Instruction: "Wake one runtime",
	})
	registerOnlineDeliverableWorker(t, instance, "builder", "runtime-notify")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, deliveryID := range []string{"delivery-a", "delivery-b"} {
		wait.Add(1)
		go func(deliveryID string) {
			defer wait.Done()
			<-start
			_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-notify",
				model.InvocationDeliveryAttempted{
					DeliveryID: deliveryID, RuntimeID: "runtime-notify",
					Transport: "LOCAL_PROCESS",
				})
			results <- err
		}(deliveryID)
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("notification reservations succeeded %d times, want 1", successes)
	}
}

func TestInvocationDeliveryFailureDoesNotTerminateObligation(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-dead", model.InvocationRequested{
		Target: "builder", Instruction: "Wake the builder",
	})
	registerOnlineDeliverableWorker(t, instance, "builder", "runtime-dead")
	if err := os.WriteFile(os.Getenv("AGENT_COMMS_TEST_CONNECTOR_EXECUTABLE"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	must(t, instance, "owner", "invocation.delivery-attempt", "inv-dead", model.InvocationDeliveryAttempted{
		DeliveryID: "delivery-final", RuntimeID: "runtime-dead",
		Transport: "LOCAL_PROCESS", Manual: true,
	})
	state, stateErr := instance.State()
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if state.Invocations["inv-dead"].Status != "PENDING" {
		t.Fatalf("delivery failure terminated the invocation: %+v", state.Invocations["inv-dead"])
	}
	if state.InvocationDeliveries["delivery-final"].Status != "EXHAUSTED" {
		t.Fatalf("delivery attempt was not closed: %+v", state.InvocationDeliveries["delivery-final"])
	}
}

func TestFailedRedeliveryPreservesEarlierSuccessfulEvidence(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-preserve", model.InvocationRequested{
		Target: "builder", Instruction: "Wake the builder",
	})
	registerOnlineDeliverableWorker(t, instance, "builder", "runtime-delivery")
	_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-preserve",
		model.InvocationDeliveryAttempted{
			DeliveryID: "delivery-success", RuntimeID: "runtime-delivery",
			Transport: "LOCAL_PROCESS",
		})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("AGENT_COMMS_TEST_CONNECTOR_EXECUTABLE"),
		[]byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	must(t, instance, "owner", "invocation.delivery-attempt", "inv-preserve",
		model.InvocationDeliveryAttempted{
			DeliveryID: "delivery-failed", RuntimeID: "runtime-delivery",
			Transport: "LOCAL_PROCESS", Manual: true,
		})
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-preserve"].Status != "NOTIFIED" {
		t.Fatalf("failed redelivery erased successful notification state: %+v",
			state.Invocations["inv-preserve"])
	}
	if state.InvocationDeliveries["delivery-success"].Status != "SUCCEEDED" ||
		len(state.InvocationDeliveries["delivery-success"].Evidence) != 1 {
		t.Fatalf("successful evidence was not preserved: %+v",
			state.InvocationDeliveries["delivery-success"])
	}
	if state.InvocationDeliveries["delivery-failed"].Status != "EXHAUSTED" {
		t.Fatalf("failed redelivery was not independently closed: %+v",
			state.InvocationDeliveries["delivery-failed"])
	}
}

func TestInvocationRequesterCanCancelActiveWork(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	activate(t, instance, "requester", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.policy.update", "builder", model.InvocationPolicyUpdated{
		Mode: "AUTOMATIC",
	})
	must(t, instance, "requester", "invocation.request", "inv-cancel", model.InvocationRequested{
		Target: "builder", Instruction: "Work that is no longer needed",
	})
	must(t, instance, "requester", "invocation.cancel", "inv-cancel", model.InvocationRejected{
		Reason: "superseded by a newer request",
	})
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-cancel"].Status != "CANCELLED" {
		t.Fatalf("invocation was not cancelled: %+v", state.Invocations["inv-cancel"])
	}
	if _, err = instance.Execute("builder", "invocation.claim", "inv-cancel",
		model.InvocationClaimed{RuntimeID: "runtime"}); err == nil {
		t.Fatal("cancelled invocation was claimable")
	}
}
