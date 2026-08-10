//go:build windows

package interactiveserve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/conpty"
	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/windows"
)

// outputDrainGrace bounds how long Serve waits, after the child process has
// exited, for its output-copy goroutine to finish naturally before forcing
// it to stop by closing the ConPTY. This exists because of a real, confirmed
// difference from unix: ConPty.Read never returns io.EOF on its own when the
// attached process exits — the pseudo-console's pipe stays open until
// Close() is called explicitly (unlike a real unix pty, where reading the
// pty master returns EIO once the child's slave-side fd closes — see
// serve.go's comment on that). Confirmed live during this feature's
// development: conhost's own final teardown sequences (window-title/mode-
// reset escapes) reliably arrive within a few hundred milliseconds of the
// child itself exiting, so this grace period is generous headroom, not a
// race against real output — and forcing Close() after it elapses still
// correctly captured full output (including those trailing escapes) in
// every live test, even in runs where the natural finish didn't happen
// within the window.
const outputDrainGrace = 1 * time.Second

// resizePollInterval is how often Serve checks whether the wrapper's own
// terminal size changed and, if so, resizes the ConPTY to match. Windows has
// no equivalent of SIGWINCH — there is no POSIX-style resize signal at all —
// so this polls rather than reacting to an event. See RFC 0014's
// "Unresolved questions" for the event-based alternative considered and not
// adopted here.
const resizePollInterval = 250 * time.Millisecond

// Serve is the Windows counterpart to serve.go's unix implementation, built
// on ConPTY (github.com/charmbracelet/x/conpty) in place of creack/pty,
// which has no Windows support. It shares delivery.go's control-socket/
// busy/echo logic verbatim with the unix implementation via the writeFlusher
// interface; only pty allocation and process lifecycle differ, per
// platform, as they structurally must. Requires Windows 10 1809 or later
// (ConPTY's own platform floor); conpty.New reports a clear error rather
// than a confusing low-level failure on an unsupported build.
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
	// LIFO — it is the LAST thing that runs on every return path, matching
	// serve.go's own ordering rationale.
	defer func() { _ = term.Restore(fd, oldState) }()

	sockPath := SocketPath(opts.ProjectRoot, opts.RuntimeID)
	listener, err := listenLocal(sockPath)
	if err != nil {
		return 1, err
	}
	defer listener.Close()

	width, height, sizeErr := term.GetSize(fd)
	if sizeErr != nil {
		width, height = 80, 24
	}
	cpty, err := conpty.New(width, height, 0)
	if err != nil {
		return 1, fmt.Errorf("interactiveserve: allocate a pseudo console (requires Windows 10 1809 or later): %w", err)
	}
	defer cpty.Close()

	env := childEnviron()
	if opts.Actor != "" {
		// Appended, not prepended: matches serve.go's exact reasoning —
		// this overrides any AGENT_COMMS_ACTOR already present in the
		// wrapper's own environment with the identity actually resolved
		// for this invocation.
		env = append(env, "AGENT_COMMS_ACTOR="+opts.Actor)
	}
	attr := &syscall.ProcAttr{
		Env: env,
		Sys: &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP},
	}
	pid, handleUintptr, err := cpty.Spawn(opts.Command[0], opts.Command, attr)
	if err != nil {
		return 1, fmt.Errorf("interactiveserve: start %q in a pseudo console: %w", opts.Command[0], err)
	}
	handle := windows.Handle(handleUintptr)
	defer windows.CloseHandle(handle)
	if opts.OnStarted != nil {
		go opts.OnStarted(pid)
	}

	// Printed before the child's own first paint, same visibility nudge as
	// serve.go's identical line — see that file's comment for why this
	// can't distinguish a human from an idle runtime.
	fmt.Fprintf(opts.Stdout, "\x1b[1;36m[agent-comms] serving runtime %q here — do not use this terminal as a personal session\x1b[0m\r\n", opts.RuntimeID)

	resizeStop := make(chan struct{})
	defer close(resizeStop)
	go pollResize(cpty, fd, resizeStop)

	tee := newOutputTee()
	go func() { _, _ = io.Copy(cpty, opts.Stdin) }() // keystrokes in; errors not actionable
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		// See outputDrainGrace's doc comment: unlike serve.go's unix
		// io.Copy, this one does not naturally unblock when the child
		// exits — Serve's own wait/drain sequence below is what ends it.
		_, _ = io.Copy(io.MultiWriter(opts.Stdout, tee), cpty)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	var deliverMu sync.Mutex
	go acceptLoop(listener, tee, cpty, &deliverMu)

	waitDone := make(chan uint32, 1)
	go func() {
		_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(handle, &code)
		waitDone <- code
	}()

	var exitCode uint32
	select {
	case exitCode = <-waitDone:
	case <-sigCh:
		exitCode = terminateAndWait(handle, waitDone)
	case <-ctx.Done():
		exitCode = terminateAndWait(handle, waitDone)
	}

	select {
	case <-outputDone:
	case <-time.After(outputDrainGrace):
	}
	cpty.Close() // idempotent (sync.Once-guarded); unblocks outputDone if still pending
	<-outputDone

	return int(exitCode), nil
}

// terminateAndWait is Windows' counterpart to serve.go's forwardAndWait —
// with one deliberate, evidenced difference: no graceful-signal step first.
// See takeover_windows.go's doc comment for the live testing that led to
// this: neither GenerateConsoleCtrlEvent nor writing a raw Ctrl-C byte into
// the ConPTY's input channel reliably delivered an interrupt to a
// CREATE_NEW_PROCESS_GROUP child in any test performed, so this goes
// straight to TerminateProcess on the process handle Serve already owns
// (no cross-process console-attachment problem here, unlike Takeover's
// case — Serve is the direct parent and already holds the handle Spawn
// returned).
func terminateAndWait(handle windows.Handle, waitDone <-chan uint32) uint32 {
	_ = windows.TerminateProcess(handle, 1)
	select {
	case code := <-waitDone:
		return code
	case <-time.After(GracePeriod):
		return 1
	}
}

// pollResize checks the wrapper's own terminal size every
// resizePollInterval and resizes cpty to match on change, until stop is
// closed. See resizePollInterval's doc comment for why this polls instead
// of reacting to a resize signal.
func pollResize(cpty *conpty.ConPty, fd uintptr, stop <-chan struct{}) {
	lastWidth, lastHeight := -1, -1
	ticker := time.NewTicker(resizePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			width, height, err := term.GetSize(fd)
			if err != nil || (width == lastWidth && height == lastHeight) {
				continue
			}
			if resizeErr := cpty.Resize(width, height); resizeErr == nil {
				lastWidth, lastHeight = width, height
			}
		}
	}
}
