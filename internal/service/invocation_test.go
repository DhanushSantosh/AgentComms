package service

import (
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestInvocationLifecycle(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	deadline := time.Now().UTC().Add(time.Hour)

	must(t, instance, "owner", "invocation.request", "inv-1", model.InvocationRequested{
		Target: "builder", Instruction: "Review the current task state",
		ExpectedResult: "Post a concise review", Priority: "high", Deadline: &deadline,
	})
	must(t, instance, "owner", "invocation.notify", "inv-1", model.InvocationNotified{
		DeliveryID: "delivery-1", RuntimeID: "runtime-1", Attempt: 1,
	})
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
	if delivery.Status != "NOTIFIED" || delivery.InvocationID != "inv-1" {
		t.Fatalf("unexpected delivery projection: %+v", delivery)
	}
	if err = instance.Store.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestInvocationClaimIsExclusive(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
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
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-notify", model.InvocationRequested{
		Target: "builder", Instruction: "Wake one runtime",
	})
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, deliveryID := range []string{"delivery-a", "delivery-b"} {
		wait.Add(1)
		go func(deliveryID string) {
			defer wait.Done()
			<-start
			_, err := instance.Execute("owner", "invocation.notify", "inv-notify",
				model.InvocationNotified{DeliveryID: deliveryID, Attempt: 1})
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

func TestInvocationDeliveryDeadLettersOnlyAtAttemptLimit(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-dead", model.InvocationRequested{
		Target: "builder", Instruction: "Wake the builder",
	})

	_, err := instance.Execute("owner", "invocation.delivery-failed", "inv-dead", model.InvocationDeliveryFailed{
		DeliveryID: "delivery-early", Attempt: 1, Error: "runtime unavailable", Final: true,
	})
	if err == nil {
		t.Fatal("delivery dead-lettered before the maximum attempt")
	}
	must(t, instance, "owner", "invocation.notify", "inv-dead", model.InvocationNotified{
		DeliveryID: "delivery-final", Attempt: controlplane.MaxDeliveryAttempts,
	})
	must(t, instance, "owner", "invocation.delivery-failed", "inv-dead", model.InvocationDeliveryFailed{
		DeliveryID: "delivery-final", Attempt: controlplane.MaxDeliveryAttempts,
		Error: "runtime unavailable", Final: true,
	})
	state, stateErr := instance.State()
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if state.Invocations["inv-dead"].Status != "DEAD_LETTER" {
		t.Fatalf("invocation did not dead-letter: %+v", state.Invocations["inv-dead"])
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
