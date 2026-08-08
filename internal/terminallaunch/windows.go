//go:build windows

package terminallaunch

import (
	"fmt"
	"os/exec"
)

// Open launches argv inside a new terminal window rooted at dir, preferring
// Windows Terminal (wt.exe) when installed and falling back to a plain
// detached console via cmd.exe's start. It does not wait for the window to
// exit -- the caller's own process is expected to return immediately,
// leaving the new window as the dedicated session.
func Open(dir string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("no command to launch")
	}
	if path, err := exec.LookPath("wt.exe"); err == nil {
		args := append([]string{"-d", dir}, argv...)
		cmd := exec.Command(path, args...)
		return cmd.Start()
	}
	// "start" with an empty title argument opens a new, detached console
	// window instead of inheriting the caller's.
	args := append([]string{"/c", "start", ""}, argv...)
	cmd := exec.Command("cmd.exe", args...)
	cmd.Dir = dir
	return cmd.Start()
}
