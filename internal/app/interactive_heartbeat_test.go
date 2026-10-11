package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// A heartbeat failure must never end the session. This is the regression for
// two live Codex/Claude sessions that were terminated together the moment one
// heartbeat hit "context deadline exceeded" during a daemon swap.
func TestInteractiveHeartbeatLoopSurvivesFailuresAndRecovers(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	var reports []string
	loop := &interactiveHeartbeatLoop{
		beat: func() error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			if calls <= 4 {
				return errors.New("local daemon is unavailable: context deadline exceeded")
			}
			return nil
		},
		interval: 40 * time.Millisecond,
		retry:    5 * time.Millisecond,
		report: func(line string) {
			mu.Lock()
			reports = append(reports, line)
			mu.Unlock()
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { loop.run(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		recovered := len(reports) > 0 && reports[len(reports)-1] == "heartbeat recovered"
		mu.Unlock()
		if recovered {
			break
		}
		select {
		case <-done:
			t.Fatal("heartbeat loop returned on its own; it must only stop when its context ends")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat never recovered; reports=%q", reports)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat loop did not stop after its context was cancelled")
	}
	summary := loop.summary()
	if !strings.Contains(summary, "4 interactive runtime heartbeat(s) failed") || !strings.Contains(summary, "not interrupted") {
		t.Fatalf("summary = %q", summary)
	}
}

// A beat refused only because another one just committed (typically a retry
// after a client-side timeout the authority had already accepted) is proof
// of presence, not a failure.
func TestInteractiveHeartbeatLoopTreatsTooSoonAsLanded(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	loop := &interactiveHeartbeatLoop{
		beat: func() error {
			mu.Lock()
			defer mu.Unlock()
			calls++
			return errors.New("runtime heartbeat interval must be at least 10s")
		},
		interval: 5 * time.Millisecond,
		retry:    time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	loop.run(ctx)
	if loop.summary() != "" {
		t.Fatalf("too-soon rejections were counted as failures: %q", loop.summary())
	}
	mu.Lock()
	defer mu.Unlock()
	if calls == 0 {
		t.Fatal("heartbeat never ran")
	}
}
