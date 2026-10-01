package service_test

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

const (
	deliveryStatusWaitTimeout  = 5 * time.Second
	deliveryStatusPollInterval = 10 * time.Millisecond
)

func waitForDeliveryStatus(
	t *testing.T,
	instance interface {
		State() (model.State, error)
	},
	deliveryID string,
	expectedStatus string,
) model.State {
	t.Helper()
	deadline := time.Now().Add(deliveryStatusWaitTimeout)
	var lastState model.State
	for time.Now().Before(deadline) {
		state, err := instance.State()
		if err != nil {
			t.Fatal(err)
		}
		lastState = state
		if delivery, exists := state.InvocationDeliveries[deliveryID]; exists &&
			delivery.Status == expectedStatus {
			return state
		}
		time.Sleep(deliveryStatusPollInterval)
	}
	t.Fatalf(
		"delivery %s did not reach %s before timeout; last state: %+v",
		deliveryID,
		expectedStatus,
		lastState.InvocationDeliveries[deliveryID],
	)
	return model.State{}
}

func TestInvocationLifecycle(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "claude-builder", model.PrincipalAgent)
	deadline := time.Now().UTC().Add(time.Hour)

	must(t, instance, "owner", "invocation.request", "inv-1", model.InvocationRequested{
		Target: "claude-builder", Instruction: "Review the current task state",
		ExpectedResult: "Post a concise review", Priority: "high", Deadline: &deadline,
	})
	registerOnlineDeliverableWorker(t, instance, "claude-builder", "runtime-1")
	// setupWithLocalConnector runs a real daemon (testsupport.StartPersonalProject),
	// whose delivery coordinator retries dispatch for pending invocations every
	// 500ms (internal/daemon/run.go's deliveryCoordinatorInterval) -- confirmed
	// live as the cause of a real, intermittent CI failure: once "runtime-1" goes
	// online here, that background goroutine can win the race and commit its own
	// automatic delivery-attempt before this explicit call runs, which the
	// invariant this test isn't exercising (delivery-attempt mutual exclusion is
	// TestInvocationNotificationReservationIsExclusive's job) correctly refuses
	// as a duplicate. Tolerate that outcome and read back whichever delivery ID
	// actually won, rather than assuming this call always wins the race.
	deliveryID := "delivery-1"
	_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-1",
		model.InvocationDeliveryAttempted{
			DeliveryID: deliveryID, RuntimeID: "runtime-1", Transport: "LOCAL_PROCESS",
		})
	if err != nil {
		state, stateErr := instance.State()
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		found := false
		for id, delivery := range state.InvocationDeliveries {
			if delivery.InvocationID == "inv-1" && delivery.RuntimeID == "runtime-1" {
				deliveryID, found = id, true
				break
			}
		}
		if !found {
			t.Fatalf("explicit delivery-attempt failed (%v) and no delivery attempt exists to fall back to", err)
		}
	}
	must(t, instance, "claude-builder", "invocation.claim", "inv-1", model.InvocationClaimed{
		RuntimeID: "runtime-1",
	})
	must(t, instance, "claude-builder", "invocation.start", "inv-1", model.InvocationProgress{
		Summary: "Review started",
	})
	nextAttempt := time.Now().UTC().Add(10 * time.Minute)
	must(t, instance, "claude-builder", "invocation.wait", "inv-1", model.InvocationWaiting{
		Reason: "Waiting for test results", NextAttemptAt: &nextAttempt,
	})
	must(t, instance, "claude-builder", "invocation.resume", "inv-1", model.InvocationProgress{
		Summary: "Test results received",
	})
	must(t, instance, "claude-builder", "invocation.complete", "inv-1", model.InvocationCompleted{
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
	delivery := state.InvocationDeliveries[deliveryID]
	if delivery.Status != "SUCCEEDED" || delivery.InvocationID != "inv-1" {
		t.Fatalf("unexpected delivery projection (id %s): %+v", deliveryID, delivery)
	}
	if err = instance.Verify(0, 0); err != nil {
		t.Fatal(err)
	}
}

func TestInvocationClaimIsExclusive(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "claude-builder", model.PrincipalAgent)
	registerOnlineWorker(t, instance, "claude-builder", "runtime-a", 1)
	registerOnlineWorker(t, instance, "claude-builder", "runtime-b", 1)
	must(t, instance, "owner", "invocation.request", "inv-exclusive", model.InvocationRequested{
		Target: "claude-builder", Instruction: "Perform one exclusive action",
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, runtimeID := range []string{"runtime-a", "runtime-b"} {
		wait.Add(1)
		go func(runtimeID string) {
			defer wait.Done()
			<-start
			_, err := instance.Execute("claude-builder", "invocation.claim", "inv-exclusive", model.InvocationClaimed{
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
	activate(t, instance, "claude-builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-notify", model.InvocationRequested{
		Target: "claude-builder", Instruction: "Wake one runtime",
	})
	registerOnlineDeliverableWorker(t, instance, "claude-builder", "runtime-notify")
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
	// setupWithLocalConnector runs a real daemon (testsupport.StartPersonalProject)
	// whose delivery coordinator retries dispatch for pending invocations every
	// 500ms (internal/daemon/run.go's deliveryCoordinatorInterval) -- confirmed
	// live as a real, intermittent third contender for the same reservation this
	// test's own two goroutines race for. If it wins before either of them starts,
	// both of this test's explicit calls correctly lose too, so successes can be 0
	// here without the real invariant (at most one delivery-attempt reservation
	// ever succeeds for a given invocation/runtime pair) being violated -- that
	// invariant is verified directly from state below instead, counting every
	// reservation regardless of which of the (up to three) callers created it.
	if successes > 1 {
		t.Fatalf("notification reservations succeeded %d times, want at most 1", successes)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	reservations := 0
	for _, delivery := range state.InvocationDeliveries {
		if delivery.InvocationID == "inv-notify" && delivery.RuntimeID == "runtime-notify" {
			reservations++
		}
	}
	if reservations != 1 {
		t.Fatalf("expected exactly one delivery-attempt reservation to exist for inv-notify/runtime-notify, found %d", reservations)
	}
}

func TestInvocationDeliveryFailureDoesNotTerminateObligation(t *testing.T) {
	for _, coordinatorFirst := range []bool{false, true} {
		name := "immediate"
		if coordinatorFirst {
			name = "coordinator-first"
		}
		t.Run(name, func(t *testing.T) {
			instance := setupWithLocalConnector(t)
			outcomePath := os.Getenv("AGENT_COMMS_TEST_CONNECTOR_OUTCOME")
			outcome := "failure"
			if coordinatorFirst {
				outcome = "blocked-failure"
			}
			if err := os.WriteFile(outcomePath, []byte(outcome), 0o600); err != nil {
				t.Fatal(err)
			}
			// Release a blocked connector before daemon cleanup, including on failure.
			t.Cleanup(func() {
				if err := os.WriteFile(outcomePath+".release", []byte("release"), 0o600); err != nil {
					t.Error(err)
				}
			})
			activate(t, instance, "claude-builder", model.PrincipalAgent)
			must(t, instance, "owner", "invocation.request", "inv-dead", model.InvocationRequested{
				Target: "claude-builder", Instruction: "Wake the builder",
			})
			registerOnlineDeliverableWorker(t, instance, "claude-builder", "runtime-dead")
			if coordinatorFirst {
				deadline := time.Now().Add(deliveryStatusWaitTimeout)
				for {
					if _, err := os.Stat(outcomePath + ".started"); err == nil {
						break
					} else if !os.IsNotExist(err) {
						t.Fatal(err)
					}
					if time.Now().After(deadline) {
						t.Fatal("coordinator did not reach the connector gate")
					}
					time.Sleep(deliveryStatusPollInterval)
				}
			}
			// A real daemon retries pending deliveries in the background. Its
			// reservation may precede this manual final attempt, even when the
			// connector outcome was configured before registering the runtime.
			// Retry only that collision, not arbitrary errors or failed assertions.
			deadline := time.Now().Add(deliveryStatusWaitTimeout)
			released := false
			for {
				_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-dead", model.InvocationDeliveryAttempted{
					DeliveryID: "delivery-final", RuntimeID: "runtime-dead",
					Transport: "LOCAL_PROCESS", Manual: true,
				})
				collision := err != nil && strings.Contains(err.Error(), "an unexpired delivery attempt already exists for this runtime")
				if coordinatorFirst && !released {
					if !collision {
						t.Fatalf("expected the coordinator's reservation to reject the first manual attempt, got %v", err)
					}
					if err := os.WriteFile(outcomePath+".release", []byte("release"), 0o600); err != nil {
						t.Fatal(err)
					}
					released = true
				}
				if err == nil {
					break
				}
				if !collision || time.Now().After(deadline) {
					t.Fatalf("manual final delivery-attempt: %v", err)
				}
				time.Sleep(deliveryStatusPollInterval)
			}
			state := waitForDeliveryStatus(t, instance, "delivery-final", "EXHAUSTED")
			if state.Invocations["inv-dead"].Status != "PENDING" {
				t.Fatalf("delivery failure terminated the invocation: %+v", state.Invocations["inv-dead"])
			}
			if delivery := state.InvocationDeliveries["delivery-final"]; !delivery.Manual || delivery.Error == "" {
				t.Fatalf("expected a failed manual final attempt: %+v", delivery)
			}
		})
	}
}

func TestFailedRedeliveryPreservesEarlierSuccessfulEvidence(t *testing.T) {
	instance := setupWithLocalConnector(t)
	activate(t, instance, "claude-builder", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.request", "inv-preserve", model.InvocationRequested{
		Target: "claude-builder", Instruction: "Wake the builder",
	})
	registerOnlineDeliverableWorker(t, instance, "claude-builder", "runtime-delivery")
	// setupWithLocalConnector runs a real daemon whose delivery coordinator
	// retries dispatch for PENDING invocations every 500ms (see
	// TestInvocationLifecycle's identical comment) -- once "runtime-delivery"
	// goes online, that background goroutine can win the race and commit its
	// own automatic delivery-attempt before this explicit call runs. Tolerate
	// that outcome and read back whichever delivery ID actually won, rather
	// than assuming this call always wins. (The second delivery-attempt below
	// is not at risk of the same race: the automatic dispatcher only
	// considers PENDING invocations -- internal/daemon/connectors.go's
	// Dispatch -- and by the time it runs the invocation has already moved to
	// NOTIFIED.)
	successID := "delivery-success"
	_, err := instance.Execute("owner", "invocation.delivery-attempt", "inv-preserve",
		model.InvocationDeliveryAttempted{
			DeliveryID: successID, RuntimeID: "runtime-delivery",
			Transport: "LOCAL_PROCESS",
		})
	if err != nil {
		state, stateErr := instance.State()
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		found := false
		for id, delivery := range state.InvocationDeliveries {
			if delivery.InvocationID == "inv-preserve" && delivery.RuntimeID == "runtime-delivery" {
				successID, found = id, true
				break
			}
		}
		if !found {
			t.Fatalf("explicit delivery-attempt failed (%v) and no delivery attempt exists to fall back to", err)
		}
	}
	waitForDeliveryStatus(t, instance, successID, "SUCCEEDED")
	if err = os.WriteFile(os.Getenv("AGENT_COMMS_TEST_CONNECTOR_OUTCOME"),
		[]byte("failure"), 0o600); err != nil {
		t.Fatal(err)
	}
	must(t, instance, "owner", "invocation.delivery-attempt", "inv-preserve",
		model.InvocationDeliveryAttempted{
			DeliveryID: "delivery-failed", RuntimeID: "runtime-delivery",
			Transport: "LOCAL_PROCESS", Manual: true,
		})
	state := waitForDeliveryStatus(t, instance, "delivery-failed", "EXHAUSTED")
	if state.Invocations["inv-preserve"].Status != "NOTIFIED" {
		t.Fatalf("failed redelivery erased successful notification state: %+v",
			state.Invocations["inv-preserve"])
	}
	if state.InvocationDeliveries[successID].Status != "SUCCEEDED" ||
		len(state.InvocationDeliveries[successID].Evidence) != 1 {
		t.Fatalf("successful evidence was not preserved: %+v",
			state.InvocationDeliveries[successID])
	}
	if state.InvocationDeliveries["delivery-failed"].Status != "EXHAUSTED" {
		t.Fatalf("failed redelivery was not independently closed: %+v",
			state.InvocationDeliveries["delivery-failed"])
	}
}

func TestInvocationRequesterCanCancelActiveWork(t *testing.T) {
	instance := setup(t)
	activate(t, instance, "claude-builder", model.PrincipalAgent)
	activate(t, instance, "claude-requester", model.PrincipalAgent)
	must(t, instance, "owner", "invocation.policy.update", "claude-builder", model.InvocationPolicyUpdated{
		Mode: "AUTOMATIC",
	})
	must(t, instance, "claude-requester", "invocation.request", "inv-cancel", model.InvocationRequested{
		Target: "claude-builder", Instruction: "Work that is no longer needed",
	})
	must(t, instance, "claude-requester", "invocation.cancel", "inv-cancel", model.InvocationRejected{
		Reason: "superseded by a newer request",
	})
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-cancel"].Status != "CANCELLED" {
		t.Fatalf("invocation was not cancelled: %+v", state.Invocations["inv-cancel"])
	}
	if _, err = instance.Execute("claude-builder", "invocation.claim", "inv-cancel",
		model.InvocationClaimed{RuntimeID: "runtime"}); err == nil {
		t.Fatal("cancelled invocation was claimable")
	}
}
