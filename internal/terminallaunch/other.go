//go:build !linux && !darwin && !windows

package terminallaunch

import "fmt"

// Open is not supported on this platform: no known terminal-emulator
// convention exists for it here.
func Open(dir string, argv []string) error {
	return fmt.Errorf("launching a new terminal window is not supported on this platform")
}
