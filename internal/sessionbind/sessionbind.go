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
// Agy (Antigravity CLI) has no publicly documented equivalent. An earlier
// version of this function guessed ANTIGRAVITY_SESSION_ID/AGY_SESSION_ID;
// neither is real. The actual variable, ANTIGRAVITY_CONVERSATION_ID, was
// only found by running `strings` on the installed agy binary and locating
// it embedded in a bundled sidecar script — inspection of the binary's
// contents to discover undocumented internal behavior Google never
// published. Separately, antigravity.google/terms Section 6 prohibits
// "using the Service in connection with products not provided by us" and
// names third-party tools accessing the Service as a breach by example
// (OAuth-hijacking backends) — language broad enough that any third-party
// wrapper of agy, this one included, sits inside an open compliance
// question the source of one env var name doesn't resolve either way; see
// docs/backlog.md for the fuller picture and status. checkAgyEnv below
// gates this lookup behind an explicit opt-in regardless: capture agy
// sessions only for an operator who has read that risk and still wants it,
// never silently by default the way the two documented providers are.
func Capture() (sessionID, adapter string) {
	if id := checkAgyEnv(); id != "" {
		return id, "agy"
	}
	if id := strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")); id != "" {
		return id, "claude"
	}
	if id := strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")); id != "" {
		return id, "codex"
	}
	for envVar, adapterName := range worker.GetRegisteredDeclarativeSessionEnvVars() {
		if id := strings.TrimSpace(os.Getenv(envVar)); id != "" {
			return id, adapterName
		}
	}
	return "", ""
}

// agyUndocumentedEnvOptIn is the explicit, conscious opt-in required before
// Capture will read ANTIGRAVITY_CONVERSATION_ID. See the ToS discussion in
// Capture's doc comment: this env var's name is not published anywhere by
// Google, only discovered via binary inspection, so this package never acts
// on it by default the way it does for Claude Code's and Codex's own
// documented session env vars.
const agyUndocumentedEnvOptIn = "AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV"

func checkAgyEnv() string {
	if !AgyUndocumentedEnvAllowed() {
		return ""
	}
	return strings.TrimSpace(os.Getenv("ANTIGRAVITY_CONVERSATION_ID"))
}

// AgyUndocumentedEnvAllowed reports whether an operator has set the
// explicit opt-in this package requires before Capture will act on
// ANTIGRAVITY_CONVERSATION_ID. Exported so other packages that also touch
// this same undocumented variable -- e.g. app.discoverAgySessionID, which
// reads it out of a live agy child's own /proc/<pid>/environ rather than
// the current process's os.Getenv -- gate on the identical opt-in instead
// of duplicating the env var name and re-deciding the same ToS question
// independently. See Capture's doc comment for the reasoning.
func AgyUndocumentedEnvAllowed() bool {
	return strings.TrimSpace(os.Getenv(agyUndocumentedEnvOptIn)) != ""
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
