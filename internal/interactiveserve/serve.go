//go:build !windows

package interactiveserve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

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
// claudeSessionInheritanceKeys and childEnviron now live in childenv.go (no
// build tag), shared verbatim with the Windows ConPTY implementation.

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
	if opts.OnStarted != nil {
		go opts.OnStarted(cmd.Process.Pid)
	}

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

// acceptLoop, handleConn, deliverToPty(WithEvidence), waitForIdle, and
// waitForEchoBuf now live in delivery.go (no build tag), shared verbatim
// with the Windows ConPTY implementation — see that file's package comment
// for why.
