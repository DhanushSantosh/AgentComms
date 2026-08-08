//go:build !windows

package interactiveserve

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Takeover gracefully replaces a still-running process at pid so a new
// interactive-serve session can safely resume the exact same provider
// conversation (via the wrapped command's own --continue/--resume) without
// colliding with a session that's still live -- two processes attached to
// one provider-side session/lock is what caused a real, confirmed
// collision when this was first done by hand (docs/site/agents/
// interactive.md's "Migrating a live session" explains why).
//
// It sends SIGTERM, waits up to gracePeriod for pid to actually exit,
// escalates to SIGKILL if it hasn't, and returns only once pid is confirmed
// gone. A pid that's already gone by the time this runs is treated as
// success, not an error, so a --launch-terminal re-exec that already
// forwards --takeover-pid to a freshly spawned process never has to
// special-case "someone already handled this."
//
// Before touching pid at all, it refuses if the CALLING process is itself a
// descendant of pid -- confirmed live 2026-08-07: an agent self-relaunching
// through its own Bash tool (a child of the very session being taken over)
// killed pid, which took its own controlling terminal down with it before
// the replacement process ever got a chance to attach to a pty of its own,
// silently losing the whole session with no clear error pointing at why.
// docs/agent-invocations.md's "Migrating a live, ordinary session" section
// already named this exact risk in words; this makes it a real, checked
// refusal instead of a warning someone has to remember to heed. A fresh
// terminal opened via --launch-terminal is never a descendant of the old
// session, so it always passes this check cleanly.
func Takeover(pid int, gracePeriod time.Duration) error {
	if descendant, determined := currentProcessIsDescendantOf(pid); determined && descendant {
		return fmt.Errorf(
			"refusing to take over pid %d: this process is itself a descendant of it, so "+
				"killing it would tear down your own controlling terminal before a replacement "+
				"could attach to one -- use --launch-terminal instead, which opens a fresh "+
				"terminal window that is never a descendant of pid, then performs the takeover "+
				"from there", pid,
		)
	}
	if !processAlive(pid) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if waitForExit(pid, gracePeriod) {
		return nil
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	// A brief final wait for the kernel to actually reclaim the pid table
	// entry. Whoever is pid's real parent is responsible for reaping it
	// (wait(2) only works for a direct parent), so this is best-effort
	// visibility via repeated existence checks, not a real wait.
	if waitForExit(pid, gracePeriod) {
		return nil
	}
	return errors.New("process did not exit after SIGTERM and SIGKILL")
}

func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// processAlive probes for pid's existence without actually signaling it.
// os.FindProcess always succeeds on Unix; Signal(0) is the portable way to
// check for a live process (and, for a zombie awaiting reap by its real
// parent, still correctly reports it as "alive" -- the pid table entry has
// not been reclaimed yet).
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// maxAncestorWalkDepth bounds currentProcessIsDescendantOf's walk up the
// parent chain -- generous for any real process tree, but finite so a
// pathological or misread `ps` chain (a bug elsewhere reporting a process
// as its own parent, for instance) can never spin forever.
const maxAncestorWalkDepth = 64

// currentProcessIsDescendantOf reports whether the calling process is a
// descendant of pid, walking the parent chain via `ps -o ppid=` -- portable
// across Linux and macOS alike, unlike assuming /proc exists. determined is
// false when the walk couldn't be completed (ps unavailable, unexpected
// output); Takeover treats "couldn't tell" as "proceed as before," since
// this check is a safety net against a real, observed failure mode, not a
// security gate that has to fail closed.
func currentProcessIsDescendantOf(pid int) (descendant, determined bool) {
	current := os.Getpid()
	for depth := 0; depth < maxAncestorWalkDepth; depth++ {
		parent, ok := parentPID(current)
		if !ok {
			return false, false
		}
		if parent == pid {
			return true, true
		}
		if parent <= 1 || parent == current {
			return false, true
		}
		current = parent
	}
	return false, true
}

func parentPID(pid int) (int, bool) {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	parent, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, false
	}
	return parent, true
}
