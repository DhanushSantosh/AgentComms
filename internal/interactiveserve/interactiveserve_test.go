package interactiveserve

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireUnixInteractiveTransport(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY transport is not supported on Windows")
	}
}

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

func TestSocketPathIsIndependentOfProcessTempDirectory(t *testing.T) {
	firstTempDirectory := t.TempDir()
	secondTempDirectory := t.TempDir()
	t.Setenv("TMPDIR", firstTempDirectory)
	first := SocketPath("/projects/shared", "DAMON")
	t.Setenv("TMPDIR", secondTempDirectory)
	second := SocketPath("/projects/shared", "DAMON")
	if first != second {
		t.Fatalf("TMPDIR changed the shared socket path: %q vs %q", first, second)
	}
	if strings.HasPrefix(first, firstTempDirectory) || strings.HasPrefix(first, secondTempDirectory) {
		t.Fatalf("shared socket path still depends on a process temp directory: %q", first)
	}
}

func TestSocketPathConfinesUnsafeRuntimeIDToSharedDirectory(t *testing.T) {
	path := SocketPath("/projects/shared", "../../another-user/socket")
	if filepath.Dir(path) != socketRootDir() {
		t.Fatalf("unsafe runtime ID escaped the socket directory: %q", path)
	}
	if strings.Contains(filepath.Base(path), "..") {
		t.Fatalf("unsafe runtime ID remained in socket filename: %q", path)
	}
}

func TestNotifyInvocationCrossesDifferentTempDirectoryEnvironments(t *testing.T) {
	requireUnixInteractiveTransport(t)
	firstTempDirectory := t.TempDir()
	secondTempDirectory := t.TempDir()
	projectRoot := t.TempDir()
	t.Setenv("TMPDIR", firstTempDirectory)
	sockPath := SocketPath(projectRoot, "DAMON")
	listener := listenTestSocket(t, sockPath)
	socketDirectoryInfo, err := os.Stat(filepath.Dir(sockPath))
	if err != nil {
		t.Fatal(err)
	}
	if permissions := socketDirectoryInfo.Mode().Perm(); permissions != 0o700 {
		t.Fatalf("shared socket directory permissions=%#o, want 0700", permissions)
	}
	go serveOneRequest(t, listener, func(req Request) Response {
		echoedAt := time.Now().UTC()
		enterSentAt := echoedAt.Add(time.Millisecond)
		return Response{OK: true, TextEchoedAt: &echoedAt, EnterSentAt: &enterSentAt}
	})

	t.Setenv("TMPDIR", secondTempDirectory)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := NotifyInvocation(ctx, projectRoot, "DAMON", "DAMON", "inv-cross-tmpdir", "PRICE"); err != nil {
		t.Fatalf("delivery failed across different TMPDIR environments: %v", err)
	}
}

func TestAliveReportsFalseForUnknownRuntime(t *testing.T) {
	requireUnixInteractiveTransport(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if Alive(ctx, t.TempDir(), "no-such-runtime") {
		t.Fatal("expected Alive to report false when nothing is listening")
	}
}

func TestDeliverFailsClosedWhenNothingListening(t *testing.T) {
	requireUnixInteractiveTransport(t)
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
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "opencode-runtime")
	listener := listenTestSocket(t, sockPath)
	var gotMessage string
	go serveOneRequest(t, listener, func(req Request) Response {
		gotMessage = req.Message
		echoedAt := time.Now().UTC()
		enterSentAt := echoedAt.Add(time.Millisecond)
		return Response{OK: true, TextEchoedAt: &echoedAt, EnterSentAt: &enterSentAt}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := NotifyInvocation(ctx, dir, "opencode-runtime", "opencode-agent", "inv-42", "codex-runner"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotMessage, "inv-42") || !strings.Contains(gotMessage, "opencode-agent") || !strings.Contains(gotMessage, "codex-runner") {
		t.Fatalf("expected the notification to mention id/target/requester, got: %s", gotMessage)
	}
}

func TestProbeReportsBusyLiveRuntime(t *testing.T) {
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "busy-runtime")
	listener := listenTestSocket(t, sockPath)
	go serveOneRequest(t, listener, func(req Request) Response {
		if req.Kind != "ping" {
			t.Errorf("expected ping request, got %+v", req)
		}
		return Response{OK: true, Busy: true}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	alive, busy := Probe(ctx, dir, "busy-runtime")
	if !alive || !busy {
		t.Fatalf("expected live busy runtime, got alive=%t busy=%t", alive, busy)
	}
}

// --- protocol round-trip -----------------------------------------------

func TestProtocolRoundTrip(t *testing.T) {
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "protocol-round-trip")
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
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "protocol-error")
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

func TestCallHonorsContextDeadlineAfterConnecting(t *testing.T) {
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "deadline")
	listener := listenTestSocket(t, sockPath)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var request Request
		_ = json.NewDecoder(conn).Decode(&request)
		<-time.After(time.Second)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := call(ctx, sockPath, Request{Kind: "ping"}); err == nil {
		t.Fatal("expected the context deadline to interrupt response waiting")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("context deadline was not honored promptly: %s", elapsed)
	}
}

// --- stale-socket detection ----------------------------------------------

func TestListenLocalRefusesWhenAlreadyLive(t *testing.T) {
	requireUnixInteractiveTransport(t)
	dir, err := os.MkdirTemp("/tmp", "agent-comms-interactive-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove temporary socket directory: %v", err)
		}
	})
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
	requireUnixInteractiveTransport(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "stale-runtime")
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

// TestEchoedFallsBackToInvocationIDWhenFullTextIsShredded guards the fix
// added for agy: its TUI redraws long text streams with status/UI elements
// interleaved at points the box-drawing and cursor-movement handling above
// doesn't cover, so a delivered prompt can come back with most of its
// content scrambled by redraw artifacts -- while the "Invocation ID:
// inv-XXXX" line embedded near the top of the prompt (see agyPrompt,
// adapter_agy.go) survives intact. Without this fallback, that would read
// as never-delivered and the caller would keep retrying (or time out and
// refuse to send Enter) even though the target genuinely has the message.
func TestEchoedFallsBackToInvocationIDWhenFullTextIsShredded(t *testing.T) {
	original := "You are the autonomous Agent Comms runtime for agent HULK.\n" +
		"Invocation ID: inv-047\nRequester: owner\nPriority: NORMAL\n\n" +
		"Instruction:\nCheck system health"
	// The rest of the message is unrecognizable after a redraw, but the
	// invocation ID line rendered cleanly.
	shredded := []byte("### some unrelated status redraw with no other overlap at all ###\ninvocationid inv047\n### more redraw noise ###")
	if !echoed(shredded, original) {
		t.Fatal("expected echoed to fall back to matching the invocation ID when the rest of the text was shredded by a redraw")
	}
}

// TestEchoedFallbackHasAFalsePositiveRisk documents, rather than fixes, a
// known tradeoff in the invocation-ID fallback above: because it only
// requires the ID substring to be present somewhere in buf -- not the rest
// of the message, and not that it appeared recently -- a buffer that
// happens to still contain a PREVIOUS delivery's invocation ID (outputTee
// only resets on a screen-clear, not between deliverToPty calls) would
// register as "echoed" for a NEW delivery of a message that mentions that
// same ID, even though the new message was never actually typed at all.
// This test exists so that risk is visible and intentional, not
// rediscovered by surprise: if a stricter fallback ever replaces this one
// (e.g. requiring some minimum overlap with the rest of the text, not just
// the ID), this test should start failing and can be deleted.
func TestEchoedFallbackHasAFalsePositiveRisk(t *testing.T) {
	newMessage := "Invocation ID: inv-047\nInstruction:\nThis text was never actually delivered"
	staleBufferFromAnEarlierDelivery := []byte("some earlier screen content mentioning inv-047 for an unrelated reason")
	if !echoed(staleBufferFromAnEarlierDelivery, newMessage) {
		t.Fatal("known tradeoff regressed: the ID fallback no longer produces this false positive -- update this test's comment, it's no longer describing current behavior")
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
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(sockPath)
	})
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
