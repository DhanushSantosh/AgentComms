//go:build !windows

package interactiveserve

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
)

// maxUnixSocketPathLen is a conservative safety margin under the real,
// hard OS limits on AF_UNIX socket paths — 108 bytes total on Linux
// (sockaddr_un.sun_path, including the null terminator) and 104 on macOS/
// BSD. Confirmed live: a path nested under a project root plus
// ".agent-comms/cache/interactive-sockets/<runtimeID>.sock" reliably blows
// through this for realistic project paths, not just unusually deep test
// temp dirs — this is a real constraint to design around, not a rare edge
// case.
const maxUnixSocketPathLen = 100

// controlAddress builds SocketPath's unix domain socket path. Deliberately
// does NOT nest under the project root itself (unlike every other local-
// routing-metadata file in this project, e.g. sessionbind's bindings file)
// — a unix domain socket path has a hard OS length limit far shorter than
// an ordinary file path, so this hashes the project into a short,
// deterministic name under a shared per-user runtime directory instead.
// That directory (socketRootDir, in socket_root_linux.go/
// socket_root_other.go) is deliberately independent of TMPDIR because the
// daemon and an interactive provider may inherit different process
// environments.
func controlAddress(projectHash, runtimeHash [32]byte, runtimeComponent string) string {
	name := fmt.Sprintf("%s-%s.sock", hex.EncodeToString(projectHash[:4]), runtimeComponent)
	path := filepath.Join(socketRootDir(), name)
	if len(path) <= maxUnixSocketPathLen {
		return path
	}
	name = fmt.Sprintf("%s-%s.sock", hex.EncodeToString(projectHash[:4]), hex.EncodeToString(runtimeHash[:8]))
	return filepath.Join(socketRootDir(), name)
}
