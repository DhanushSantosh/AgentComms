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
	// Unlike claudeAdapter, which uses AgentCommsPath to scope unattended
	// Bash access to just the Agent Comms binary via
	// --allowedTools "Bash(<path> *)" -- agy's real CLI (agy --help) has
	// no equivalent per-tool allowlist flag at all, only the all-or-
	// nothing --dangerously-skip-permissions and --mode
	// accept-edits/plan. --claude-allow-agent-comms is a plain bool on
	// the shared `worker` command, not gated to the claude adapter the
	// way interactive-serve's own --claude-allow-agent-comms flag is
	// (app.go's withClaudeAllowAgentComms explicitly refuses a non-claude
	// wrapped command) -- so without this check, passing it alongside
	// --adapter agy would resolve and validate AgentCommsPath as a real
	// executable, giving the impression it scopes something, when
	// Arguments() below has nothing to actually pass it to. Reject it
	// outright instead, so misconfiguration fails loud at registration
	// time rather than silently doing nothing at every invocation.
	if config.AgentCommsPath != "" {
		return errors.New("agy has no per-tool permission scoping to apply --claude-allow-agent-comms to; omit that flag for this adapter")
	}
	return nil
}

func (agyAdapter) Arguments(config Config) []string {
	arguments := []string{
		"--print",
		"--output-format", "text",
	}
	switch config.PermissionMode {
	case "dontAsk", "auto":
		arguments = append(arguments, "--dangerously-skip-permissions")
	case "plan":
		arguments = append(arguments, "--mode", "plan")
	case "acceptEdits", "accept-edits":
		// The default (validateAgyConfig falls back to "acceptEdits" when
		// unset) and the most common case in practice -- must map to a
		// real flag. agy --help documents --mode as accepting exactly
		// "(accept-edits, plan)", so this is the one literal value it
		// understands for this permission level.
		arguments = append(arguments, "--mode", "accept-edits")
	case "manual":
		// No flag: agy's --mode only accepts accept-edits/plan, and
		// --print runs non-interactively, so there is no way to actually
		// prompt for per-action approval the way "manual" implies for
		// claude/opencode -- there's no TTY for agy to ask on. Leaving
		// both --mode and --dangerously-skip-permissions off falls back
		// to agy's own CLI default, confirmed live to respond promptly
		// rather than hang waiting on approval input it could never
		// receive here.
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
