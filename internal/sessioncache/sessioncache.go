// Package sessioncache locates on-disk cache files for project-scoped,
// non-authoritative session state: which live-serve broker is running for
// a project, which provider conversation a runtime is bound to. This data
// is useful across process restarts, but several callers of it (live
// serve/attach, runtime session binding) may legitimately run against a
// directory that is not, and was never meant to be, a real initialized
// Agent Comms project -- so it must never be written inside that
// directory. See RFC 0035: writing directly into
// <root>/.agent-comms/cache/*.json (the exact directory name a real
// project uses for its own managed state) is what left a stray directory
// indistinguishable from a real project, the root cause of a v0.7.0 bug
// (commit 13d8cf0).
package sessioncache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
)

// Path returns the cache file path for (root, kind). root is typically a
// project root, or any working directory a broker or session was launched
// from; kind names the cache's purpose (e.g. "claude-serve", "codex-serve",
// "opencode-server", "runtime-sessions"). The returned file lives under
// identity.ConfigDir(), never inside root itself, so root's own
// directory -- initialized project or not -- is never touched.
func Path(root, kind string) (string, error) {
	configDir, err := identity.ConfigDir()
	if err != nil {
		return "", err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(absoluteRoot))
	name := fmt.Sprintf("%s-%s.json", kind, hex.EncodeToString(sum[:8]))
	return filepath.Join(configDir, "sessions", name), nil
}
