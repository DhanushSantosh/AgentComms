package interactiveserve

import (
	"os"
	"runtime"
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
//
// The rest describe the parent's host rather than the user's configuration,
// observed in the environment of a Claude Code session running inside the
// Claude desktop app: CLAUDECODE (the "inside Claude Code" marker), the
// entrypoint and SDK markers, the host session ID, and the desktop app's
// messaging socket and token. A fresh top-level session must not claim any
// of them -- one told the host refreshes its auth (SDK_HAS_HOST_AUTH_REFRESH)
// has no host to do it -- and the messaging token is a live credential for
// the parent's host channel that every wrapped agent, whatever its
// provider, and every tool command it runs could otherwise read. User
// configuration (ANTHROPIC_*, CLAUDE_CODE_USE_BEDROCK, model and output
// settings) is deliberately left alone.
var claudeSessionInheritanceKeys = []string{
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_BRIDGE_SESSION_ID",
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_HOST_SESSION_ID",
	"CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_CODE_SSE_PORT",
	"CLAUDE_CODE_SDK_HAS_HOST_AUTH_REFRESH",
	"CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_DESKTOP_APP_VERSION",
	"CLAUDE_AGENT_SDK_VERSION",
	"CLAUDE_PID",
}

// childEnviron returns os.Environ() with claudeSessionInheritanceKeys
// removed, so the wrapped command never inherits a stale parent-session
// identity it never asked for.
func childEnviron() []string {
	// Windows environment variable names are case-insensitive.
	fold := func(key string) string { return key }
	if runtime.GOOS == "windows" {
		fold = strings.ToUpper
	}
	exclude := make(map[string]bool, len(claudeSessionInheritanceKeys))
	for _, k := range claudeSessionInheritanceKeys {
		exclude[fold(k)] = true
	}
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if exclude[fold(key)] {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}
