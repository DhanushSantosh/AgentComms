// Mirrors run.go's own !js constraint -- the code under test here does not
// exist in a js/wasm build.
//go:build !js

package daemon

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeliveryCoordinatorKeepsSynchronizingAfterTransientFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := make(chan string, 2)
	stopped := make(chan struct{})
	go func() {
		runDeliveryCoordinator(ctx, "project", time.Millisecond, func(_ context.Context, projectID string) error {
			select {
			case calls <- projectID:
			default:
			}
			return errors.New("temporary authority outage")
		})
		close(stopped)
	}()

	for call := 0; call < 2; call++ {
		select {
		case projectID := <-calls:
			if projectID != "project" {
				t.Fatalf("coordinator synchronized project %q", projectID)
			}
		case <-time.After(time.Second):
			t.Fatal("delivery coordinator stopped after a transient failure")
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("delivery coordinator did not stop after cancellation")
	}
}
