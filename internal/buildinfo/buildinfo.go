package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

var (
	Version = "0.1.0"
	BuildID = ""
)

func ResolvedBuildID() string {
	if value := strings.TrimSpace(BuildID); value != "" {
		return value
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		revision := ""
		dirty := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
		if revision != "" {
			if dirty {
				return revision + "-dirty"
			}
			return revision
		}
	}
	return devBuildID()
}

// devBuildID identifies a binary with no VCS revision available (untagged
// or non-module builds, -buildvcs=false, etc.) by its own file's size and
// modification time rather than the fixed literal "dev". Two different
// local dev builds must never be treated as identical by
// ensureDaemon's compatibility check -- a fixed literal would let a
// rebuilt binary silently reuse a stale daemon from a previous build.
// Stable across repeated calls against the same, unmodified binary file;
// changes whenever the binary is rebuilt (which changes its mtime and
// almost always its size).
func devBuildID() string {
	executable, err := os.Executable()
	if err != nil {
		return "dev"
	}
	info, err := os.Stat(executable)
	if err != nil {
		return "dev"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", executable, info.Size(), info.ModTime().UnixNano())))
	return "dev-" + hex.EncodeToString(sum[:8])
}
