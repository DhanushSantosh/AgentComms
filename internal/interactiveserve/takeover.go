//go:build !windows

package interactiveserve

import (
	"errors"
	"os"
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
func Takeover(pid int, gracePeriod time.Duration) error {
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
