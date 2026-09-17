package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/DhanushSantosh/AgentComms/internal/codexserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// codexLiveAdapter drives one persistent Codex app-server process through
// the local codexserve broker, the same shape claudeLiveAdapter gives
// Claude: one process outliving any single invocation, observers watching
// through the broker's SSE stream without being able to control it.
//
// Unlike Claude, Codex mints its own thread IDs -- there is no "create at
// this exact ID" call the way Claude's --session-id has. Continuity
// across invocations and across broker restarts is therefore handled the
// same way opencode-live handles it: the thread ID this runtime creates
// on first use is cached locally and reused automatically on every later
// invocation, with no --session-id flag required at all. If one is
// supplied anyway, it's attempted as a thread to resume first, falling
// back to creating (and caching) a fresh one if that resume fails.
type codexLiveAdapter struct{}

func (codexLiveAdapter) Validate(config *Config) error {
	if config.Model != "" {
		return errors.New("codex-live adapter does not yet support --model overrides")
	}
	return validateCodexConfig(config)
}

func (codexLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	baseURL, err := codexserve.EnsureServer(ctx, config.WorkDir, config.WorkDir)
	if err != nil {
		return "", fmt.Errorf("codex-live: ensure broker: %w", err)
	}
	codexExecutable, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("codex-live: locate Codex executable: %w", err)
	}
	client := codexserve.New(baseURL)

	threadID := config.SessionID
	if threadID == "" {
		threadID = loadCodexLiveThreadID(config.WorkDir, config.RuntimeID)
	}
	boundThreadID, err := client.Register(ctx, config.RuntimeID, codexserve.ProcessConfig{
		Executable: codexExecutable, WorkDir: config.WorkDir,
		Sandbox: config.Sandbox, AddDirs: config.CodexAddDirs,
		IgnoreUserConfig: config.CodexIgnoreUserConfig, Model: config.Model,
		ThreadID: threadID,
	})
	if err != nil {
		return "", fmt.Errorf("codex-live: register runtime: %w", err)
	}
	if boundThreadID != "" && boundThreadID != threadID {
		if err := saveCodexLiveThreadID(config.WorkDir, config.RuntimeID, boundThreadID); err != nil {
			return "", fmt.Errorf("codex-live: persist thread id: %w", err)
		}
	}
	config.Status("watch this runtime's Codex activity live in a terminal: agent-comms live attach --provider codex --runtime " + config.RuntimeID + " --server " + baseURL)

	output, err := client.Prompt(ctx, config.RuntimeID, codexPrompt(config.Actor, invocation))
	if err != nil {
		return "", fmt.Errorf("codex-live: %w", err)
	}
	return output, nil
}

// codexLiveThreadPath returns this runtime's locally-cached Codex thread
// record, the same non-authoritative local-routing convention
// opencodeLiveSessionPath uses for OpenCode's own minted session IDs --
// never part of the signed project event chain, just what lets a runtime
// with no explicit --session-id keep resuming its own thread across
// invocations instead of starting a fresh one every time.
func codexLiveThreadPath(workDir, runtimeID string) string {
	return filepath.Join(workDir, ".agent-comms", "cache", "codex-live-thread-"+runtimeID+".json")
}

func loadCodexLiveThreadID(workDir, runtimeID string) string {
	raw, err := os.ReadFile(codexLiveThreadPath(workDir, runtimeID))
	if err != nil {
		return ""
	}
	var record struct {
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return ""
	}
	return record.ThreadID
}

func saveCodexLiveThreadID(workDir, runtimeID, threadID string) error {
	path := codexLiveThreadPath(workDir, runtimeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(struct {
		ThreadID string `json:"thread_id"`
	}{ThreadID: threadID})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
