package interactiveserve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests exercise delivery.go's control-socket and delivery logic and
// childenv.go's environment filtering directly. All of it is platform
// neutral (parameterized on writeFlusher and a rendered terminalScreen, not
// a real pty or ConPTY), so unlike serve_test.go/serve_windows_test.go's
// end-to-end tests, these run identically on every platform without needing
// a real terminal at all.

func TestTryDeliverRefusesBusyTargetQuickly(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	if _, err := screen.Write([]byte("• Working (3s • " + busyMarkers[0] + ")")); err != nil {
		t.Fatal(err)
	}
	var sink syncedBuffer
	go handleConn(server, newTestTarget(&sink, screen))

	started := time.Now()
	if err := json.NewEncoder(client).Encode(Request{Kind: "try-deliver", Message: "wake"}); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.OK || !response.Busy {
		t.Fatalf("expected a busy refusal, got %+v", response)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("busy refusal took too long: %s", elapsed)
	}
	if sink.String() != "" {
		t.Fatalf("nothing may be typed into a busy target, got %q", sink.String())
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
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_MESSAGING_TOKEN", "parent-host-secret")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "claude-desktop")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1") // user configuration: must survive
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
	for _, keep := range []string{"SOME_UNRELATED_VAR=keep-me", "CLAUDE_CODE_USE_BEDROCK=1"} {
		found := false
		for _, kv := range env {
			if kv == keep {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %s to be preserved", keep)
		}
	}
}

func TestDeliverToPtyPortableWaitsForIdleThenSucceeds(t *testing.T) {
	var buf syncedBuffer
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	if _, err := screen.Write([]byte(busyMarkers[0])); err != nil {
		t.Fatal(err)
	}
	// A real pty echoes back whatever's written to it, which is what
	// delivery's echo detection relies on. A plain buffer has no such
	// loopback, so this mirrors every write onto the screen as well.
	target := loopbackWriter{buf: &buf, screen: screen}

	deliverDone := make(chan error, 1)
	go func() {
		deliverDone <- deliverToPty(target, screen, "hello", 2*time.Second, 3*time.Second)
	}()

	select {
	case err := <-deliverDone:
		t.Fatalf("expected delivery to wait while busy, but it returned early: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := screen.Write([]byte("\x1b[2J\x1b[Hnow idle\r\n")); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-deliverDone:
		if err != nil {
			t.Fatalf("expected delivery to succeed once idle: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("delivery never returned after the target went idle")
	}

	if got := buf.String(); got != "hello\r" {
		t.Fatalf("expected the message followed by a carriage return, got %q", got)
	}
}

func TestDeliverToPtyPortableFailsClosedWhenTargetStaysBusy(t *testing.T) {
	var buf syncedBuffer
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	if _, err := screen.Write([]byte(busyMarkers[0])); err != nil {
		t.Fatal(err)
	}

	err := deliverToPty(&buf, screen, "should not be injected", 300*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("expected delivery to fail closed rather than inject while the target stayed busy")
	}
	if buf.String() != "" {
		t.Fatal("delivery must not have written anything while the target stayed busy")
	}
}

// A previous delivery's text staying visible in the transcript must never be
// taken as the echo of the next one: the old raw-buffer matcher did exactly
// that, so Enter could be sent before the new text had arrived at all.
func TestDeliverIgnoresStaleCopyOfEarlierMessage(t *testing.T) {
	message := invocationNotice("rt-codex", "codex", "inv-777", "claude")
	screen := newTerminalScreen(200, 24)
	defer screen.Close()
	if _, err := screen.Write([]byte("> " + message + "\r\n")); err != nil {
		t.Fatal(err)
	}
	var buf syncedBuffer // swallows input: nothing new ever renders
	_, err := newTestTarget(&buf, screen).deliver(message, "inv-777", time.Second, 400*time.Millisecond)
	if err == nil {
		t.Fatal("a stale on-screen copy was accepted as the echo of a message that never rendered")
	}
	if strings.Contains(buf.String(), "\r") {
		t.Fatalf("Enter was sent although the text never rendered: %q", buf.String())
	}
}

// When the text never renders, whatever was typed is erased so the
// composer is left as it was found.
func TestDeliverErasesTextWhenEchoNeverArrives(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	var buf syncedBuffer
	if err := deliverToPty(&buf, screen, "wake up", time.Second, 300*time.Millisecond); err == nil {
		t.Fatal("expected delivery to fail without an echo")
	}
	if want := "wake up" + strings.Repeat("\x7f", len("wake up")); buf.String() != want {
		t.Fatalf("typed input = %q, want the text followed by one backspace per character", buf.String())
	}
}

func TestDeliverWaitsForHumanToStopTyping(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	var buf syncedBuffer
	target := newTestTarget(&buf, screen)
	if _, err := target.gate.Write([]byte("draft\r")); err != nil { // typed and submitted just now
		t.Fatal(err)
	}
	if _, err := target.deliver("wake", "", 300*time.Millisecond, time.Second); err == nil {
		t.Fatal("expected delivery to hold off while the human was typing a moment ago")
	}
	if got := buf.String(); got != "draft\r" {
		t.Fatalf("only the human's own input may have reached the target, got %q", got)
	}
}

// An unsubmitted draft blocks delivery however old it is: typing the wake-up
// line onto it and pressing Enter would send the human's half-written text.
func TestDeliverRefusesWhileHumanDraftPending(t *testing.T) {
	gate := newInputGate(&syncedBuffer{})
	if _, err := gate.Write([]byte("half a thou")); err != nil {
		t.Fatal(err)
	}
	gate.stateMu.Lock()
	gate.lastTyped = time.Now().Add(-time.Hour)
	gate.stateMu.Unlock()
	if gate.humanQuietFor(humanQuietPeriod) {
		t.Fatal("a pending draft must keep the session busy")
	}
	for _, clear := range []string{"\x15", "\x03", "\r"} {
		if _, err := gate.Write([]byte("text" + clear)); err != nil {
			t.Fatal(err)
		}
		gate.stateMu.Lock()
		gate.lastTyped = time.Now().Add(-time.Hour)
		gate.stateMu.Unlock()
		if !gate.humanQuietFor(humanQuietPeriod) {
			t.Fatalf("%q should have cleared the draft", clear)
		}
	}
	// Escape sequences are not typing: arrows, focus events, mouse reports.
	if _, err := gate.Write([]byte("\x1b[A\x1b[I\x1b[<0;10;5M\x1b[O\x1bOP")); err != nil {
		t.Fatal(err)
	}
	gate.stateMu.Lock()
	gate.lastTyped = time.Now().Add(-time.Hour)
	gate.stateMu.Unlock()
	if !gate.humanQuietFor(humanQuietPeriod) {
		t.Fatal("escape sequences were counted as a draft")
	}
	// Text inside a bracketed paste is a draft, newlines included.
	if _, err := gate.Write([]byte("\x1b[200~pasted\rline\x1b[201~")); err != nil {
		t.Fatal(err)
	}
	if gate.humanQuietFor(0) {
		t.Fatal("a bracketed paste must count as a draft")
	}
}

// Once a nested ConPTY asks for win32-input-mode, Windows Terminal encodes
// every key as "CSI Vk;Sc;Uc;Kd;Cs;Rc _". The sequences here are the exact
// bytes captured through ConPTY: Enter arrived as
// "\x1b[13;28;13;1;0;1_\x1b[13;28;13;0;0;1_" while letters typed earlier
// came through plain, so without decoding the draft could never be cleared.
func TestInputGateDecodesWin32InputModeKeys(t *testing.T) {
	quiet := func(g *inputGate) bool {
		g.stateMu.Lock()
		g.lastTyped = time.Now().Add(-time.Hour)
		g.stateMu.Unlock()
		return g.humanQuietFor(humanQuietPeriod)
	}
	encodedEnter := "\x1b[13;28;13;1;0;1_\x1b[13;28;13;0;0;1_"

	mixed := newInputGate(&syncedBuffer{})
	_, _ = mixed.Write([]byte("ab"))
	_, _ = mixed.Write([]byte(encodedEnter))
	if !quiet(mixed) {
		t.Fatal("an encoded Enter did not clear a plain-text draft")
	}

	encoded := newInputGate(&syncedBuffer{})
	// "hi" as key-down/key-up pairs.
	_, _ = encoded.Write([]byte("\x1b[72;35;104;1;0;1_\x1b[72;35;104;0;0;1_\x1b[73;23;105;1;0;1_\x1b[73;23;105;0;0;1_"))
	if encoded.humanQuietFor(humanQuietPeriod) {
		t.Fatal("encoded typing was not seen as typing")
	}
	if quiet(encoded) {
		t.Fatal("encoded typing did not leave a draft")
	}
	// Two encoded backspaces (one event with a repeat count of 2) empty it.
	_, _ = encoded.Write([]byte("\x1b[8;14;8;1;0;2_"))
	if !quiet(encoded) {
		t.Fatal("encoded backspaces did not clear the draft")
	}
	// An arrow key (no character) and a key release are not typing.
	arrows := newInputGate(&syncedBuffer{})
	_, _ = arrows.Write([]byte("\x1b[38;72;0;1;0;1_\x1b[65;30;97;0;0;1_"))
	if !arrows.humanQuietFor(humanQuietPeriod) {
		t.Fatal("an arrow key or a key release counted as typing")
	}
}

// Keystrokes typed while a delivery is in flight must arrive after it, never
// in the middle of the injected line.
func TestHumanKeystrokesWaitForDelivery(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	var buf syncedBuffer
	echo := &slowLoopback{buf: &buf, screen: screen, delay: 200 * time.Millisecond}
	target := newTestTarget(echo, screen)
	done := make(chan error, 1)
	go func() {
		_, err := target.deliver("wake up now", "", time.Second, 3*time.Second)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // the delivery now holds the gate
	typed := make(chan struct{})
	go func() {
		_, _ = target.gate.Write([]byte("X"))
		close(typed)
	}()
	if err := <-done; err != nil {
		t.Fatalf("deliver: %v", err)
	}
	<-typed
	if got := buf.String(); got != "wake up now\rX" {
		t.Fatalf("input order = %q, want the whole delivery before the human's keystroke", got)
	}
}

// Guards the codex paste-burst regression: Enter written the instant the
// echo appeared was swallowed as a newline by codex's composer.
func TestDeliverWaitsForQuietOutputBeforeEnter(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	target := &timedLoopback{screen: screen}

	if err := deliverToPty(target, screen, "wake", time.Second, time.Second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if gap := target.enterAt.Sub(target.textAt); gap < enterSettleQuiet {
		t.Fatalf("Enter followed the text after %s; want at least %s of settle time", gap, enterSettleQuiet)
	}
}

func TestDeliverStillSendsEnterWhenTargetNeverGoesQuiet(t *testing.T) {
	screen := newTerminalScreen(80, 24)
	defer screen.Close()
	target := &timedLoopback{screen: screen}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		spin := 0
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				spin++
				_, _ = screen.Write([]byte(fmt.Sprintf("\x1b7\x1b[1;80H%c\x1b8", `|/-\`[spin%4])))
			}
		}
	}()

	started := time.Now()
	if err := deliverToPty(target, screen, "wake", time.Second, time.Second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if target.enterAt.IsZero() {
		t.Fatal("Enter was never written")
	}
	if elapsed := time.Since(started); elapsed > 2*enterSettleMax+submitCheckWindow+time.Second {
		t.Fatalf("delivery took %s; the settle waits should be capped", elapsed)
	}
}

// If Enter is taken as a newline (codex inside a paste burst), the message
// is still in the composer with the cursor on a fresh line below it; a
// backspace removes that newline and one more Enter, after output settles,
// submits the message exactly as typed.
func TestDeliverResendsEnterSwallowedAsNewline(t *testing.T) {
	screen := newTerminalScreen(100, 30)
	defer screen.Close()
	composer := &fakeComposer{screen: screen, swallowEnters: 1}
	composer.render()
	if err := deliverToPty(composer, screen, "wake up and check", time.Second, 2*time.Second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	composer.mu.Lock()
	defer composer.mu.Unlock()
	if len(composer.submitted) != 1 || composer.submitted[0] != "wake up and check" {
		t.Fatalf("submitted = %q, want the message submitted once, without the swallowed newline", composer.submitted)
	}
}

// Retries are bounded. A composer that never accepts Enter must not be left
// holding the text, and the delivery must not be reported as a success.
func TestDeliverGivesUpAndErasesWhenEnterIsNeverAccepted(t *testing.T) {
	screen := newTerminalScreen(100, 30)
	defer screen.Close()
	composer := &fakeComposer{screen: screen, swallowEnters: 100}
	composer.render()
	err := deliverToPty(composer, screen, "wake up and check", time.Second, 2*time.Second)
	if err == nil {
		t.Fatal("expected an error when no Enter was ever accepted")
	}
	composer.mu.Lock()
	defer composer.mu.Unlock()
	if composer.enters != maxEnterAttempts || len(composer.submitted) != 0 || len(composer.text) != 0 {
		t.Fatalf("enters=%d submitted=%q composer=%q; want %d attempts, nothing submitted, composer empty",
			composer.enters, composer.submitted, string(composer.text), maxEnterAttempts)
	}
}

func TestDeliverSucceedsOnALaterEnterAttempt(t *testing.T) {
	screen := newTerminalScreen(100, 30)
	defer screen.Close()
	composer := &fakeComposer{screen: screen, swallowEnters: maxEnterAttempts - 1}
	composer.render()
	if err := deliverToPty(composer, screen, "wake up and check", time.Second, 2*time.Second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	composer.mu.Lock()
	defer composer.mu.Unlock()
	if len(composer.submitted) != 1 || composer.submitted[0] != "wake up and check" {
		t.Fatalf("submitted = %q, want the clean message once", composer.submitted)
	}
}

// A composer that submits on the first Enter must never get a second one.
func TestDeliverDoesNotResendEnterAfterRealSubmit(t *testing.T) {
	screen := newTerminalScreen(100, 30)
	defer screen.Close()
	composer := &fakeComposer{screen: screen}
	composer.render()
	if err := deliverToPty(composer, screen, "wake up and check", time.Second, 2*time.Second); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	time.Sleep(submitCheckWindow / 2)
	composer.mu.Lock()
	defer composer.mu.Unlock()
	if len(composer.submitted) != 1 || composer.enters != 1 {
		t.Fatalf("submitted=%q enters=%d, want one submit from exactly one Enter", composer.submitted, composer.enters)
	}
}

// syncedBuffer is a concurrency-safe io.Writer wrapping a bytes.Buffer.
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
// what was "sent") and screen (standing in for a real pty's echo).
type loopbackWriter struct {
	buf    *syncedBuffer
	screen *terminalScreen
}

func (l loopbackWriter) Write(p []byte) (int, error) {
	if _, err := l.screen.Write(p); err != nil {
		return 0, err
	}
	return l.buf.Write(p)
}

// slowLoopback echoes like loopbackWriter, but only after delay.
type slowLoopback struct {
	buf    *syncedBuffer
	screen *terminalScreen
	delay  time.Duration
}

func (l *slowLoopback) Write(p []byte) (int, error) {
	data := append([]byte(nil), p...)
	time.AfterFunc(l.delay, func() { _, _ = l.screen.Write(data) })
	return l.buf.Write(p)
}

// timedLoopback records when the message text and the Enter were each
// written, and mirrors writes onto the screen the way a real pty echoes.
type timedLoopback struct {
	mu      sync.Mutex
	screen  *terminalScreen
	textAt  time.Time
	enterAt time.Time
}

func (w *timedLoopback) Write(p []byte) (int, error) {
	w.mu.Lock()
	if string(p) == "\r" {
		if w.enterAt.IsZero() {
			w.enterAt = time.Now()
		}
	} else {
		w.textAt = time.Now()
	}
	w.mu.Unlock()
	return w.screen.Write(p)
}

// fakeComposer renders a minimal agent TUI onto screen: a transcript of
// submitted lines and a composer row ("› text") holding the cursor. The
// first swallowEnters Enters insert a newline instead of submitting, the
// way codex treats an Enter inside a paste burst.
type fakeComposer struct {
	mu            sync.Mutex
	screen        *terminalScreen
	text          []rune
	swallowEnters int
	enters        int
	submitted     []string
}

func (f *fakeComposer) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range string(p) {
		switch r {
		case '\r':
			f.enters++
			if f.swallowEnters > 0 {
				f.swallowEnters--
				f.text = append(f.text, '\n')
				continue
			}
			f.submitted = append(f.submitted, string(f.text))
			f.text = nil
		case 0x7f:
			if len(f.text) > 0 {
				f.text = f.text[:len(f.text)-1]
			}
		default:
			f.text = append(f.text, r)
		}
	}
	f.renderLocked()
	return len(p), nil
}

func (f *fakeComposer) render() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renderLocked()
}

func (f *fakeComposer) renderLocked() {
	var b strings.Builder
	b.WriteString("\x1b[2J")
	for i, line := range f.submitted {
		fmt.Fprintf(&b, "\x1b[%d;1H> %s", i+1, strings.ReplaceAll(line, "\n", " "))
	}
	lines := strings.Split(string(f.text), "\n")
	for i, line := range lines {
		fmt.Fprintf(&b, "\x1b[%d;1H› %s", 25+i, line)
	}
	last := lines[len(lines)-1]
	fmt.Fprintf(&b, "\x1b[%d;%dH", 25+len(lines)-1, 3+len([]rune(last)))
	_, _ = f.screen.Write([]byte(b.String()))
}
