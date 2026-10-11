package interactiveserve

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// This file holds the control-socket delivery logic shared by both the unix
// pty implementation (serve.go) and the Windows ConPTY implementation
// (serve_windows.go). None of it touches a real file descriptor directly --
// it only needs something that accepts terminal input as bytes and a
// rendered view of the child's screen -- so both platforms share one
// hardened implementation of everything that has nothing to do with how a
// given OS allocates a pty.

// idleTimeout bounds how long a queued ("deliver") delivery waits for a busy
// target to come up for air before giving up rather than injecting into an
// active turn.
const idleTimeout = 90 * time.Second

// echoTimeout bounds how long delivery waits, after typing, for the text to
// visibly appear on the child's screen before giving up -- never trusting a
// fixed sleep to be long enough for a given provider's input handling.
const echoTimeout = 10 * time.Second

// enterSettleQuiet is the minimum delay, and the required output silence,
// between the echoed text and the Enter that follows it. Confirmed live:
// codex's composer treats keystrokes arriving in a rapid burst as a paste,
// and an Enter that lands inside that burst window is inserted as a newline
// rather than submitting -- the wake-up line sat typed-but-unsent until a
// human pressed Enter. Claude Code submitted fine either way, which is why
// sending Enter the instant the echo appeared looked correct until codex was
// a target.
const enterSettleQuiet = 300 * time.Millisecond

// enterSettleMax caps that wait, so a target that redraws continuously (a
// spinner, a blinking status line) can't stall delivery: Enter still goes
// out, just never sooner than enterSettleQuiet after the echo.
const enterSettleMax = 2 * time.Second

// submitCheckWindow is how long delivery watches, after Enter, for the
// message to leave the composer before deciding Enter was swallowed and
// sending exactly one more.
const submitCheckWindow = 1500 * time.Millisecond

// humanQuietPeriod is how long the human must have stopped typing before a
// delivery may start typing into the same terminal.
const humanQuietPeriod = 3 * time.Second

// directDeliveryIdleTimeout keeps invocation creation responsive. A direct
// wake is opportunistic: if the target became busy after the caller's probe,
// it is safer to return a retryable warning than to hold the requester while
// another agent finishes a potentially long turn.
const directDeliveryIdleTimeout = 250 * time.Millisecond

// errTargetBusy marks every "not now" refusal (agent mid-turn, human typing
// or holding a draft) so callers can report busy rather than a fault.
var errTargetBusy = errors.New("target is busy")

// ptyTarget is everything delivery needs from a serving session.
type ptyTarget struct {
	input  writeFlusher // the child's pty input, bypassing the gate
	screen *terminalScreen
	gate   *inputGate
	mu     sync.Mutex // serializes deliveries
}

// acceptLoop serves control-socket connections until listener is closed
// (which happens via Serve's deferred cleanup on shutdown).
func acceptLoop(listener net.Listener, target *ptyTarget) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, target)
	}
}

// writeFlusher is the minimal interface delivery needs from the underlying
// pty/ConPTY master: write terminal input. Both *os.File (creack/pty's
// ptmx) and *conpty.ConPty satisfy this.
type writeFlusher interface {
	Write(p []byte) (n int, err error)
}

func handleConn(conn net.Conn, target *ptyTarget) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	switch req.Kind {
	case "ping":
		_ = json.NewEncoder(conn).Encode(Response{OK: true, Busy: target.busy()})
	case "snapshot":
		snap := target.screen.snapshot()
		_ = json.NewEncoder(conn).Encode(Response{OK: true, Busy: target.busy(), OutputSnapshot: snap.text()})
	case "deliver":
		target.mu.Lock()
		evidence, err := target.deliver(req.Message, req.EchoToken, idleTimeout, echoTimeout)
		target.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(deliveryResponse(evidence, err))
	case "try-deliver":
		if !target.mu.TryLock() {
			_ = json.NewEncoder(conn).Encode(Response{OK: false, Busy: true, Error: "another delivery is already in progress"})
			return
		}
		evidence, err := target.deliver(req.Message, req.EchoToken, directDeliveryIdleTimeout, echoTimeout)
		target.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(deliveryResponse(evidence, err))
	default:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "unknown request kind"})
	}
}

func deliveryResponse(evidence DeliveryReceipt, err error) Response {
	if err != nil {
		return Response{OK: false, Busy: errors.Is(err, errTargetBusy), Error: err.Error()}
	}
	return Response{OK: true, TextEchoedAt: &evidence.TextEchoedAt, EnterSentAt: &evidence.EnterSentAt}
}

// busy reports whether a delivery right now would interrupt someone: the
// agent's status line shows it working, or the human is typing or has an
// unsent draft.
func (t *ptyTarget) busy() bool {
	return screenBusy(t.screen.snapshot()) || !t.gate.humanQuietFor(humanQuietPeriod)
}

// deliver types message into the child's composer and submits it:
//
//  1. wait (up to idleTO) until neither the agent nor the human is busy;
//  2. hold human input for the rest of the sequence;
//  3. type the message and wait for it to newly appear on screen -- if it
//     never does, erase what was typed and refuse to press Enter blind;
//  4. let output settle, then press Enter;
//  5. if the message is still sitting in the composer once output settles
//     again (an Enter swallowed as a newline), press Enter once more.
func (t *ptyTarget) deliver(message, token string, idleTO, echoTO time.Duration) (DeliveryReceipt, error) {
	if strings.ContainsAny(message, "\r\n") {
		return DeliveryReceipt{}, errors.New("message must be a single line")
	}
	if !t.waitUntilIdle(idleTO) {
		return DeliveryReceipt{}, fmt.Errorf("%w: still busy after %s; not injecting into an in-progress turn or a human's unsent input", errTargetBusy, idleTO)
	}
	t.gate.mu.Lock()
	defer t.gate.mu.Unlock()
	// The human may have started typing between the idle check and taking
	// the gate.
	if t.busy() {
		return DeliveryReceipt{}, fmt.Errorf("%w: became busy just before typing", errTargetBusy)
	}
	before := t.screen.snapshot()
	if _, err := t.input.Write([]byte(message)); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write text: %w", err)
	}
	if !t.waitForEcho(before, message, token, echoTO) {
		// Leave the composer as we found it. Typed input is processed in
		// order, so even text the target has not rendered yet is erased.
		_, _ = t.input.Write([]byte(strings.Repeat("\x7f", len([]rune(message)))))
		return DeliveryReceipt{}, errors.New("target never showed the typed text within the timeout; erased it and refused to send Enter blind")
	}
	textEchoedAt := time.Now().UTC()
	waitForOutputQuiet(t.screen, enterSettleQuiet, enterSettleMax)
	beforeEnter := t.screen.snapshot()
	if _, err := t.input.Write([]byte("\r")); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write enter: %w", err)
	}
	enterSentAt := time.Now().UTC()
	if err := t.confirmSubmitted(beforeEnter, message); err != nil {
		return DeliveryReceipt{}, err
	}
	return DeliveryReceipt{TextEchoedAt: textEchoedAt, EnterSentAt: enterSentAt}, nil
}

func (t *ptyTarget) waitUntilIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !t.busy() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (t *ptyTarget) waitForEcho(before screenSnapshot, message, token string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if newlyEchoed(before, t.screen.snapshot(), message, token) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// maxEnterAttempts bounds how many times delivery presses Enter for one
// message. Each retry doubles the quiet period before it, so a composer
// whose paste window is wider than enterSettleQuiet is still outlasted.
const maxEnterAttempts = 3

// confirmSubmitted makes sure Enter actually submitted the message.
//
// After each Enter it watches for evidence of a submit: the agent's status
// line showing it working, the message leaving the composer at the cursor,
// or a fresh copy of the message appearing (a line-oriented program echoing
// what it read). If none appears and the message is still in the composer,
// the Enter was swallowed and another is sent after a longer quiet period --
// preceded by a backspace when the swallowed Enter left a newline behind
// (codex inside a paste burst), so the submitted text carries no stray line
// break. A TUI that parks its cursor away from the composer never looks
// "still there", so it never gets a second Enter.
//
// If every attempt is swallowed, the typed text is erased and an error is
// returned: reporting success for a line that sits unsent in someone's
// input box is exactly the failure this exists to prevent.
func (t *ptyTarget) confirmSubmitted(beforeEnter screenSnapshot, message string) error {
	settle := enterSettleQuiet
	for attempt := 1; ; attempt++ {
		if t.waitForSubmit(beforeEnter, message) {
			return nil
		}
		snap := t.screen.snapshot()
		holds, newline := composerHoldsMessage(snap, message)
		if !holds || screenBusy(snap) {
			return nil
		}
		if attempt == maxEnterAttempts {
			erase := strings.Repeat("\x7f", len([]rune(message)))
			if newline {
				erase += "\x7f"
			}
			_, _ = t.input.Write([]byte(erase))
			return fmt.Errorf("target kept the text in its input after %d Enter attempts; erased it", attempt)
		}
		settle *= 2
		if newline {
			_, _ = t.input.Write([]byte("\x7f"))
		}
		waitForOutputQuiet(t.screen, settle, settle+enterSettleMax)
		beforeEnter = t.screen.snapshot()
		_, _ = t.input.Write([]byte("\r"))
	}
}

// waitForSubmit watches, for up to submitCheckWindow, for evidence that the
// Enter just sent submitted message.
func (t *ptyTarget) waitForSubmit(beforeEnter screenSnapshot, message string) bool {
	deadline := time.Now().Add(submitCheckWindow)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		snap := t.screen.snapshot()
		if screenBusy(snap) || !cursorRowHoldsMessage(snap, message) || newlyShownComplete(beforeEnter, snap, message) {
			return true
		}
	}
	return false
}

// waitForOutputQuiet returns once screen has seen no output for quiet, or
// once maxWait has elapsed, whichever comes first. It always waits at least
// quiet.
func waitForOutputQuiet(screen *terminalScreen, quiet, maxWait time.Duration) {
	started := time.Now()
	for {
		if since := screen.sinceLastWrite(); since >= quiet && time.Since(started) >= quiet {
			return
		}
		if time.Since(started) >= maxWait {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}
