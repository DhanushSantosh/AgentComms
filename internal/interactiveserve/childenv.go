package interactiveserve

import (
	"os"
	"strings"
)

// claudeSessionInheritanceKeys are environment variables Claude Code's own
// CLI sets to mark "this process is a subordinate child of another live
// session" (CLAUDE_CODE_CHILD_SESSION) and to identify that parent session
// (CLAUDE_CODE_SESSION_ID, CLAUDE_CODE_BRIDGE_SESSION_ID). The child Serve
// execs is meant to become a fresh, independent, top-level interactive
// session in its own right — if Serve is invoked from inside an
// already-running Claude Code session (an agent spinning up its own, or
// another runtime's, interactive-serve), the wrapped `claude` process would
// otherwise inherit these unchanged and conclude it is itself a child,
// disabling its own transcript persistence entirely. Confirmed live: a
// `claude` child spawned this way showed "Transcript saving is off —
// inherited CLAUDE_CODE_CHILD_SESSION marker" in its own status line.
// Platform-independent — shared verbatim by serve.go (unix) and
// serve_windows.go.
var claudeSessionInheritanceKeys = []string{
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_BRIDGE_SESSION_ID",
}

// childEnviron returns os.Environ() with claudeSessionInheritanceKeys
// removed, so the wrapped command never inherits a stale parent-session
// identity it never asked for.
func childEnviron() []string {
	exclude := make(map[string]bool, len(claudeSessionInheritanceKeys))
	for _, k := range claudeSessionInheritanceKeys {
		exclude[k] = true
	}
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if exclude[key] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}
