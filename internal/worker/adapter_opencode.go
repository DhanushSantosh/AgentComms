package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// openCodeAdapter drives OpenCode as a plain, direct CLI exec via `opencode
// run`, the same shape claude/codex's exec adapters already have —
// confirmed live this session that nothing about opencode rules this out:
// `opencode run` accepts its message on stdin exactly like claude/codex do,
// `--format json` gives a clean newline-delimited event stream to parse for
// the final answer, and a `--session <id>` that doesn't exist fails with a
// plain "Error: Session not found" line rather than hanging or crashing.
type openCodeExecutor func(
	context.Context,
	Config,
	model.Invocation,
	string,
) (openCodeRunResult, string, error)

type openCodeAdapter struct {
	execute openCodeExecutor
}

func (openCodeAdapter) Validate(config *Config) error {
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("opencode permission mode must not bypass permissions")
	}
	return validateExecutablePath(config.Executable, "worker executable")
}

func (openCodeAdapter) Arguments(config Config) []string {
	arguments := []string{"run", "--format", "json", "--pure"}
	if sessionID := config.SessionID; sessionID != "" {
		arguments = append(arguments, "--session", sessionID)
	}
	if config.Model != "" {
		arguments = append(arguments, "--model", config.Model)
	}
	return arguments
}

func (openCodeAdapter) Prompt(_ string, invocation model.Invocation) string {
	return claudeUserPrompt(invocation)
}

// opencodePermissionEnv forces OpenCode's tool-category permission config
// the same way openCodeACPPermissionEnv does for opencode-acp, but as a
// static choice rather than a live approve/deny callback — this adapter has
// no protocol channel to answer a request through. Confirmed live: a
// category set to "ask" auto-rejects in this non-interactive mode rather
// than hanging, so "edit" -> "allow" (only when PermissionMode is
// "acceptEdits") plus everything else -> "ask" reproduces the exact same
// edits-allowed/everything-else-denied contract opencode-acp and
// opencode-live already enforce, just reached by a different mechanism.
func opencodePermissionEnv(permissionMode string) string {
	editPermission := "ask"
	if permissionMode == "acceptEdits" {
		editPermission = "allow"
	}
	return fmt.Sprintf(
		`OPENCODE_PERMISSION={"edit":%q,"bash":"ask","webfetch":"ask","websearch":"ask","external_directory":"ask","task":"ask"}`,
		editPermission,
	)
}

// openCodeEvent is the shape of one line of `opencode run --format json`'s
// stdout. Fields not needed by this adapter are omitted; unrecognized
// fields are ignored by encoding/json rather than rejected.
type openCodeEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Tool      string `json:"tool"`
		MessageID string `json:"messageID"`
		State     struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"state"`
	} `json:"part"`
}

func (adapter openCodeAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	sessionID := config.SessionID
	if sessionID == "" {
		sessionID = loadOpenCodeSessionID(config.WorkDir, config.RuntimeID)
	}

	execute := adapter.execute
	if execute == nil {
		execute = runOpenCode
	}
	output, resultSessionID, err := execute(ctx, config, invocation, sessionID)
	if err != nil {
		return "", err
	}
	if strings.Contains(output.raw, "Error: Session not found") && sessionID != "" {
		// OpenCode mints its own session IDs; there is no way to create one
		// at a caller-chosen ID, the same limitation opencode-acp/
		// opencode-live already document. Retry once with no --session at
		// all rather than failing the invocation outright.
		output, resultSessionID, err = execute(ctx, config, invocation, "")
		if err != nil {
			return "", err
		}
	}

	if resultSessionID != "" && resultSessionID != config.SessionID {
		if err := saveOpenCodeSessionID(config.WorkDir, config.RuntimeID, resultSessionID); err != nil {
			return "", fmt.Errorf("opencode: persist session id: %w", err)
		}
	}

	if strings.TrimSpace(output.text) == "" && len(output.deniedTools) > 0 {
		return "", fmt.Errorf("agent produced no result after a permission request was denied for: %s",
			strings.Join(output.deniedTools, ", "))
	}
	return output.text, nil
}

type openCodeRunResult struct {
	raw         string
	text        string
	deniedTools []string
}

func runOpenCode(ctx context.Context, config Config, invocation model.Invocation, sessionID string) (openCodeRunResult, string, error) {
	config.SessionID = sessionID
	arguments := openCodeAdapter{}.Arguments(config)
	command := exec.CommandContext(ctx, config.Executable, arguments...)
	command.Dir = config.WorkDir
	command.Env = append(os.Environ(), opencodePermissionEnv(config.PermissionMode))
	command.Stdin = strings.NewReader(claudeUserPrompt(invocation))
	stdout := &boundedBuffer{limit: maxAgentOutputBytes}
	stderr := &boundedBuffer{limit: maxAgentOutputBytes}
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		return openCodeRunResult{}, "", fmt.Errorf("execution deadline reached: %w", ctx.Err())
	}
	if runErr != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = runErr.Error()
		}
		return openCodeRunResult{}, "", errors.New(truncateUTF8(detail, maxFailureReasonBytes))
	}
	if stdout.truncated {
		return openCodeRunResult{}, "", fmt.Errorf("agent output exceeded %d bytes", maxAgentOutputBytes)
	}

	return parseOpenCodeOutput(stdout.String())
}

func parseOpenCodeOutput(raw string) (openCodeRunResult, string, error) {
	result := openCodeRunResult{raw: raw}
	var resultSessionID string
	var text strings.Builder
	var lastTextMessageID string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event openCodeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Plain-text lines opencode prints alongside JSON events (e.g.
			// "permission requested: edit (...); auto-rejecting" or
			// "Error: Session not found") are not a parse failure — the raw
			// output is inspected separately for those signals.
			continue
		}
		if resultSessionID == "" && event.SessionID != "" {
			resultSessionID = event.SessionID
		}
		switch event.Type {
		case "text":
			// Confirmed live: separate assistant turns (distinct messageID)
			// arrive as separate "text" events with no whitespace of their
			// own between them — concatenating them raw glues sentences
			// together ("...first.Let me run..."). A blank line between
			// turns keeps each one readable without guessing at prose-level
			// punctuation.
			if text.Len() > 0 && lastTextMessageID != "" && event.Part.MessageID != lastTextMessageID {
				text.WriteString("\n\n")
			}
			text.WriteString(event.Part.Text)
			lastTextMessageID = event.Part.MessageID
		case "tool_use":
			if event.Part.State.Status == "error" && strings.Contains(event.Part.State.Error, "rejected permission") {
				result.deniedTools = append(result.deniedTools, event.Part.Tool)
			}
		}
	}
	result.text = text.String()
	return result, resultSessionID, nil
}

// openCodeSessionPath returns this runtime's locally-cached OpenCode
// session record for the plain exec adapter — deliberately a distinct file
// from opencode-live's own (openCodeLiveSessionPath), even though the
// underlying OpenCode session store is shared: these are different
// adapters and a runtime's session shouldn't cross-wire between them.
func openCodeSessionPath(workDir, runtimeID string) string {
	return filepath.Join(workDir, ".agent-comms", "cache", "opencode-session-"+runtimeID+".json")
}

func loadOpenCodeSessionID(workDir, runtimeID string) string {
	raw, err := os.ReadFile(openCodeSessionPath(workDir, runtimeID))
	if err != nil {
		return ""
	}
	var record struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return ""
	}
	return record.SessionID
}

func saveOpenCodeSessionID(workDir, runtimeID, sessionID string) error {
	path := openCodeSessionPath(workDir, runtimeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(struct {
		SessionID string `json:"session_id"`
	}{SessionID: sessionID})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
