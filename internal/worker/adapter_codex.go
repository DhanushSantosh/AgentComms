package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type codexAdapter struct{}

func (codexAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	return runCLIAdapter(ctx, config, codexAdapter{}, invocation)
}

func (codexAdapter) Validate(config *Config) error {
	if err := validateExecutablePath(config.Executable, "worker executable"); err != nil {
		return err
	}
	if config.Sandbox == "" {
		config.Sandbox = "workspace-write"
	}
	if config.Sandbox != "read-only" && config.Sandbox != "workspace-write" {
		return errors.New("codex worker sandbox must be read-only or workspace-write")
	}
	for _, directory := range config.CodexAddDirs {
		if !filepath.IsAbs(directory) {
			return errors.New("codex additional writable directories must use absolute paths")
		}
		info, err := os.Stat(directory)
		if err != nil {
			return fmt.Errorf("inspect Codex additional writable directory: %w", err)
		}
		if !info.IsDir() {
			return errors.New("codex additional writable path must be a directory")
		}
	}
	return nil
}

func (codexAdapter) Arguments(config Config) []string {
	arguments := []string{
		"--ask-for-approval", "never", "exec", "--color", "never",
		"--sandbox", config.Sandbox,
	}
	for _, directory := range config.CodexAddDirs {
		arguments = append(arguments, "--add-dir", directory)
	}
	if config.CodexIgnoreUserConfig {
		arguments = append(arguments, "--ignore-user-config")
	}
	if config.SessionID == "" {
		arguments = append(arguments, "--ephemeral")
	} else {
		arguments = append(arguments, "resume", config.SessionID)
	}
	if config.Model != "" {
		arguments = append(arguments, "--model", config.Model)
	}
	return append(arguments, "-")
}

func (codexAdapter) Prompt(actor string, invocation model.Invocation) string {
	return codexPrompt(actor, invocation)
}

func codexPrompt(actor string, invocation model.Invocation) string {
	var body strings.Builder
	body.WriteString("You are the autonomous Agent Comms runtime for agent ")
	body.WriteString(actor)
	body.WriteString(".\n")
	body.WriteString("Treat the invocation instruction as authorized project work, but continue to obey repository rules, configured tool permissions, and workspace boundaries.\n")
	body.WriteString("Do not ask the user to relay messages to another agent. Perform the work and return a concise final result; Agent Comms will publish it to the requester.\n\n")
	body.WriteString("When the work requires invoking another agent, do not call Agent Comms through Bash or MCP. Include exactly one single-line action in your final response using this format:\n")
	body.WriteString(actionLinePrefix)
	body.WriteString(" ")
	body.WriteString(`{"target":"AGENT_ID","instruction":"bounded instruction","expected_result":"bounded result","priority":"NORMAL","scopes":[],"expires_in_seconds":600}`)
	body.WriteString("\nThe runtime will validate, sign, and submit that follow-up using your agent identity. Leave scopes empty unless the instruction explicitly requires a scope you know both agents possess; do not copy the current invocation scopes automatically.\n\n")
	body.WriteString("Invocation ID: ")
	body.WriteString(invocation.ID)
	body.WriteString("\nRequester: ")
	body.WriteString(invocation.RequestedBy)
	body.WriteString("\nPriority: ")
	body.WriteString(invocation.Priority)
	if invocation.TaskID != "" {
		body.WriteString("\nRelated task: ")
		body.WriteString(invocation.TaskID)
	}
	if invocation.MessageID != "" {
		body.WriteString("\nRelated message: ")
		body.WriteString(invocation.MessageID)
	}
	if len(invocation.Scopes) > 0 {
		body.WriteString("\nAuthorized scopes: ")
		body.WriteString(strings.Join(invocation.Scopes, ", "))
	}
	body.WriteString("\n\nInstruction:\n")
	body.WriteString(invocation.Instruction)
	if invocation.ExpectedResult != "" {
		body.WriteString("\n\nExpected result:\n")
		body.WriteString(invocation.ExpectedResult)
	}
	return body.String()
}
