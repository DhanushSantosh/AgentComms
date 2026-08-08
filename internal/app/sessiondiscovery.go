package app

import (
	"encoding/json"
	"os"
	"os/exec"
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
// freshly spawned interactive-serve child assigned itself, so
// runInteractiveServe can auto-pin it via sessionbind.Save without requiring
// an operator to run `runtime bind-session` by hand every time. workDir is
// the runtime's project root -- only opencode's discovery path uses it (to
// scope `opencode session list` to the right project's sessions).
//
// Returns ok=false when adapter has no known discovery mechanism, or the
// mechanism it has didn't turn anything up within sessionDiscoveryTimeout.
// Asked PETER (opencode) directly 2026-08-07, from its own live CLI's
// vantage point, what it provides: claude writes a per-PID
// ~/.claude/sessions/<pid>.json; opencode has no PID-keyed file, but
// `opencode session list --format json --max-count 1`, run from the
// runtime's own cwd, is confirmed (both by PETER live and opencode's own
// --help text: "-n, --max-count: limit to N most recent sessions") to
// return exactly the most recent session for the current project with no
// manual directory-filtering needed. See docs/backlog.md's "Interactive-
// serve session pinning" entry for the fuller writeup and current status.
// (An agy case lived here too, briefly -- removed 2026-08-08 along with the
// rest of agy support, over an unresolved third-party ToS compliance
// question; same doc.)
func discoverSessionID(adapter string, pid int, workDir string) (string, bool) {
	switch adapter {
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return discoverClaudeSessionID(home, pid)
	case "opencode":
		return discoverOpencodeSessionID(workDir)
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

// discoverOpencodeSessionID polls `opencode session list --format json
// --max-count 1`, run from workDir, for the most recent session belonging
// to that project -- confirmed live by PETER (running opencode) 2026-08-07:
// the command already scopes to the project containing the given cwd with
// no manual directory filtering needed, and --max-count's own --help text
// ("limit to N most recent sessions") independently confirms --max-count 1
// returns the newest one. A freshly spawned opencode process needs a
// moment to register its session, hence the poll rather than one shot.
func discoverOpencodeSessionID(workDir string) (string, bool) {
	deadline := time.Now().Add(sessionDiscoveryTimeout)
	for {
		if id, ok := mostRecentOpencodeSessionID(workDir); ok {
			return id, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func mostRecentOpencodeSessionID(workDir string) (string, bool) {
	cmd := exec.Command("opencode", "session", "list", "--format", "json", "--max-count", "1")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return parseOpencodeSessionListJSON(out)
}

// parseOpencodeSessionListJSON pulls the first entry's id out of `opencode
// session list --format json`'s output. Split out from
// mostRecentOpencodeSessionID so tests can exercise the parsing directly
// against a real captured sample instead of needing the opencode binary
// installed.
func parseOpencodeSessionListJSON(raw []byte) (string, bool) {
	var sessions []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &sessions) != nil || len(sessions) == 0 || sessions[0].ID == "" {
		return "", false
	}
	return sessions[0].ID, true
}
