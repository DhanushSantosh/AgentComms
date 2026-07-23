package worker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/claudeserve"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type claudeAdapter struct{}

func (claudeAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	return runCLIAdapter(ctx, config, claudeAdapter{}, invocation)
}

func (claudeAdapter) Validate(config *Config) error {
	if err := validateExecutablePath(config.Executable, "worker executable"); err != nil {
		return err
	}
	return validateClaudeConfig(config)
}

func validateClaudeConfig(config *Config) error {
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("claude permission mode must not bypass permissions")
	}
	if config.ClaudeBudgetUSD <= 0 || config.ClaudeBudgetUSD > maxClaudeBudgetUSD {
		return fmt.Errorf("claude budget must be greater than 0 and at most %.0f USD", float64(maxClaudeBudgetUSD))
	}
	if config.AgentCommsPath != "" {
		if err := validateExecutablePath(config.AgentCommsPath, "allowed Agent Comms executable"); err != nil {
			return err
		}
	}
	return nil
}

func (claudeAdapter) Arguments(config Config) []string {
	arguments := []string{
		"--print", "--output-format", "text",
		"--append-system-prompt", claudeSystemPrompt(config.Actor),
		"--permission-mode", config.PermissionMode,
		"--max-budget-usd", strconv.FormatFloat(config.ClaudeBudgetUSD, 'f', 2, 64),
	}
	switch {
	case config.SessionID == "":
		arguments = append(arguments, "--no-session-persistence")
	case claudeserve.SessionExists(config.WorkDir, config.SessionID):
		arguments = append(arguments, "--resume", config.SessionID)
	default:
		// First invocation a runtime makes with a bound session ID: `--resume`
		// fails outright on an ID with no conversation behind it yet
		// ("No conversation found"), confirmed live against the real claude
		// binary. `--session-id` creates the conversation at that exact ID
		// instead, so every later invocation's `claudeSessionExists` check
		// finds it and resumes it normally from then on.
		arguments = append(arguments, "--session-id", config.SessionID)
	}
	if config.AgentCommsPath != "" {
		arguments = append(arguments, "--allowedTools", "Bash("+config.AgentCommsPath+" *)")
	}
	if config.Model != "" {
		arguments = append(arguments, "--model", config.Model)
	}
	return arguments
}

func (claudeAdapter) Prompt(_ string, invocation model.Invocation) string {
	return claudeUserPrompt(invocation)
}

// claudeSystemPrompt carries the runtime's operating convention on the
// trusted system-prompt channel instead of the first user turn. Blending this
// framing into user content reads as a self-authorizing instruction smuggled
// into the conversation, which Claude correctly treats as a likely prompt
// injection and refuses; appending it as a system prompt avoids that false
// positive while keeping the invocation body itself as plain user content.
func claudeSystemPrompt(actor string) string {
	var body strings.Builder
	body.WriteString("You are ")
	body.WriteString(actor)
	body.WriteString(", an agent registered with Agent Comms, a governed multi-agent coordination tool. This message describes a standing, audited operating convention for invocations you receive through it; it does not itself grant, alter, or bypass any tool permission.\n")
	body.WriteString("Each user turn carries one Agent Comms invocation already authorized by project governance. Complete the described work under your normal repository rules, configured tool permissions, and workspace boundaries.\n")
	body.WriteString("Do not ask the user to relay messages to another agent; perform the work and return a concise final result, and Agent Comms will publish it to the requester.\n\n")
	body.WriteString("Only if completing the work genuinely requires another registered Agent Comms agent's help, you may end your response with exactly one line in this format (do not invoke Agent Comms via Bash or MCP for this):\n")
	body.WriteString(actionLinePrefix)
	body.WriteString(" ")
	body.WriteString(`{"target":"AGENT_ID","instruction":"bounded instruction","expected_result":"bounded result","priority":"NORMAL","scopes":[],"expires_in_seconds":600}`)
	body.WriteString("\nAgent Comms validates, signs, and submits that follow-up under your own agent identity; it never executes arbitrary content. Leave scopes empty unless the instruction explicitly requires a scope you know both agents possess — never copy the current invocation's scopes automatically. Omit the line entirely when no follow-up is needed.")
	return body.String()
}

// claudeUserPrompt carries only the concrete task details for this
// invocation; the operating convention lives in claudeSystemPrompt instead.
func claudeUserPrompt(invocation model.Invocation) string {
	var body strings.Builder
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
