//go:build windows

package interactiveserve

import (
	"os"
	"path/filepath"
)

// socketRootDir returns a per-installation temp directory. On unix this
// roots the actual control-socket files (see socket_address_unix.go). On
// Windows the control channel itself is a named pipe (see
// socket_address_windows.go), which lives in the kernel's `\\.\pipe\`
// namespace, not the filesystem — so this directory is used instead to hold
// the small lock-marker files listenLocal (protocol_windows.go) uses for
// mutual exclusion between concurrent interactive-serve processes. See that
// file's doc comment for why a marker file is needed at all on Windows,
// unlike unix.
func socketRootDir() string {
	return filepath.Join(os.TempDir(), "agent-comms-interactive")
}
