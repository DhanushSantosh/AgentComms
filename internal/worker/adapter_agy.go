package worker

import (
	"context"
	"errors"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type agyAdapter struct{}

func (agyAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	return runCLIAdapter(ctx, config, agyAdapter{}, invocation)
}

func (agyAdapter) Validate(config *Config) error {
	if err := validateExecutablePath(config.Executable, "worker executable"); err != nil {
		return err
	}
	return validateAgyConfig(config)
}

func validateAgyConfig(config *Config) error {
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "accept-edits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("agy permission mode must not bypass permissions")
	}
	if config.AgentCommsPath != "" {
		if err := validateExecutablePath(config.AgentCommsPath, "allowed Agent Comms executable"); err != nil {
			return err
		}
	}
	return nil
}

func (agyAdapter) Arguments(config Config) []string {
	arguments := []string{
		"--print",
		"--output-format", "text",
	}
	if config.PermissionMode == "dontAsk" || config.PermissionMode == "auto" {
		arguments = append(arguments, "--dangerously-skip-permissions")
	} else if config.PermissionMode == "plan" {
		arguments = append(arguments, "--mode", "plan")
	}
	if config.SessionID != "" {
		arguments = append(arguments, "--conversation", config.SessionID)
	}
	if config.Model != "" {
		arguments = append(arguments, "--model", config.Model)
	}
	return arguments
}

func (agyAdapter) Prompt(actor string, invocation model.Invocation) string {
	return agyPrompt(actor, invocation)
}

func agyPrompt(actor string, invocation model.Invocation) string {
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
	body.WriteString("\n\nInstruction:\n")
	body.WriteString(invocation.Instruction)
	return body.String()
}
