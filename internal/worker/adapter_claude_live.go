package worker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// claudeLiveAdapter drives one persistent Claude stream-json process through
// the local claudeserve broker so observers can watch without controlling it.
type claudeLiveAdapter struct{}

func (claudeLiveAdapter) Validate(config *Config) error {
	if config.SessionID == "" {
		return errors.New("claude-live requires --session-id for restart-safe conversation continuity")
	}
	return validateClaudeConfig(config)
}

func (claudeLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	baseURL, err := claudeserve.EnsureServer(ctx, config.WorkDir, config.WorkDir)
	if err != nil {
		return "", fmt.Errorf("claude-live: ensure broker: %w", err)
	}
	claudeExecutable, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude-live: locate Claude executable: %w", err)
	}
	client := claudeserve.New(baseURL)
	if err := client.Register(ctx, config.RuntimeID, claudeserve.ProcessConfig{
		Executable: claudeExecutable, WorkDir: config.WorkDir,
		PermissionMode: config.PermissionMode, SystemPrompt: claudeSystemPrompt(config.Actor),
		AgentCommsPath: config.AgentCommsPath, Model: config.Model,
		SessionID: config.SessionID, MaxBudgetUSD: config.ClaudeBudgetUSD,
	}); err != nil {
		return "", fmt.Errorf("claude-live: register runtime: %w", err)
	}
	config.Status("watch this runtime's Claude activity live in a terminal: agent-comms live attach --provider claude --runtime " + config.RuntimeID + " --server " + baseURL)
	output, err := client.Prompt(ctx, config.RuntimeID, claudeUserPrompt(invocation))
	if err != nil {
		return "", fmt.Errorf("claude-live: %w", err)
	}
	return output, nil
}
