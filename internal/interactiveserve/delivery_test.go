package interactiveserve

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// These two tests exercise delivery.go's control-socket logic and
// childenv.go's environment filtering directly — both are platform-neutral
// (parameterized on writeFlusher/io.Writer, not a real pty or ConPTY), so
// unlike serve_test.go/serve_windows_test.go's end-to-end tests, these run
// identically on every platform without needing a real terminal at all.

func TestTryDeliverRefusesBusyTargetQuickly(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	tee := newOutputTee()
	if _, err := tee.Write([]byte(busyMarkers[0])); err != nil {
		t.Fatal(err)
	}
	target, err := os.CreateTemp(t.TempDir(), "target")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go handleConn(server, tee, target, &sync.Mutex{})

	started := time.Now()
	if err = json.NewEncoder(client).Encode(Request{Kind: "try-deliver", Message: "wake"}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err = json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.OK || !response.Busy {
		t.Fatalf("expected a busy refusal, got %+v", response)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("busy refusal took too long: %s", elapsed)
	}
}

// TestChildEnvironStripsClaudeSessionInheritance guards the real, live-
// confirmed bug this fixes: Serve's wrapped child inheriting the invoking
// Claude Code session's own CLAUDE_CODE_CHILD_SESSION/SESSION_ID/
// BRIDGE_SESSION_ID and concluding it is itself a subordinate child,
// disabling its own transcript persistence entirely.
func TestChildEnvironStripsClaudeSessionInheritance(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "parent-session-id")
	t.Setenv("CLAUDE_CODE_BRIDGE_SESSION_ID", "session_parentbridge")
	t.Setenv("SOME_UNRELATED_VAR", "keep-me")

	env := childEnviron()
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		for _, stripped := range claudeSessionInheritanceKeys {
			if key == stripped {
				t.Fatalf("expected %s to be stripped, but it was present: %s", stripped, kv)
			}
		}
	}
	found := false
	for _, kv := range env {
		if kv == "SOME_UNRELATED_VAR=keep-me" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected an unrelated environment variable to be preserved")
	}
}

// TestDeliverToPtyPortableWaitsForIdleThenSucceeds and
// TestDeliverToPtyPortableFailsClosedWhenTargetStaysBusy exercise
// deliverToPty directly against a plain bytes.Buffer target, distinct from
// serve_test.go's identically-behaved but real-pty-backed unix tests
// (TestDeliverToPtyWaitsForIdleThenSucceeds/
// TestDeliverToPtyFailsClosedWhenTargetStaysBusy) — those stay unix-only
// since they also exercise a real pty round trip, but deliverToPty's own
// idle/busy-timeout logic needs nothing platform-specific at all, so it's
// worth giving it a caller (and coverage) on every platform via a simple
// io.Writer rather than leaving it exercised only indirectly through
// handleConn's socket-level tests.

func TestDeliverToPtyPortableWaitsForIdleThenSucceeds(t *testing.T) {
	var buf syncedBuffer
	tee := newOutputTee()
	if _, err := tee.Write([]byte(busyMarkers[0])); err != nil {
		t.Fatal(err)
	}
	// A real pty echoes back whatever's written to it, which is what
	// deliverToPty's echo-detection (via tee) relies on — see serve.go's
	// real usage, where master/ptmx is the same fd tee reads from. A plain
	// bytes.Buffer has no such loopback, so this wraps buf with one:
	// everything written also gets mirrored into tee, standing in for a
	// real pty's own echo.
	target := loopbackWriter{buf: &buf, tee: tee}

	deliverDone := make(chan error, 1)
	go func() {
		deliverDone <- deliverToPty(target, tee, "hello", 2*time.Second, 3*time.Second)
	}()

	select {
	case err := <-deliverDone:
		t.Fatalf("expected deliverToPty to wait while busy, but it returned early: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := tee.Write([]byte("\x1b[2Jnow idle")); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-deliverDone:
		if err != nil {
			t.Fatalf("expected deliverToPty to succeed once idle: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deliverToPty never returned after the target went idle")
	}

	if got := buf.String(); got != "hello\r" {
		t.Fatalf("expected deliverToPty to write the message followed by a carriage return, got %q", got)
	}
}

func TestDeliverToPtyPortableFailsClosedWhenTargetStaysBusy(t *testing.T) {
	var buf syncedBuffer
	tee := newOutputTee()
	if _, err := tee.Write([]byte(busyMarkers[0])); err != nil {
		t.Fatal(err)
	}

	err := deliverToPty(&buf, tee, "should not be injected", 300*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("expected deliverToPty to fail closed rather than inject while the target stayed busy")
	}
	if buf.String() != "" {
		t.Fatal("deliverToPty must not have written anything while the target stayed busy")
	}
}

// syncedBuffer is a concurrency-safe io.Writer wrapping a bytes.Buffer, used
// as deliverToPty's target in the two portable tests above (deliverToPty
// runs in its own goroutine there, so the buffer needs its own locking
// rather than relying on the happens-before edge a channel receive would
// give a single-shot call).
type syncedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncedBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncedBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// loopbackWriter mirrors every write into both buf (so the test can inspect
// what was "sent") and tee (so deliverToPty's own echo-detection, which
// reads back from tee, sees it) — standing in for a real pty's natural
// write-then-echo loopback in a platform-neutral test.
type loopbackWriter struct {
	buf *syncedBuffer
	tee *outputTee
}

func (l loopbackWriter) Write(p []byte) (int, error) {
	if _, err := l.tee.Write(p); err != nil {
		return 0, err
	}
	return l.buf.Write(p)
}
