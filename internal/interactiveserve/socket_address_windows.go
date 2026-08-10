//go:build windows

package interactiveserve

import (
	"encoding/hex"
	"fmt"
)

// maxPipeAddressLen is a conservative safety margin under the documented
// Windows limit on a full named pipe path (256 characters, including the
// `\\.\pipe\` prefix).
const maxPipeAddressLen = 200

// controlAddress builds SocketPath's Windows named pipe address. Unlike a
// unix domain socket, a named pipe is not a filesystem path at all — it
// lives in the kernel's `\\.\pipe\` namespace, so this does not use
// socketRootDir() (that directory now holds lock-marker files instead; see
// protocol_windows.go).
func controlAddress(projectHash, runtimeHash [32]byte, runtimeComponent string) string {
	name := fmt.Sprintf(`\\.\pipe\agent-comms-interactive-%s-%s`, hex.EncodeToString(projectHash[:4]), runtimeComponent)
	if len(name) <= maxPipeAddressLen {
		return name
	}
	return fmt.Sprintf(`\\.\pipe\agent-comms-interactive-%s-%s`, hex.EncodeToString(projectHash[:4]), hex.EncodeToString(runtimeHash[:8]))
}
