//go:build !windows

package interactiveserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

// idleTimeout bounds how long deliverToPty waits for a busy target to come
// up for air before giving up rather than injecting into an active turn. It
// is generous because the target may legitimately be deep in its own
// multi-step tool-calling sequence — waiting longer here is what actually
// prevents two deliveries from gluing their text together in the pty's input
// buffer, not a fallback for it.
const idleTimeout = 90 * time.Second

// echoTimeout bounds how long deliverToPty waits, after writing text, for
// the pty to visibly echo it back before writing Enter — never trusting a
// fixed sleep to be long enough for a given provider's input handling.
const echoTimeout = 10 * time.Second

// directDeliveryIdleTimeout keeps invocation creation responsive. A direct
// wake is opportunistic: if the target became busy after the caller's probe,
// it is safer to return a retryable warning than to hold the requester while
// another agent finishes a potentially long turn.
const directDeliveryIdleTimeout = 250 * time.Millisecond

// GracePeriod bounds how long Serve waits for the child to exit on its own
// after being sent a forwarded signal before it is killed outright. Exported
// so Takeover (takeover.go) can wait the same amount of time for an
// existing session to exit cleanly before escalating.
const GracePeriod = 3 * time.Second

// Serve allocates a real pty, execs opts.Command attached to it, and
// transparently forwards opts.ControlFD/Stdin/Stdout so the invoking
// terminal shows the child's native UI unmediated — the same experience as
// running opts.Command directly, in any terminal emulator. It simultaneously
// listens on this runtime's control socket (SocketPath) so other processes
// can wake it with Deliver.
//
// Only the wrapper's OWN controlling terminal (opts.ControlFD) is put into
// raw mode; the child's pty retains normal cooked-mode line discipline, so a
// Ctrl-C the user types flows through as an ordinary byte and the child's
// own tty generates SIGINT for it exactly as if it were run directly — no
// special-casing needed. The signal handling here is for the other case:
// something sends SIGINT/SIGTERM to the wrapper process itself directly.
//
// claudeSessionInheritanceKeys are environment variables Claude Code's own
// CLI sets to mark "this process is a subordinate child of another live
// session" (CLAUDE_CODE_CHILD_SESSION) and to identify that parent session
// (CLAUDE_CODE_SESSION_ID, CLAUDE_CODE_BRIDGE_SESSION_ID). The child Serve
// execs is meant to become a fresh, independent, top-level interactive
// session in its own right — if Serve is invoked from inside an
// already-running Claude Code session (an agent spinning up its own, or
// another runtime's, interactive-serve), the wrapped `claude` process would
// otherwise inherit these unchanged and conclude it is itself a child,
// disabling its own transcript persistence entirely. Confirmed live: a
// `claude` child spawned this way showed "Transcript saving is off —
// inherited CLAUDE_CODE_CHILD_SESSION marker" in its own status line.
var claudeSessionInheritanceKeys = []string{
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_BRIDGE_SESSION_ID",
}

// childEnviron returns os.Environ() with claudeSessionInheritanceKeys
// removed, so the wrapped command never inherits a stale parent-session
// identity it never asked for.
func childEnviron() []string {
	exclude := make(map[string]bool, len(claudeSessionInheritanceKeys))
	for _, k := range claudeSessionInheritanceKeys {
		exclude[k] = true
	}
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if exclude[key] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

// Serve returns the child's exit code and never calls os.Exit itself — the
// caller must do that only after Serve has returned, so pending cleanup
// (terminal-mode restore, socket removal) always runs first. Calling
// os.Exit before that would skip Go's deferred functions entirely and leave
// the user's real terminal broken (raw mode, no echo) after every use.
func Serve(ctx context.Context, opts ServeOptions) (int, error) {
	if len(opts.Command) == 0 {
		return 1, errors.New("interactiveserve: no command given to serve")
	}
	if opts.ControlFD == 0 {
		opts.ControlFD = int(os.Stdin.Fd())
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}

	fd := uintptr(opts.ControlFD)
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 1, fmt.Errorf("interactiveserve: put terminal in raw mode: %w", err)
	}
	// Deferred first, before any other cleanup step, so — since defers run
	// LIFO — it is the LAST thing that runs on every return path, even if a
	// later cleanup step itself panics.
	defer func() { _ = term.Restore(fd, oldState) }()

	sockPath := SocketPath(opts.ProjectRoot, opts.RuntimeID)
	listener, err := listenLocal(sockPath)
	if err != nil {
		return 1, err
	}
	defer listener.Close()
	defer func() { _ = os.Remove(sockPath) }()

	cmd := exec.Command(opts.Command[0], opts.Command[1:]...)
	cmd.Env = childEnviron()
	if opts.Actor != "" {
		// Appended, not prepended: os/exec keeps only the last value for a
		// duplicate key, so this deliberately overrides any AGENT_COMMS_ACTOR
		// already present in the wrapper's own environment (e.g. one set by
		// hand in the shell) with the identity actually resolved for this
		// invocation -- the explicit, resolved value should win.
		cmd.Env = append(cmd.Env, "AGENT_COMMS_ACTOR="+opts.Actor)
	}
	w, h, sizeErr := term.GetSize(fd)
	if sizeErr != nil {
		w, h = 80, 24
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		return 1, fmt.Errorf("interactiveserve: start %q in a pty: %w", opts.Command[0], err)
	}
	defer ptmx.Close()

	// Printed before the child's own first paint, so it's visible for a
	// moment even though a full-screen TUI's alt-screen entry will cover it
	// shortly after. This is a visibility nudge, not a detection mechanism —
	// nothing here can tell a human directly using this terminal apart from
	// the runtime it serves being idle; see docs/agent-invocations.md's
	// "Many-to-many delivery" section for that structural limit.
	fmt.Fprintf(opts.Stdout, "\x1b[1;36m[agent-comms] serving runtime %q here — do not use this terminal as a personal session\x1b[0m\r\n", opts.RuntimeID)

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if width, height, sizeErr := term.GetSize(fd); sizeErr == nil {
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
			}
		}
	}()

	tee := newOutputTee()
	go func() { _, _ = io.Copy(ptmx, opts.Stdin) }() // keystrokes in; errors not actionable
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		// Once the child's slave-side fd closes, reading the pty master
		// returns EIO on Linux, not io.EOF — creack/pty's most common
		// gotcha. Both mean "the child is gone," not a real I/O error.
		_, copyErr := io.Copy(io.MultiWriter(opts.Stdout, tee), ptmx)
		_ = copyErr
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var deliverMu sync.Mutex
	go acceptLoop(listener, tee, ptmx, &deliverMu)

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case sig := <-sigCh:
		waitErr = forwardAndWait(cmd, sig, waitDone)
	case <-ctx.Done():
		waitErr = forwardAndWait(cmd, os.Interrupt, waitDone)
	}
	<-outputDone // let final output flush through before returning

	return exitCodeFor(cmd, waitErr), nil
}

// forwardAndWait forwards sig to cmd's process, waits up to GracePeriod for
// it to exit on its own, and kills it outright if it hasn't.
func forwardAndWait(cmd *exec.Cmd, sig os.Signal, waitDone <-chan error) error {
	_ = cmd.Process.Signal(sig)
	select {
	case err := <-waitDone:
		return err
	case <-time.After(GracePeriod):
		_ = cmd.Process.Kill()
		return <-waitDone
	}
}

// exitCodeFor computes the exit code to report, matching shell convention
// (128+signal) for a process that was terminated by a signal rather than
// exiting on its own — cmd.ProcessState.ExitCode() alone returns -1 in that
// case.
func exitCodeFor(cmd *exec.Cmd, waitErr error) int {
	if cmd.ProcessState != nil {
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			return code
		}
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
	}
	if waitErr != nil {
		return 1
	}
	return 0
}

// acceptLoop serves control-socket connections until listener is closed
// (which happens via Serve's deferred cleanup on shutdown).
func acceptLoop(listener net.Listener, tee *outputTee, ptmx *os.File, mu *sync.Mutex) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go handleConn(conn, tee, ptmx, mu)
	}
}

func handleConn(conn net.Conn, tee *outputTee, ptmx *os.File, mu *sync.Mutex) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}
	switch req.Kind {
	case "ping":
		_ = json.NewEncoder(conn).Encode(Response{OK: true, Busy: isBusy(tee.snapshot())})
	case "deliver":
		mu.Lock()
		evidence, err := deliverToPtyWithEvidence(ptmx, tee, req.Message, idleTimeout, echoTimeout)
		mu.Unlock()
		resp := Response{OK: err == nil}
		if err == nil {
			resp.TextEchoedAt = &evidence.TextEchoedAt
			resp.EnterSentAt = &evidence.EnterSentAt
		}
		if err != nil {
			resp.Error = err.Error()
		}
		_ = json.NewEncoder(conn).Encode(resp)
	case "try-deliver":
		if !mu.TryLock() {
			_ = json.NewEncoder(conn).Encode(Response{OK: false, Busy: true, Error: "another delivery is already in progress"})
			return
		}
		evidence, err := deliverToPtyWithEvidence(ptmx, tee, req.Message, directDeliveryIdleTimeout, echoTimeout)
		mu.Unlock()
		resp := Response{OK: err == nil, Busy: err != nil && isBusy(tee.snapshot())}
		if err == nil {
			resp.TextEchoedAt = &evidence.TextEchoedAt
			resp.EnterSentAt = &evidence.EnterSentAt
		}
		if err != nil {
			resp.Error = err.Error()
		}
		_ = json.NewEncoder(conn).Encode(resp)
	default:
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "unknown request kind"})
	}
}

// deliverToPty writes message into ptmx as terminal input: waits for the
// target to be idle (see isBusy) before sending anything — the direct
// replacement for the old cross-process delivery lock, since there's now
// only ever one process touching this pty, concurrent senders just make
// concurrent socket connections and this mutex-guarded function serializes
// them — then writes the text and a separate Enter only once tee has
// visibly reflected the text back, never blind. idleTO/echoTO are the real
// package constants in production; tests call this directly with short
// overrides rather than waiting out the real, deliberately generous values.
func deliverToPty(ptmx *os.File, tee *outputTee, message string, idleTO, echoTO time.Duration) error {
	_, err := deliverToPtyWithEvidence(ptmx, tee, message, idleTO, echoTO)
	return err
}

func deliverToPtyWithEvidence(ptmx *os.File, tee *outputTee, message string, idleTO, echoTO time.Duration) (DeliveryReceipt, error) {
	if !waitForIdle(tee, idleTO) {
		return DeliveryReceipt{}, fmt.Errorf("target was still busy after %s; not injecting into an in-progress turn", idleTO)
	}
	if _, err := ptmx.Write([]byte(message)); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write text: %w", err)
	}
	if !waitForEchoBuf(tee, message, echoTO) {
		return DeliveryReceipt{}, errors.New("target never echoed the sent text back within the timeout; refusing to send Enter blind")
	}
	textEchoedAt := time.Now().UTC()
	if _, err := ptmx.Write([]byte("\r")); err != nil {
		return DeliveryReceipt{}, fmt.Errorf("write enter: %w", err)
	}
	enterSentAt := time.Now().UTC()
	return DeliveryReceipt{TextEchoedAt: textEchoedAt, EnterSentAt: enterSentAt}, nil
}

func waitForIdle(tee *outputTee, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !isBusy(tee.snapshot()) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func waitForEchoBuf(tee *outputTee, text string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if echoed(tee.snapshot(), text) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
