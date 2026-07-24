package interactiveserve

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSocketPathIsDeterministic(t *testing.T) {
	a := SocketPath("/tmp/project", "codex-runner")
	b := SocketPath("/tmp/project", "codex-runner")
	if a != b {
		t.Fatalf("expected SocketPath to be deterministic, got %q vs %q", a, b)
	}
	if SocketPath("/tmp/project", "codex-runner") == SocketPath("/tmp/project", "opencode-runner") {
		t.Fatal("expected different runtime IDs to get different socket paths")
	}
}

func TestAliveReportsFalseForUnknownRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if Alive(ctx, t.TempDir(), "no-such-runtime") {
		t.Fatal("expected Alive to report false when nothing is listening")
	}
}

func TestDeliverFailsClosedWhenNothingListening(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Deliver(ctx, t.TempDir(), "no-such-runtime", "hello"); err == nil {
		t.Fatal("expected Deliver to fail when nothing is listening")
	}
}

func TestDeliverRejectsEmbeddedNewlines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, msg := range []string{"line one\nline two", "carriage\rreturn"} {
		if err := Deliver(ctx, t.TempDir(), "whatever", msg); err == nil {
			t.Fatalf("expected Deliver to reject a message containing a newline: %q", msg)
		}
	}
}

func TestNotifyInvocationMentionsIDAndTarget(t *testing.T) {
	dir := t.TempDir()
	sockPath := SocketPath(dir, "opencode-runner")
	listener := listenTestSocket(t, sockPath)
	var gotMessage string
	go serveOneRequest(t, listener, func(req Request) Response {
		gotMessage = req.Message
		return Response{OK: true}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := NotifyInvocation(ctx, dir, "opencode-runner", "inv-42", "codex-runner"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotMessage, "inv-42") || !strings.Contains(gotMessage, "opencode-runner") || !strings.Contains(gotMessage, "codex-runner") {
		t.Fatalf("expected the notification to mention id/target/requester, got: %s", gotMessage)
	}
}

// --- protocol round-trip -----------------------------------------------

func TestProtocolRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	listener := listenTestSocket(t, sockPath)
	go serveOneRequest(t, listener, func(req Request) Response {
		if req.Kind != "deliver" || req.Message != "ping-pong" {
			t.Errorf("unexpected request: %+v", req)
		}
		return Response{OK: true}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := call(ctx, sockPath, Request{Kind: "deliver", Message: "ping-pong"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("expected ok response, got %+v", resp)
	}
}

func TestProtocolRoundTripSurfacesError(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	listener := listenTestSocket(t, sockPath)
	go serveOneRequest(t, listener, func(req Request) Response {
		return Response{OK: false, Error: "target refused"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := call(ctx, sockPath, Request{Kind: "deliver", Message: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.OK || resp.Error != "target refused" {
		t.Fatalf("expected the server's error to round-trip, got %+v", resp)
	}
}

// --- stale-socket detection ----------------------------------------------

func TestListenLocalRefusesWhenAlreadyLive(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "runtime.sock")
	first, err := listenLocal(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := listenLocal(sockPath); err == nil {
		t.Fatal("expected a second listenLocal against a live socket to refuse")
	}
}

func TestListenLocalRecoversStaleSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "runtime.sock")
	first, err := listenLocal(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed owner: close the listener without removing the
	// socket file, leaving a stale file behind.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := listenLocal(sockPath)
	if err != nil {
		t.Fatalf("expected listenLocal to recover a stale socket left by a dead process: %v", err)
	}
	defer second.Close()
}

// --- matcher: busy/echo heuristics ---------------------------------------

func TestIsBusyDetectsAndIgnoresMarkers(t *testing.T) {
	if isBusy([]byte("nothing interesting here")) {
		t.Fatal("expected no false positive on unrelated text")
	}
	for _, marker := range busyMarkers {
		if !isBusy([]byte("some UI chrome " + marker + " more chrome")) {
			t.Fatalf("expected isBusy to detect marker %q", marker)
		}
	}
}

func TestIsBusyOnlyLooksAtRecentTail(t *testing.T) {
	old := strings.Repeat("x", busyTailBytes+500) + busyMarkers[0]
	fresh := []byte(old + strings.Repeat("y", busyTailBytes+500))
	if isBusy(fresh) {
		t.Fatal("expected an old marker far outside the tail window to not count as busy")
	}
}

func TestNormalizeForMatchIsCaseInsensitiveAndStripsPunctuation(t *testing.T) {
	if got, want := normalizeForMatch(`Hello, "World"! (42)`), "helloworld42"; got != want {
		t.Fatalf("normalizeForMatch(%q) = %q, want %q", `Hello, "World"! (42)`, got, want)
	}
}

// TestEchoedSurvivesBoxDrawingWrapPrefix guards the fix for a failure
// confirmed live against the tmux-backed design: OpenCode's compose box
// renders a "┃" border character at the start of every wrapped line, and a
// long delivered message reliably wraps across several lines in any
// reasonably-sized pane. A plain whitespace-collapsed comparison treated
// "┃" as ordinary content and it ended up interleaved into the captured
// text at every wrap point, breaking the substring match even though the
// message had, in fact, been delivered correctly. The raw-pty design
// forwards the same real, wrapping rendering verbatim, so this risk is
// exactly as real here as it was there.
func TestEchoedSurvivesBoxDrawingWrapPrefix(t *testing.T) {
	original := `Agent Comms: new invocation "verify-fix-3" is pending for you (requested by owner). Run agent-comms invocation list --status PENDING --to opencode-runner --json to see it, then handle it per your existing protocol.`
	wrapped := []byte("        ┃  Agent Comms: new invocation \"verify-fix-3\" is pending for you (\n" +
		"        ┃  requested by owner). Run agent-comms invocation list --status PENDING\n" +
		"        ┃  --to opencode-runner --json to see it, then handle it per your\n" +
		"        ┃  existing protocol.")
	if !echoed(wrapped, original) {
		t.Fatalf("expected the box-drawing-wrapped rendering to still match the original message")
	}
}

func TestEchoedIsFalseWhenTextNeverAppeared(t *testing.T) {
	if echoed([]byte("totally unrelated content"), "the delivered message") {
		t.Fatal("expected echoed to report false when the text never appeared")
	}
}

// TestEchoedSurvivesInterleavedCursorMovement guards the fix for a failure
// confirmed live against real codex: a full-screen TUI can redraw the same
// logical text across several separate writes with cursor-repositioning
// escape sequences interspersed between individual characters. Those
// sequences routinely contain digits and letters (parameter and final
// command bytes) that survive a plain "keep only letters and digits"
// filter and get treated as content, fragmenting what should be one
// contiguous run of real text — even though the text is, in fact, present
// and correctly rendered. Delivery against a trivial wrapped command (cat)
// worked immediately; delivery against real codex failed with exactly this
// symptom until ANSI sequences were stripped as whole units first.
func TestEchoedSurvivesInterleavedCursorMovement(t *testing.T) {
	// "hello" typed one character at a time, each followed by a cursor-move
	// sequence containing a digit and a letter (\x1b[5C = move right 5).
	wrapped := "h\x1b[5Ce\x1b[5Cl\x1b[5Cl\x1b[5Co\x1b[5C"
	if !echoed([]byte(wrapped), "hello") {
		t.Fatal("expected echoed to survive text interleaved with cursor-movement escape sequences")
	}
}

func TestIsBusyIgnoresColorCodesAroundMarker(t *testing.T) {
	styled := "\x1b[90m" + busyMarkers[0] + "\x1b[0m"
	if !isBusy([]byte(styled)) {
		t.Fatal("expected isBusy to detect a marker wrapped in color escape codes")
	}
}

// --- outputTee: the new risk this design introduces (raw tee vs tmux's own
// rendered screen grid) ---------------------------------------------------

func TestOutputTeeAccumulatesAndCaps(t *testing.T) {
	tee := newOutputTee()
	if _, err := tee.Write([]byte("hello ")); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if got := string(tee.snapshot()); got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
	big := strings.Repeat("z", outputTeeCap+1000)
	if _, err := tee.Write([]byte(big)); err != nil {
		t.Fatal(err)
	}
	if len(tee.snapshot()) > outputTeeCap {
		t.Fatalf("expected the tee to stay capped at %d bytes, got %d", outputTeeCap, len(tee.snapshot()))
	}
}

// TestOutputTeeResetsOnScreenClear guards the mitigation for a failure mode
// the tmux-backed design never had to worry about: tmux's capture-pane
// already reflects the post-clear, currently-visible screen. Teeing the raw
// byte stream instead means stale, already-cleared content could otherwise
// still sit in the tail buffer and produce a false busy/echo match after
// the target has genuinely redrawn a fresh, unrelated screen.
func TestOutputTeeResetsOnScreenClear(t *testing.T) {
	tee := newOutputTee()
	if _, err := tee.Write([]byte("stale content that should be purged")); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write([]byte("\x1b[2Jfresh screen content")); err != nil {
		t.Fatal(err)
	}
	got := string(tee.snapshot())
	if strings.Contains(got, "stale content") {
		t.Fatalf("expected pre-clear content to be purged, got %q", got)
	}
	if !strings.Contains(got, "fresh screen content") {
		t.Fatalf("expected post-clear content to survive, got %q", got)
	}
}

func TestOutputTeeResetsOnAlternateScreenBuffer(t *testing.T) {
	tee := newOutputTee()
	if _, err := tee.Write([]byte("primary screen chatter")); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write([]byte("\x1b[?1049halternate screen redraw")); err != nil {
		t.Fatal(err)
	}
	got := string(tee.snapshot())
	if strings.Contains(got, "primary screen chatter") {
		t.Fatalf("expected content before entering the alternate screen buffer to be purged, got %q", got)
	}
	if !strings.Contains(got, "alternate screen redraw") {
		t.Fatalf("expected the new screen's content to survive, got %q", got)
	}
}

// --- test helpers ---------------------------------------------------------

func listenTestSocket(t *testing.T, sockPath string) net.Listener {
	t.Helper()
	listener, err := listenLocal(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

// serveOneRequest accepts exactly one connection, decodes one Request,
// hands it to handle, and writes back the Response. Errors are reported via
// t.Error since this runs in a goroutine.
func serveOneRequest(t *testing.T, listener net.Listener, handle func(Request) Response) {
	t.Helper()
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		t.Errorf("decode request: %v", err)
		return
	}
	if err := json.NewEncoder(conn).Encode(handle(req)); err != nil {
		t.Errorf("encode response: %v", err)
	}
}
