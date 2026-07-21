package worker

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/acpclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	acpsdk "github.com/coder/acp-go-sdk"
)

// claudeACPAdapter drives Claude over the Agent Client Protocol via
// @agentclientprotocol/claude-agent-acp, an npm-distributed adapter that
// wraps the same claude CLI / Claude Agent SDK the exec-based claudeAdapter
// drives directly (confirmed session-store-compatible: a session/load with a
// given session ID resumes the same underlying Claude Code conversation
// --resume would). It is registered under a distinct adapter name
// ("claude-acp") rather than replacing claudeAdapter — the hand-rolled exec
// adapter is a proven, stable path this project depends on today, and ACP
// support is a new, separately-opted-into one.
//
// Unlike the exec adapter, permission decisions here happen per tool call
// over the protocol (session/request_permission) rather than as one
// upfront --permission-mode flag; acpclient.Session resolves them via the
// project's hybrid policy (see acpclient.Classify).
type claudeACPAdapter struct{}

func (claudeACPAdapter) Validate(config *Config) error {
	if config.Model != "" {
		return errors.New("claude-acp adapter does not yet support --model overrides")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("claude permission mode must not bypass permissions")
	}
	return nil
}

func (claudeACPAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	session, err := acpclient.Dial(ctx, acpclient.Config{
		Command:        "npx",
		Args:           []string{"-y", "@agentclientprotocol/claude-agent-acp"},
		Env:            os.Environ(),
		Dir:            config.WorkDir,
		Cwd:            config.WorkDir,
		AllowEdits:     func() bool { return config.PermissionMode == "acceptEdits" },
		Governance:     denyGovernance{},
		MaxOutputBytes: maxAgentOutputBytes,
		SessionMeta: map[string]any{
			"systemPrompt": map[string]any{"append": claudeSystemPrompt(config.Actor)},
		},
	}, config.SessionID)
	if err != nil {
		return "", fmt.Errorf("claude-acp: connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	output, stopReason, err := session.Prompt(ctx, claudeUserPrompt(invocation))
	if err != nil {
		return "", fmt.Errorf("claude-acp: %w", err)
	}
	return acpResult(output, stopReason, session)
}

// denyGovernance implements acpclient.GovernanceApprover by denying every
// request: this project has no existing per-tool-call blocking approval
// primitive (approval.request/approve is async and human-resolved
// out-of-band, not something an in-flight ACP turn can await), so tool calls
// the hybrid policy routes to governance — delete, execute, fetch, and any
// unrecognized kind — are rejected outright rather than silently granted.
// Loosening this to check the runtime's registered scopes/invocation policy,
// or to build a real blocking approval wait, is a deliberate future step,
// not an oversight.
type denyGovernance struct{}

func (denyGovernance) Approve(context.Context, string, acpsdk.ToolCallUpdate) (bool, error) {
	return false, nil
}
