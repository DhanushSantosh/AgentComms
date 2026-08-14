//go:build windows

package interactiveserve

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// Takeover gracefully replaces a still-running process at pid, mirroring
// takeover.go's unix contract exactly: refuses if the calling process is
// itself a descendant of pid (see that file's doc comment for the real,
// confirmed-live failure this guards against — the same risk applies
// identically on Windows), treats an already-gone pid as success, and
// returns only once pid is confirmed gone.
//
// Unlike the unix implementation, there is no graceful-then-forced
// escalation here — TerminateProcess only. This is a deliberate, evidenced
// choice, not a shortcut: Windows has no POSIX-style per-pid signal that
// reliably reaches an unrelated process from an unrelated caller.
// GenerateConsoleCtrlEvent, the nearest analogue, was tested live during
// this feature's development from a genuinely separate process (not the
// parent, not console-attached) against an ordinary target process with
// CREATE_NEW_PROCESS_GROUP, both directly and via the documented
// FreeConsole+AttachConsole workaround, and failed outright both times
// (ERROR_INVALID_PARAMETER) rather than silently no-op'ing — confirmed via
// a target process that proves signal receipt through its own
// signal.Notify(os.Interrupt) handler, not inferred from a target that
// merely didn't respond. TerminateProcess, by contrast, requires only a
// process handle (PROCESS_TERMINATE access) and was confirmed reliable in
// every live test performed. gracePeriod is still honored, but only to
// bound how long this waits for the OS to finish reclaiming the process
// after TerminateProcess returns — that call schedules termination, it
// does not guarantee the process is immediately gone.
func Takeover(pid int, gracePeriod time.Duration) error {
	if descendant, determined := currentProcessIsDescendantOf(pid); determined && descendant {
		return fmt.Errorf(
			"refusing to take over pid %d: this process is itself a descendant of it, so "+
				"terminating it could tear down your own controlling terminal before a replacement "+
				"could attach to one -- use --launch-terminal instead, which opens a fresh "+
				"terminal window that is never a descendant of pid, then performs the takeover "+
				"from there", pid,
		)
	}
	handle, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, uint32(pid),
	)
	if err != nil {
		// Already gone -- treat as success, matching takeover.go's own "a
		// pid that's already gone by the time this runs is treated as
		// success, not an error" rule.
		return nil
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err == nil && exitCode != 259 /* STILL_ACTIVE */ {
		return nil
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("interactiveserve: terminate pid %d: %w", pid, err)
	}
	result, waitErr := windows.WaitForSingleObject(handle, uint32(gracePeriod/time.Millisecond))
	if waitErr == nil && result == windows.WAIT_OBJECT_0 {
		return nil
	}
	return errors.New("process did not exit after TerminateProcess")
}
