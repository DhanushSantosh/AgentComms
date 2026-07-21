package worker

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/acpclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// codexACPAdapter drives Codex over the Agent Client Protocol via
// @agentclientprotocol/codex-acp, an npm-distributed adapter that wraps the
// bundled @openai/codex CLI dependency. Registered as "codex-acp" — the
// working exec-based "codex" adapter is untouched.
//
// This adapter's permission story is weaker than claude-acp's or
// opencode-acp's, and that gap is deliberate, not an oversight:
//
//   - codex-acp exposes only three coarse mode presets (read-only, agent,
//     agent-full-access) via INITIAL_AGENT_MODE, not a per-category "always
//     ask" policy the way OpenCode's OPENCODE_PERMISSION provides. The
//     non-full-access presets use Codex's "on-request" approval policy,
//     which lets the model itself decide whether an action is risky enough
//     to raise session/request_permission — confirmed live that a plain
//     file-write-then-cat prompt never triggered a permission request under
//     either read-only or agent mode. So acpclient's hybrid
//     Classify/AllowEdits/Governance logic engages far less often here than
//     it does for the other two providers; the OS-level sandbox tied to the
//     chosen mode is the primary control for this provider, same as the
//     exec-based codexAdapter today (which also runs with
//     --ask-for-approval never and trusts --sandbox alone).
//   - CODEX_CONFIG, despite the npm package's README describing it as
//     merged into "the Codex session config," is — per its source —
//     forwarded only into gateway/provider auth config, not approval_policy
//     or sandbox_policy. It cannot be used to force a stricter policy than
//     the mode presets already give you.
//
// Only "read-only" and "agent" (workspace-write) presets are used here,
// selected by config.Sandbox exactly as the exec adapter already validates
// it — "agent-full-access" (Codex's `never`-approval, danger-full-access
// preset) is never selected, matching this project's standing rule against
// any bypass-permissions configuration.
type codexACPAdapter struct{}

func (codexACPAdapter) Validate(config *Config) error {
	if config.Model != "" {
		return errors.New("codex-acp adapter does not yet support --model overrides")
	}
	if config.Sandbox == "" {
		config.Sandbox = "workspace-write"
	}
	if config.Sandbox != "read-only" && config.Sandbox != "workspace-write" {
		return errors.New("codex-acp sandbox must be read-only or workspace-write")
	}
	return nil
}

func (codexACPAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	agentMode := "agent"
	if config.Sandbox == "read-only" {
		agentMode = "read-only"
	}
	allowEdits := config.Sandbox == "workspace-write"

	session, err := acpclient.Dial(ctx, acpclient.Config{
		Command:        "npx",
		Args:           []string{"-y", "@agentclientprotocol/codex-acp"},
		Env:            append(os.Environ(), "INITIAL_AGENT_MODE="+agentMode),
		Dir:            config.WorkDir,
		Cwd:            config.WorkDir,
		AllowEdits:     func() bool { return allowEdits },
		Governance:     denyGovernance{},
		MaxOutputBytes: maxAgentOutputBytes,
	}, config.SessionID)
	if err != nil {
		return "", fmt.Errorf("codex-acp: connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	output, stopReason, err := session.Prompt(ctx, codexPrompt(config.Actor, invocation))
	if err != nil {
		return "", fmt.Errorf("codex-acp: %w", err)
	}
	return acpResult(output, stopReason, session)
}
