//go:build windows

package interactiveserve

import (
	"errors"
	"time"
)

// Takeover is not supported on Windows, for the same reason Serve isn't:
// this package's interactive-session model requires a real pty.
func Takeover(pid int, gracePeriod time.Duration) error {
	return errors.New("--takeover-pid is not supported on Windows")
}
