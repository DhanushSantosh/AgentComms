//go:build windows

package interactiveserve

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/term"
)

// requireRealConsole returns a real Windows console handle for use as
// ControlFD, skipping the test only if no console is attached to this
// process at all. Unlike unix, where serve_test.go's newControlPty
// (github.com/creack/pty) can fabricate a synthetic controlling terminal,
// term.MakeRaw/GetSize on Windows need a genuine console handle
// (GetConsoleMode/SetConsoleMode operate on console handles specifically,
// not arbitrary pipes) — there is no equivalent way to fake one in a
// `go test` process. os.Stdin itself is not a reliable source of that
// handle: it's commonly redirected/piped by the very tooling that launches
// `go test` (confirmed live — os.Stdin.Fd() reports non-terminal here even
// when a real console session is attached), so this opens the console
// device directly (CONIN$) instead, which reliably finds the attached
// console regardless of what stdin was redirected to. Only actually skips
// when CONIN$ itself can't be opened, meaning no console is attached to
// this process at all.
func requireRealConsole(t *testing.T) uintptr {
	t.Helper()
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no real console attached to this test process; Serve's raw-mode/size queries need a genuine Windows console handle")
	}
	t.Cleanup(func() { _ = f.Close() })
	fd := f.Fd()
	if !term.IsTerminal(fd) {
		t.Skip("CONIN$ opened but does not report as a terminal; Serve's raw-mode/size queries need a genuine Windows console handle")
	}
	return fd
}

// syncBuffer is a concurrency-safe io.Writer wrapping a bytes.Buffer, used
// as the fake "Stdout" a test can inspect while Serve's output-copy
// goroutine writes to it concurrently. Mirrors serve_test.go's identical
// helper (kept duplicated per build-tag file rather than shared, since it
// has no platform dependency of its own but the tests using it do).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("condition was never met within timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// writeFakeChild writes a small cmd.exe batch script standing in for a real
// wrapped provider CLI, mirroring serve_test.go's bash fakechild.sh: prints
// "ready", reads one line, echoes it prefixed with "GOT: ", exits 7. Uses
// cmd.exe rather than PowerShell deliberately — this project's own live
// ConPTY testing during development confirmed a plain cmd.exe input/output
// round trip works exactly as expected; PowerShell's PSReadLine line-editing
// was not part of that verification and would be a real, separate risk to
// take on in this test for no benefit.
func writeFakeChild(t *testing.T, dir string) string {
	t.Helper()
	scriptPath := filepath.Join(dir, "fakechild.cmd")
	script := "@echo off\r\necho ready\r\nset /p line=\r\necho GOT: %line%\r\nexit /b 7\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	return scriptPath
}

// TestServeEndToEnd wraps a small, fully scripted fake child through the
// real Serve() — real ConPTY allocation, real named-pipe control socket,
// real Deliver round trip over it — and confirms the whole pipeline
// behaves: the socket becomes dialable, a delivered message is injected and
// echoed back through to Stdout, the child's own reply reaches Stdout, and
// the process exits with the expected code. Mirrors serve_test.go's
// identically-named unix test.
func TestServeEndToEnd(t *testing.T) {
	fd := requireRealConsole(t)
	dir := t.TempDir()
	scriptPath := writeFakeChild(t, dir)

	stdinR, stdinW := io.Pipe()
	stdout := &syncBuffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := Serve(ctx, ServeOptions{
			ProjectRoot: dir,
			RuntimeID:   "test-runtime",
			Command:     []string{"cmd.exe", "/c", scriptPath},
			ControlFD:   int(fd),
			Stdin:       stdinR,
			Stdout:      stdout,
		})
		done <- result{code, err}
	}()
	t.Cleanup(func() { _ = stdinW.Close() })

	waitForCondition(t, 15*time.Second, func() bool {
		return Alive(context.Background(), dir, "test-runtime")
	})
	waitForCondition(t, 15*time.Second, func() bool {
		return bytes.Contains([]byte(stdout.String()), []byte("ready"))
	})

	deliverCtx, deliverCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deliverCancel()
	if err := Deliver(deliverCtx, dir, "test-runtime", "hello from another runtime"); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	waitForCondition(t, 10*time.Second, func() bool {
		return bytes.Contains([]byte(stdout.String()), []byte("GOT: hello from another runtime"))
	})

	var res result
	select {
	case res = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Serve did not return after the child exited")
	}
	if res.err != nil {
		t.Fatalf("Serve returned an error: %v", res.err)
	}
	if res.code != 7 {
		t.Fatalf("expected exit code 7, got %d", res.code)
	}
	if Alive(context.Background(), dir, "test-runtime") {
		t.Fatal("expected the control pipe to be gone after Serve returned")
	}
}

// TestServeInvokesOnStartedWithChildPID mirrors serve_test.go's identically
// named unix test: Serve must call OnStarted, off its own critical path,
// with the real child PID.
func TestServeInvokesOnStartedWithChildPID(t *testing.T) {
	fd := requireRealConsole(t)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "reportpid.cmd")
	script := "@echo off\r\nexit /b 0\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })
	stdout := &syncBuffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	onStartedPID := make(chan int, 1)
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := Serve(ctx, ServeOptions{
			ProjectRoot: dir,
			RuntimeID:   "test-runtime-onstarted",
			Command:     []string{"cmd.exe", "/c", scriptPath},
			ControlFD:   int(fd),
			Stdin:       stdinR,
			Stdout:      stdout,
			OnStarted: func(pid int) {
				time.Sleep(300 * time.Millisecond) // outlast the child; must not block Serve's return
				onStartedPID <- pid
			},
		})
		done <- result{code, err}
	}()

	var res result
	select {
	case res = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Serve did not return")
	}
	if res.err != nil {
		t.Fatalf("Serve returned an error: %v", res.err)
	}

	var gotPID int
	select {
	case gotPID = <-onStartedPID:
	case <-time.After(3 * time.Second):
		t.Fatal("OnStarted was never called")
	}

	// Unlike the unix test, this only confirms OnStarted fired with *a*
	// real, positive pid rather than cross-checking it against the child's
	// own self-reported pid — cmd.exe has no simple built-in way to report
	// its own pid without spawning a further subprocess (which would then
	// have a different pid than the one Serve/ConPTY reports), and that
	// cross-check isn't the point of this test: it exists to prove
	// OnStarted fires with the real spawned pid and doesn't block Serve's
	// return, both of which this still verifies.
	if gotPID <= 0 {
		t.Fatalf("expected a positive pid from OnStarted, got %d", gotPID)
	}
}

// TestServeExportsActorToChildEnvironment mirrors serve_test.go's
// identically named unix test.
func TestServeExportsActorToChildEnvironment(t *testing.T) {
	fd := requireRealConsole(t)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "printenv.cmd")
	script := "@echo off\r\necho ACTOR=%AGENT_COMMS_ACTOR%\r\nexit /b 0\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &syncBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	code, err := Serve(ctx, ServeOptions{
		ProjectRoot: dir,
		RuntimeID:   "actor-test-runtime",
		Command:     []string{"cmd.exe", "/c", scriptPath},
		ControlFD:   int(fd),
		Stdin:       strings.NewReader(""),
		Stdout:      stdout,
		Actor:       "DAMON",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(stdout.String()), []byte("ACTOR=DAMON")) {
		t.Fatalf("expected the child to see AGENT_COMMS_ACTOR=DAMON, got: %s", stdout.String())
	}
}

// TestServeSecondInstanceRefusesWhileFirstIsLive is the Windows-specific
// counterpart to serve_test.go's identically named unix test, but exercises
// genuinely different code: on unix this proves out the filesystem-backed
// stale-socket check in protocol_unix.go's listenLocal; on Windows it
// proves out the LockFileEx-guarded mutual exclusion in
// protocol_windows.go's listenLocal, added specifically because
// winio.ListenPipe alone allows unlimited concurrent instances of the same
// pipe name (confirmed via go-winio's own source — see that file's doc
// comment) and would otherwise let two interactive-serve processes both
// silently attach to one runtime's control pipe.
func TestServeSecondInstanceRefusesWhileFirstIsLive(t *testing.T) {
	fd := requireRealConsole(t)
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "waitforinput.cmd")
	script := "@echo off\r\nset /p line=\r\nexit /b 0\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Serve(ctx, ServeOptions{
			ProjectRoot: dir,
			RuntimeID:   "dup-runtime",
			Command:     []string{"cmd.exe", "/c", scriptPath},
			ControlFD:   int(fd),
			Stdin:       stdinR,
			Stdout:      io.Discard,
		})
	}()

	waitForCondition(t, 15*time.Second, func() bool {
		return Alive(context.Background(), dir, "dup-runtime")
	})

	_, err := Serve(context.Background(), ServeOptions{
		ProjectRoot: dir,
		RuntimeID:   "dup-runtime",
		Command:     []string{"cmd.exe", "/c", "exit", "0"},
		ControlFD:   int(fd),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
	})
	if err == nil {
		t.Fatal("expected a second Serve for the same runtime to refuse while the first is live")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("first Serve did not exit after its context was cancelled")
	}
}
