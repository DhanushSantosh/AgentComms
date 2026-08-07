package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// sessionDiscoveryTimeout bounds how long discoverSessionID polls for a
// freshly spawned interactive-serve child to write its own session-identity
// file. Generous enough for a slow first start, short enough that a
// goroutine calling this from interactiveserve.ServeOptions.OnStarted never
// spins indefinitely for an adapter that never writes one at all. A var,
// not a const, so tests covering the "file never appears" path can shrink
// it rather than actually waiting out the production value.
var sessionDiscoveryTimeout = 5 * time.Second

// discoverSessionID looks up the provider-native session/conversation ID a
// freshly spawned interactive-serve child assigned itself, keyed by its own
// PID, so runInteractiveServe can auto-pin it via sessionbind.Save without
// requiring an operator to run `runtime bind-session` by hand every time.
//
// Returns ok=false when adapter has no known per-PID discovery mechanism.
// Asked HULK (agy) and PETER (opencode) directly 2026-08-07, from their own
// live CLI's vantage point, whether either has an equivalent to claude's
// own ~/.claude/sessions/<pid>.json -- neither could point at a concrete
// file, so this stays claude-only rather than guessing at an agy/opencode
// path that might not exist; see docs/backlog.md's "Interactive-serve
// session pinning" entry. sessionbind.Capture (env-var based, for a process
// resuming its OWN session from inside) and the manual `runtime
// bind-session --id <id>` command remain the way to seed a binding for
// those two adapters until a real discovery path is confirmed.
func discoverSessionID(adapter string, pid int) (string, bool) {
	switch adapter {
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return discoverClaudeSessionID(home, pid)
	default:
		return "", false
	}
}

// discoverClaudeSessionID polls Claude Code's own per-PID session-state
// file -- confirmed live to contain {"pid":...,"sessionId":...,"cwd":...},
// written by the claude binary itself, not something this project produces
// -- until it appears or sessionDiscoveryTimeout elapses. A few hundred
// milliseconds' delay between the child starting and it writing this file
// is normal, hence the poll rather than a single read.
//
// Takes claudeHome explicitly (the caller resolves os.UserHomeDir() once,
// same pattern claudeserve.go already uses for claudepath.SessionPath)
// rather than calling os.UserHomeDir() internally -- confirmed live via a
// real CI failure that t.Setenv("HOME", ...) is silently a no-op for
// os.UserHomeDir() on Windows (which reads %USERPROFILE%, not $HOME), so a
// version of this function that resolved its own home directory internally
// could never be pointed at a test's tempdir on that platform.
func discoverClaudeSessionID(claudeHome string, pid int) (string, bool) {
	path := filepath.Join(claudeHome, ".claude", "sessions", strconv.Itoa(pid)+".json")
	deadline := time.Now().Add(sessionDiscoveryTimeout)
	for {
		if raw, readErr := os.ReadFile(path); readErr == nil {
			var record struct {
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(raw, &record) == nil && record.SessionID != "" {
				return record.SessionID, true
			}
			return "", false
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
