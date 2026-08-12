// Package sessionbind persists local, non-authoritative bindings between a
// registered Agent Comms runtime and the Claude or Codex conversation it
// resumes. Bindings are operational routing metadata supplied by the local
// process, never part of the signed project event chain, and live only in
// the project's local, gitignored cache directory.
package sessionbind

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/worker"
)

// Binding records which provider conversation a runtime is bound to.
type Binding struct {
	SessionID  string    `json:"session_id"`
	Adapter    string    `json:"adapter"`
	CapturedAt time.Time `json:"captured_at"`
}

// Path returns the local binding file path for a project root.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, ".agent-comms", "cache", "runtime-sessions.json")
}

// Capture inspects the current process environment for a provider-native
// session identifier. It reports an empty adapter when no supported
// provider environment variable is present.
//
// Claude Code exports CLAUDE_CODE_SESSION_ID to every process it spawns.
// Codex exports CODEX_THREAD_ID (codex-rs/protocol/src/shell_environment.rs)
// to commands run by its shell tool, injected even when the configured
// shell_environment_policy restricts inherited variables to an include_only
// list — so it survives a restrictive policy the way an ordinary env var
// would not. Both are publicly documented behavior of their respective CLIs.
//
// (This package previously also captured agy/Antigravity sessions via an
// undocumented env var found only by running `strings` on the installed
// binary -- removed 2026-08-08 along with the rest of agy support, over an
// unresolved third-party ToS compliance question; see docs/backlog.md.)
//
// The Claude/Codex checks delegate to identity.DetectProviderSessionID
// rather than duplicating them -- that function exists specifically so
// packages that cannot import this one without an import cycle (see RFC
// 0016) still get the identical detection logic, from one implementation.
func Capture() (sessionID, adapter string) {
	if id := identity.DetectProviderSessionID(); id != "" {
		switch {
		case strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")) == id:
			return id, "claude"
		case strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) == id:
			return id, "codex"
		}
	}
	for envVar, adapterName := range worker.GetRegisteredDeclarativeSessionEnvVars() {
		if id := strings.TrimSpace(os.Getenv(envVar)); id != "" {
			return id, adapterName
		}
	}
	return "", ""
}

func load(path string) (map[string]Binding, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Binding{}, nil
	}
	if err != nil {
		return nil, err
	}
	bindings := map[string]Binding{}
	if err := json.Unmarshal(raw, &bindings); err != nil {
		return nil, err
	}
	return bindings, nil
}

// Save records the binding for runtimeID, overwriting any prior entry.
func Save(projectRoot, runtimeID, sessionID, adapter string) error {
	path := Path(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	bindings, err := load(path)
	if err != nil {
		return err
	}
	bindings[runtimeID] = Binding{SessionID: sessionID, Adapter: adapter, CapturedAt: time.Now().UTC()}
	raw, err := json.MarshalIndent(bindings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Load returns the binding recorded for runtimeID, if any.
func Load(projectRoot, runtimeID string) (Binding, bool, error) {
	bindings, err := load(Path(projectRoot))
	if err != nil {
		return Binding{}, false, err
	}
	binding, ok := bindings[runtimeID]
	return binding, ok, nil
}
