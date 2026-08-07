package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/sessionbind"
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
// Asked HULK (agy) and PETER (opencode) directly 2026-08-07, from their own
// live CLI's vantage point, what each provides: claude writes a per-PID
// ~/.claude/sessions/<pid>.json; agy carries the (undocumented, see
// sessionbind.AgyUndocumentedEnvAllowed) ANTIGRAVITY_CONVERSATION_ID in its
// own process environment, readable via /proc/<pid>/environ; opencode has
// neither a PID-keyed file nor a session env var, but `opencode session list
// --format json --max-count 1`, run from the runtime's own cwd, is confirmed
// (both by PETER live and opencode's own --help text: "-n, --max-count:
// limit to N most recent sessions") to return exactly the most recent
// session for the current project with no manual directory-filtering
// needed. See docs/backlog.md's "Interactive-serve session pinning" entry
// for the fuller writeup and current status.
func discoverSessionID(adapter string, pid int, workDir string) (string, bool) {
	switch adapter {
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		return discoverClaudeSessionID(home, pid)
	case "agy":
		return discoverAgySessionID(pid)
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

// discoverAgySessionID reads a live agy child's own /proc/<pid>/environ for
// ANTIGRAVITY_CONVERSATION_ID -- confirmed viable live by HULK (running agy)
// 2026-08-07: agy's own process environment carries this value, and reading
// it via procfs is the only external discovery route since agy writes no
// PID-keyed session file the way claude does. /proc is Linux-only; reading
// it on any other OS just fails closed (file not found) and this reports
// not-found, no special-casing needed.
//
// Gated behind the SAME AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV opt-in that
// sessionbind.Capture already requires before acting on this same
// undocumented variable (see that package's doc comment for the ToS
// reasoning) -- reading it out of a live process's own environment via
// procfs is the same category of "inspecting undocumented internal
// behavior" as reading it from os.Getenv, so it gets the identical
// conscious opt-in rather than silent-by-default treatment.
func discoverAgySessionID(pid int) (string, bool) {
	if !sessionbind.AgyUndocumentedEnvAllowed() {
		return "", false
	}
	deadline := time.Now().Add(sessionDiscoveryTimeout)
	for {
		if id, ok := readAgyConversationIDFromEnviron(pid); ok {
			return id, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// readAgyConversationIDFromEnviron parses one raw /proc/<pid>/environ read
// (NUL-separated KEY=VALUE entries, the kernel's own format for this file)
// for ANTIGRAVITY_CONVERSATION_ID. Split out from discoverAgySessionID so
// tests can exercise the parsing directly against a fixture file instead of
// a real /proc entry.
func readAgyConversationIDFromEnviron(pid int) (string, bool) {
	return parseAgyConversationIDFromEnviron(procEnvironPath(pid))
}

func procEnvironPath(pid int) string {
	return fmt.Sprintf("/proc/%d/environ", pid)
}

func parseAgyConversationIDFromEnviron(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, kv := range strings.Split(string(raw), "\x00") {
		key, value, found := strings.Cut(kv, "=")
		if found && key == "ANTIGRAVITY_CONVERSATION_ID" && value != "" {
			return value, true
		}
	}
	return "", false
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
// mostRecentOpencodeSessionID (like parseAgyConversationIDFromEnviron is
// split from its own caller) so tests can exercise the parsing directly
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
