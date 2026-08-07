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
// contents, which Google's Antigravity Additional Terms of Service prohibit
// ("Reverse engineer, decompile, or disassemble any aspect of the
// Services"). Community discussion on Google's own Antigravity forum
// (discuss.ai.google.dev) suggests invoking the official agy binary as a
// documented-flag subprocess is understood differently from that clause,
// but nothing from Google confirms depending on an undocumented internal
// env var name specifically is fine, and Google has suspended Antigravity
// accounts over disputed ToS reads in this exact area. checkAgyEnv below
// gates this lookup behind an explicit opt-in for that reason: capture agy
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
	if strings.TrimSpace(os.Getenv(agyUndocumentedEnvOptIn)) == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv("ANTIGRAVITY_CONVERSATION_ID"))
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
