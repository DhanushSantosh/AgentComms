package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
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
// or non-module builds, -buildvcs=false, etc.) by a hash of its own file's
// content rather than the fixed literal "dev". Two different local dev
// builds must never be treated as identical by ensureDaemon's
// compatibility check -- a fixed literal would let a rebuilt binary
// silently reuse a stale daemon from a previous build.
//
// Hashes content, not path+size+modtime as an earlier version of this
// function did: identical binaries built at different absolute paths (a
// worktree checkout vs. the main one, confirmed live: two agents working
// the same source from separate git worktrees today) got different build
// IDs despite being byte-for-byte the same build, and a CI cache restore
// that preserves content but resets modtime produced spurious build-ID
// churn and triggered unnecessary daemon restarts on every restore. A
// content hash only changes when the binary actually did, which is
// exactly the property ensureDaemon's check needs -- if a rebuild
// genuinely produces different bytes, this still changes; if it doesn't
// (a fully reproducible build with no code change), not restarting the
// daemon is correct, not a bug.
func devBuildID() string {
	executable, err := os.Executable()
	if err != nil {
		return "dev"
	}
	return devBuildIDForPath(executable)
}

// devBuildIDForPath does the actual hashing, taking the executable path as
// a parameter rather than calling os.Executable() itself, so tests can
// exercise it against a throwaway file instead of the real test binary.
func devBuildIDForPath(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "dev"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "dev"
	}
	return "dev-" + hex.EncodeToString(h.Sum(nil)[:8])
}
