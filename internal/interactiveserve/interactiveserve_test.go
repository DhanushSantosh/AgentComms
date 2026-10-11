package interactiveserve

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// requireUnixSocketFilesystemSemantics skips tests that depend on unix
// domain sockets being ordinary filesystem entries (a real, statable
// directory with POSIX permission bits; a path that can be a "stale
// regular file" left behind). Named pipes, Windows' equivalent transport
// (protocol_windows.go), share none of that: they live in the kernel's
// `\\.\pipe\` namespace, not the filesystem, so these specific scenarios
// don't translate. Most of this file's transport tests are NOT guarded by
// this â€” they exercise listenLocal/dialLocal generically via SocketPath
// and run unmodified on both platforms, now that Serve/interactive-serve
// itself works on Windows too (see serve_windows.go).
func requireUnixSocketFilesystemSemantics(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this scenario is specific to unix domain sockets' filesystem semantics; Windows named pipes have no equivalent")
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

// TestSocketPathConfinesUnsafeRuntimeIDToSharedDirectory confirms an unsafe
// runtime ID (path traversal characters, in this case) never survives into
// the returned control address literally â€” it's always hashed first
// (safeRuntimeComponent). What "confined" means differs by platform: on
// unix, SocketPath returns a real filesystem path, so confinement means it
// stays nested under socketRootDir(); on Windows, it returns a `\\.\pipe\`
// address, which was never a filesystem path to begin with (see
// socket_address_windows.go) â€” there, confinement means the traversal
// characters simply don't appear in the pipe name, which
// safeRuntimeComponent already guarantees platform-independently.
func TestSocketPathConfinesUnsafeRuntimeIDToSharedDirectory(t *testing.T) {
	path := SocketPath("/projects/shared", "../../another-user/socket")
	if strings.Contains(path, "..") {
		t.Fatalf("unsafe runtime ID's traversal characters survived into the control address: %q", path)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(path, `\\.\pipe\agent-comms-interactive-`) {
			t.Fatalf("expected a well-formed named pipe address, got %q", path)
		}
		return
	}
	if filepath.Dir(path) != socketRootDir() {
		t.Fatalf("unsafe runtime ID escaped the socket directory: %q", path)
	}
}

func TestNotifyInvocationCrossesDifferentTempDirectoryEnvironments(t *testing.T) {
	requireUnixSocketFilesystemSemantics(t)
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
	if _, err := NotifyInvocationWithEvidence(ctx, projectRoot, "DAMON", "DAMON", "inv-cross-tmpdir", "PRICE"); err != nil {
		t.Fatalf("delivery failed across different TMPDIR environments: %v", err)
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
	if _, err := TryDeliverWithEvidence(ctx, t.TempDir(), "no-such-runtime", "hello"); err == nil {
		t.Fatal("expected TryDeliverWithEvidence to fail when nothing is listening")
	}
}

func TestDeliverRejectsEmbeddedNewlines(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, msg := range []string{"line one\nline two", "carriage\rreturn"} {
		if _, err := TryDeliverWithEvidence(ctx, t.TempDir(), "whatever", msg); err == nil {
			t.Fatalf("expected TryDeliverWithEvidence to reject a message containing a newline: %q", msg)
		}
	}
}

func TestNotifyInvocationMentionsIDAndTarget(t *testing.T) {
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
	if _, err := NotifyInvocationWithEvidence(ctx, dir, "opencode-runtime", "opencode-agent", "inv-42", "codex-runner"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotMessage, "inv-42") || !strings.Contains(gotMessage, "opencode-agent") || !strings.Contains(gotMessage, "codex-runner") {
		t.Fatalf("expected the notification to mention id/target/requester, got: %s", gotMessage)
	}
}

func TestProbeReportsBusyLiveRuntime(t *testing.T) {
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

func TestSnapshotReturnsLivePTYBuffer(t *testing.T) {
	dir := t.TempDir()
	sockPath := SocketPath(dir, "snapshot-test")
	listener := listenTestSocket(t, sockPath)
	go serveOneRequest(t, listener, func(req Request) Response {
		if req.Kind != "snapshot" {
			return Response{OK: false, Error: "unexpected request kind"}
		}
		return Response{OK: true, OutputSnapshot: "live terminal output snapshot"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	snapshot, err := Snapshot(ctx, dir, "snapshot-test")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot != "live terminal output snapshot" {
		t.Fatalf("got snapshot %q, want %q", snapshot, "live terminal output snapshot")
	}
}

func TestProtocolRoundTripSurfacesError(t *testing.T) {
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
	requireUnixSocketFilesystemSemantics(t)
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

func TestListenLocalRecoversStaleRegularFile(t *testing.T) {
	requireUnixSocketFilesystemSemantics(t)
	dir := t.TempDir()
	sockPath := SocketPath(dir, "stale-file-runtime")
	if err := os.WriteFile(sockPath, []byte("stale content"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := listenLocal(sockPath)
	if err != nil {
		t.Fatalf("expected listenLocal to recover a stale non-socket file: %v", err)
	}
	defer second.Close()
}

// --- matcher: busy/echo decisions against the rendered screen -------------

func TestScreenBusyDetectsAndIgnoresMarkers(t *testing.T) {
	if screenBusy(screenOf(t, 80, 24, "nothing interesting here")) {
		t.Fatal("expected no false positive on unrelated text")
	}
	for _, marker := range busyMarkers {
		if !screenBusy(screenOf(t, 80, 24, "some UI chrome "+marker+" more chrome")) {
			t.Fatalf("expected screenBusy to detect marker %q", marker)
		}
	}
}

func TestScreenBusyIgnoresColorCodesAroundMarker(t *testing.T) {
	if !screenBusy(screenOf(t, 80, 24, "\x1b[90m"+busyMarkers[0]+"\x1b[0m")) {
		t.Fatal("expected screenBusy to detect a marker wrapped in color escape codes")
	}
}

// TestScreenBusyIgnoresMarkerOverwrittenInPlace is the regression for a
// session that read as busy forever: a TUI that clears its status line by
// overwriting it with spaces (differential rendering, no erase-line) left
// the marker's bytes in the old raw tail buffer, and an idle TUI never
// emitted enough to push them out. Confirmed with a scripted fake agent
// under a real ConPTY: every delivery was refused while the screen showed
// nothing busy.
func TestScreenBusyIgnoresMarkerOverwrittenInPlace(t *testing.T) {
	raw := "\x1b[20;1H• Working (3s • " + busyMarkers[0] + ")" +
		"\x1b[20;1H" + strings.Repeat(" ", 40)
	if screenBusy(screenOf(t, 80, 24, raw)) {
		t.Fatal("a marker that is no longer on screen still counted as busy")
	}
}

// Only the status area counts: a transcript row far above it that merely
// quotes a marker is not the agent working.
func TestScreenBusyOnlyLooksAtTheStatusArea(t *testing.T) {
	var raw strings.Builder
	raw.WriteString("the docs say: " + busyMarkers[0] + "\r\n")
	for i := 0; i < busyRegionRows+2; i++ {
		fmt.Fprintf(&raw, "transcript line %d\r\n", i)
	}
	if screenBusy(screenOf(t, 80, 40, raw.String())) {
		t.Fatal("a marker quoted in the transcript, outside the status area, counted as busy")
	}
}

func TestNormalizeForMatchIsCaseInsensitiveAndStripsPunctuation(t *testing.T) {
	if got, want := normalizeForMatch(`Hello, "World"! (42)`), "helloworld42"; got != want {
		t.Fatalf("normalizeForMatch(%q) = %q, want %q", `Hello, "World"! (42)`, got, want)
	}
}

// TestNewlyEchoedSurvivesBoxDrawingWrapPrefix guards a failure confirmed
// live against the tmux-backed design: OpenCode's compose box renders a "┃"
// border at the start of every wrapped line, and a long delivered message
// reliably wraps across several lines.
func TestNewlyEchoedSurvivesBoxDrawingWrapPrefix(t *testing.T) {
	original := `Agent Comms: new invocation "verify-fix-3" is pending for you (requested by owner). Run agent-comms invocation list --status PENDING --to opencode-runner --json to see it, then handle it per your existing protocol.`
	wrapped := "        ┃  Agent Comms: new invocation \"verify-fix-3\" is pending for you (\r\n" +
		"        ┃  requested by owner). Run agent-comms invocation list --status PENDING\r\n" +
		"        ┃  --to opencode-runner --json to see it, then handle it per your\r\n" +
		"        ┃  existing protocol."
	if !newlyEchoed(screenOf(t, 100, 24, ""), screenOf(t, 100, 24, wrapped), original, "") {
		t.Fatal("expected the box-drawing-wrapped rendering to match the original message")
	}
}

func TestNewlyEchoedIsFalseWhenTextNeverAppeared(t *testing.T) {
	if newlyEchoed(screenOf(t, 80, 24, ""), screenOf(t, 80, 24, "totally unrelated content"), "the delivered message", "") {
		t.Fatal("expected false when the text never appeared")
	}
}

// TestNewlyEchoedSurvivesInterleavedCursorMovement guards a failure
// confirmed live against real codex: a full-screen TUI redraws the same
// logical text with cursor-repositioning sequences between individual
// characters. An emulator resolves them to the text actually on screen.
func TestNewlyEchoedSurvivesInterleavedCursorMovement(t *testing.T) {
	wrapped := "h\x1b[5Ce\x1b[5Cl\x1b[5Cl\x1b[5Co\x1b[5C"
	if !newlyEchoed(screenOf(t, 80, 24, ""), screenOf(t, 80, 24, wrapped), "hello", "") {
		t.Fatal("expected text interleaved with cursor-movement sequences to match")
	}
}

// TestNewlyEchoedFallsBackToTheEchoToken covers TUIs that shred or truncate
// a long line past recognition while the invocation ID survives.
func TestNewlyEchoedFallsBackToTheEchoToken(t *testing.T) {
	message := invocationNotice("rt-1", "codex", "inv-047", "owner")
	shredded := "### unrelated status redraw ###\r\n…check inv-047 for owner\r\n### more noise ###"
	if !newlyEchoed(screenOf(t, 80, 24, ""), screenOf(t, 80, 24, shredded), message, "inv-047") {
		t.Fatal("expected the echo token to confirm a shredded rendering")
	}
}

// TestNewlyEchoedIgnoresStaleCopy replaces a test that documented, as a
// known tradeoff, the old matcher accepting a PREVIOUS delivery's text still
// in its buffer as the echo of a new one. Echo now has to be new on screen.
func TestNewlyEchoedIgnoresStaleCopy(t *testing.T) {
	message := invocationNotice("rt-1", "codex", "inv-047", "owner")
	stale := screenOf(t, 200, 24, "> "+message)
	if newlyEchoed(stale, stale, message, "inv-047") {
		t.Fatal("a copy that was already on screen before typing counted as a new echo")
	}
	again := screenOf(t, 200, 24, "> "+message+"\r\n› "+message)
	if !newlyEchoed(stale, again, message, "inv-047") {
		t.Fatal("a second, newly rendered copy must count")
	}
}

// Interleaved status rows between wrapped segments break a full-text match;
// the distinctive tail still confirms it.
func TestNewlyEchoedMatchesTailAcrossInterleavedStatusRows(t *testing.T) {
	delivered := "Agent Comms: new invocation is pending for you. Run agent-comms invocation list to see it."
	screen := "Agent Comms: new invocation\r\n" +
		"─── [model] ─── [status: active] ───\r\n" +
		"is pending for you. Run agent-comms\r\n" +
		"─── [tokens: 1420] ───\r\n" +
		"invocation list to see it."
	if !newlyEchoed(screenOf(t, 80, 24, ""), screenOf(t, 80, 24, screen), delivered, "") {
		t.Fatal("expected the message tail to match across interleaved status rows")
	}
}

// A TUI that collapses long input into a placeholder never shows the text;
// the placeholder appearing is the echo.
func TestNewlyEchoedAcceptsPastePlaceholders(t *testing.T) {
	message := invocationNotice("rt-1", "codex", "inv-1791726870134014400", "claude")
	for _, placeholder := range []string{"› [Pasted Content 287 chars]", "> [Pasted text #1 +1 lines]"} {
		if !newlyEchoed(screenOf(t, 80, 24, ""), screenOf(t, 80, 24, placeholder), message, "inv-1791726870134014400") {
			t.Errorf("placeholder %q was not accepted as the echo", placeholder)
		}
	}
}

func TestCursorRowHoldsMessage(t *testing.T) {
	message := "wake up and check"
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"still typed at the cursor", "\x1b[25;1H› wake up and check", true},
		{"newline inserted below it", "\x1b[25;1H› wake up and check\x1b[26;1H› ", true},
		{"composer cleared after submit", "\x1b[1;1H> wake up and check\x1b[25;1H› ", false},
		{"cursor parked elsewhere", "\x1b[25;1H› wake up and check\x1b[30;1H", false},
		// Found by the fake-agent suite: a notice wrapped across rows hid
		// its tail from a check that only read the cursor row.
		{"wrapped across rows with a newline after", "\x1b[24;1H› wake up and c\x1b[25;1Hheck\x1b[26;1H", true},
		{"placeholder still in the composer", "\x1b[25;1H› Ask Codex to do anything", false},
		// Also found by the suite: stale fragments of a wrapped message left
		// above an emptied composer must not read as an unsent message.
		{"stale fragment above an emptied composer", "\x1b[23;1Hup and check\x1b[24;1H›\x1b[25;1H", false},
	}
	for _, tc := range cases {
		if got := cursorRowHoldsMessage(screenOf(t, 100, 30, tc.raw), message); got != tc.want {
			t.Errorf("%s: cursorRowHoldsMessage = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// --- terminalScreen ------------------------------------------------------

func TestTerminalScreenRendersClearsAndAlternateScreen(t *testing.T) {
	screen := newTerminalScreen(40, 10)
	defer screen.Close()
	_, _ = screen.Write([]byte("stale content that should be purged"))
	_, _ = screen.Write([]byte("\x1b[2J\x1b[Hfresh screen content"))
	got := screen.snapshot().text()
	if strings.Contains(got, "stale content") || !strings.Contains(got, "fresh screen content") {
		t.Fatalf("after a clear the screen reads %q", got)
	}
	_, _ = screen.Write([]byte("\x1b[?1049h\x1b[Halternate screen redraw"))
	got = screen.snapshot().text()
	if strings.Contains(got, "fresh screen content") || !strings.Contains(got, "alternate screen redraw") {
		t.Fatalf("on the alternate screen the screen reads %q", got)
	}
}

// Terminal queries (device attributes, mode reports) make the emulator write
// replies into a pipe nobody reads in production; that must never block the
// output path, or the whole session freezes.
func TestTerminalScreenNeverBlocksOnTerminalQueries(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			_, _ = screen.Write([]byte("\x1b[c\x1b[>c\x1b[?2004$p\x1b[6n"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writing terminal queries blocked the screen")
	}
}

func TestTerminalScreenResizeReflowsGeometry(t *testing.T) {
	screen := newTerminalScreen(20, 5)
	defer screen.Close()
	screen.Resize(60, 12)
	snap := screen.snapshot()
	if len(snap.rows) != 12 {
		t.Fatalf("rows = %d after resize, want 12", len(snap.rows))
	}
	_, _ = screen.Write([]byte(strings.Repeat("x", 50)))
	if got := screen.snapshot().rows[0]; got != strings.Repeat("x", 50) {
		t.Fatalf("row 0 = %q, want 50 x's on one 60-column row", got)
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
