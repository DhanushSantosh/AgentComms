//go:build !windows

package interactiveserve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
}

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

// newControlPty allocates a real pty pair standing in for "the wrapper's own
// controlling terminal" — term.MakeRaw/GetSize/Restore need a genuine tty,
// which is exactly why ServeOptions takes an explicit ControlFD instead of
// hardcoding os.Stdin: it lets tests point raw-mode/size queries at a
// synthetic pty instead of the test process's real terminal (or lack of
// one, under `go test`).
func newControlPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	return master, slave
}

// syncBuffer is a concurrency-safe io.Writer wrapping a bytes.Buffer, used
// as the fake "Stdout" a test can inspect while Serve's output-copy
// goroutine writes to it concurrently.
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

// TestServeEndToEnd wraps a small, fully scripted fake child through the
// real Serve() — real pty allocation, real control socket, real Deliver
// round trip over it — and confirms the whole pipeline behaves: the socket
// becomes dialable, a delivered message is injected and echoed back through
// to Stdout, the child's own reply reaches Stdout, the process exits
// cleanly, and cleanup (socket removed, terminal state restored) runs.
func TestServeEndToEnd(t *testing.T) {
	requireBash(t)
	_, controlSlave := newControlPty(t)

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fakechild.sh")
	script := "#!/bin/bash\n" +
		"echo ready\n" +
		"read -r line\n" +
		"echo \"GOT: $line\"\n" +
		"exit 7\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	stdinR, stdinW := io.Pipe()
	stdout := &syncBuffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
			Command:     []string{"bash", scriptPath},
			ControlFD:   int(controlSlave.Fd()),
			Stdin:       stdinR,
			Stdout:      stdout,
		})
		done <- result{code, err}
	}()
	t.Cleanup(func() { _ = stdinW.Close() })

	waitForCondition(t, 5*time.Second, func() bool {
		return Alive(context.Background(), dir, "test-runtime")
	})

	waitForCondition(t, 5*time.Second, func() bool {
		return bytes.Contains([]byte(stdout.String()), []byte("ready"))
	})

	deliverCtx, deliverCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer deliverCancel()
	if err := Deliver(deliverCtx, dir, "test-runtime", "hello from another runtime"); err != nil {
		t.Fatalf("Deliver failed: %v", err)
	}

	waitForCondition(t, 5*time.Second, func() bool {
		return bytes.Contains([]byte(stdout.String()), []byte("GOT: hello from another runtime"))
	})

	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the child exited")
	}
	if res.err != nil {
		t.Fatalf("Serve returned an error: %v", res.err)
	}
	if res.code != 7 {
		t.Fatalf("expected exit code 7, got %d", res.code)
	}

	if Alive(context.Background(), dir, "test-runtime") {
		t.Fatal("expected the socket to be gone after Serve returned")
	}
	if _, err := os.Stat(SocketPath(dir, "test-runtime")); err == nil {
		t.Fatal("expected the socket file to have been removed")
	}
}

// TestServeExportsActorToChildEnvironment guards the actual fix: the wrapped
// child must see AGENT_COMMS_ACTOR set to whatever identity the wrapper
// itself resolved, so its own subsequent agent-comms calls authenticate as
// that actor instead of falling back to ambient owner resolution -- without
// this, every agent-comms call the wrapped session makes on its own would
// need a manually-set env var or an explicit --actor flag on every
// invocation, exactly the friction this closes.
func TestServeExportsActorToChildEnvironment(t *testing.T) {
	requireBash(t)
	_, controlSlave := newControlPty(t)

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "printenv.sh")
	script := "#!/bin/bash\n" +
		"echo \"ACTOR=$AGENT_COMMS_ACTOR\"\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &syncBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code, err := Serve(ctx, ServeOptions{
		ProjectRoot: dir,
		RuntimeID:   "actor-test-runtime",
		Command:     []string{"bash", scriptPath},
		ControlFD:   int(controlSlave.Fd()),
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

// TestServeLeavesEnvironmentUnchangedWithoutActor confirms the fix doesn't
// overreach: when the caller doesn't resolve an actor (Actor left empty,
// e.g. an already-set AGENT_COMMS_ACTOR from the shell), the child still
// inherits the wrapper's environment exactly as it always did.
func TestServeLeavesEnvironmentUnchangedWithoutActor(t *testing.T) {
	requireBash(t)
	_, controlSlave := newControlPty(t)
	t.Setenv("AGENT_COMMS_ACTOR", "shell-set-actor")

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "printenv.sh")
	script := "#!/bin/bash\n" +
		"echo \"ACTOR=$AGENT_COMMS_ACTOR\"\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout := &syncBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	code, err := Serve(ctx, ServeOptions{
		ProjectRoot: dir,
		RuntimeID:   "actor-fallback-runtime",
		Command:     []string{"bash", scriptPath},
		ControlFD:   int(controlSlave.Fd()),
		Stdin:       strings.NewReader(""),
		Stdout:      stdout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains([]byte(stdout.String()), []byte("ACTOR=shell-set-actor")) {
		t.Fatalf("expected the child to inherit the wrapper's own AGENT_COMMS_ACTOR unchanged, got: %s", stdout.String())
	}
}

// TestServeWritesSessionDedicationBanner guards the one mitigation available
// for the structural "a session must be dedicated" limitation (nothing can
// detect a human typing directly into a live session vs. the runtime being
// idle): Serve must write a visibility banner naming the runtime to its own
// Stdout before the wrapped child's own output appears, so someone launching
// interactive-serve directly in a terminal has a chance to notice.
func TestServeWritesSessionDedicationBanner(t *testing.T) {
	requireBash(t)
	_, controlSlave := newControlPty(t)

	dir := t.TempDir()
	stdinR, stdinW := io.Pipe()
	stdout := &syncBuffer{}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Serve(ctx, ServeOptions{
			ProjectRoot: dir,
			RuntimeID:   "banner-runtime",
			Command:     []string{"bash", "-c", "echo ready; cat"},
			ControlFD:   int(controlSlave.Fd()),
			Stdin:       stdinR,
			Stdout:      stdout,
		})
	}()
	t.Cleanup(func() { _ = stdinW.Close() })
	t.Cleanup(cancel)

	waitForCondition(t, 5*time.Second, func() bool {
		return strings.Contains(stdout.String(), "ready")
	})

	text := stdout.String()
	bannerIdx := strings.Index(text, "banner-runtime")
	readyIdx := strings.Index(text, "ready")
	if bannerIdx == -1 {
		t.Fatalf("expected the banner to name the runtime, got:\n%s", text)
	}
	if !strings.Contains(text, "do not use this terminal as a personal session") {
		t.Fatalf("expected the banner to warn against personal use, got:\n%s", text)
	}
	if bannerIdx > readyIdx {
		t.Fatalf("expected the banner before the child's own output, got:\n%s", text)
	}
}

// TestServeSecondInstanceRefusesWhileFirstIsLive proves the duplicate-
// registration concern from the tmux-backed design is now handled
// structurally rather than by a separate guard command: a second
// interactive-serve for the same runtime simply can't bind the socket while
// the first is alive.
func TestServeSecondInstanceRefusesWhileFirstIsLive(t *testing.T) {
	requireBash(t)
	_, controlSlave := newControlPty(t)
	dir := t.TempDir()

	stdinR, stdinW := io.Pipe()
	t.Cleanup(func() { _ = stdinW.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// bash's `read` blocks on the pty itself, not on opts.Stdin — closing
		// stdinW only stops us copying INTO the pty, it doesn't deliver an
		// EOF the child would see. Ending this Serve relies on cancelling
		// ctx, which drives Serve's own forward-signal-then-kill path.
		_, _ = Serve(ctx, ServeOptions{
			ProjectRoot: dir,
			RuntimeID:   "dup-runtime",
			Command:     []string{"bash", "-c", "read -r _"},
			ControlFD:   int(controlSlave.Fd()),
			Stdin:       stdinR,
			Stdout:      io.Discard,
		})
	}()

	waitForCondition(t, 5*time.Second, func() bool {
		return Alive(context.Background(), dir, "dup-runtime")
	})

	_, secondControlSlave := newControlPty(t)
	_, err := Serve(context.Background(), ServeOptions{
		ProjectRoot: dir,
		RuntimeID:   "dup-runtime",
		Command:     []string{"bash", "-c", "true"},
		ControlFD:   int(secondControlSlave.Fd()),
		Stdin:       strings.NewReader(""),
		Stdout:      io.Discard,
	})
	if err == nil {
		t.Fatal("expected a second Serve for the same runtime to refuse while the first is live")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("first Serve did not exit after its context was cancelled")
	}
}

// --- deliverToPty: precise busy/idle/echo timing, without waiting out the
// package's real, deliberately generous production timeouts -------------

func TestDeliverToPtyWaitsForIdleThenSucceeds(t *testing.T) {
	requireBash(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	cmd := exec.Command("bash", "-c", "read -r line; echo \"GOT: $line\"")
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	tee := newOutputTee()
	go func() { _, _ = io.Copy(tee, master) }()

	// Simulate "busy" by writing the marker directly into the tee (the
	// production path feeds this from the child's own real output; here we
	// drive it directly to test the timing logic in isolation).
	if _, err := tee.Write([]byte("esc to interrupt")); err != nil {
		t.Fatal(err)
	}

	deliverDone := make(chan error, 1)
	go func() {
		deliverDone <- deliverToPty(master, tee, "hello", 2*time.Second, 3*time.Second)
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

	waitForCondition(t, 5*time.Second, func() bool {
		return bytes.Contains(tee.snapshot(), []byte("GOT: hello"))
	})
}

func TestDeliverToPtyFailsClosedWhenTargetStaysBusy(t *testing.T) {
	requireBash(t)
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()

	tee := newOutputTee()
	if _, err := tee.Write([]byte("esc to interrupt")); err != nil {
		t.Fatal(err)
	}

	err = deliverToPty(master, tee, "should not be injected", 300*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("expected deliverToPty to fail closed rather than inject while the target stayed busy")
	}
	if bytes.Contains(tee.snapshot(), []byte("should not be injected")) {
		t.Fatal("deliverToPty must not have written anything while the target stayed busy")
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
