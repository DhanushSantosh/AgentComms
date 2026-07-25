package worker

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/acpclient"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// openCodeACPPermissionEnv forces OpenCode's tool-category permission
// config to "ask" for every category that isn't purely read-only, via the
// OPENCODE_PERMISSION env var (JSON-merged over whatever config OpenCode
// would otherwise resolve). Confirmed live: without this, a --pure
// `opencode acp` process never calls session/request_permission at all —
// it silently performs edits and shell commands — so this is not an
// optional hardening step, it's what makes acpclient's hybrid policy
// (Classify/AllowEdits/Governance) engage at all for this provider.
// read/glob/grep/list/lsp are left alone: acpclient's own Classify already
// auto-approves the read/search-shaped ToolKinds regardless of what
// OpenCode itself would have decided, so there is nothing to gain by also
// forcing those categories to ask.
const openCodeACPPermissionEnv = `OPENCODE_PERMISSION={"edit":"ask","bash":"ask","webfetch":"ask","websearch":"ask","external_directory":"ask","task":"ask"}`

// openCodeACPAdapter drives OpenCode over the Agent Client Protocol via its
// native `opencode acp` subcommand — unlike Claude, no separate npm package
// is needed. Confirmed live: the transport is stdio despite --port/
// --hostname/--mdns looking server-oriented (those are for an unrelated
// mode); --pure is required for a usable startup time, since without it
// OpenCode loads every globally-configured plugin before the ACP handshake
// completes; session/load correctly resumes a prior session's context.
//
// Registered as "opencode-acp", distinct from the plain exec-based
// "opencode" adapter (adapter_opencode.go) — the name stays consistent with
// "claude-acp" alongside a provider that now has both, the same as
// claude/codex already do.
type openCodeACPAdapter struct{}

func (openCodeACPAdapter) Validate(config *Config) error {
	if config.Model != "" {
		return errors.New("opencode-acp adapter does not yet support --model overrides")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("opencode-acp permission mode must not bypass permissions")
	}
	return nil
}

func (openCodeACPAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	session, err := acpclient.Dial(ctx, acpclient.Config{
		Command:        "opencode",
		Args:           []string{"acp", "--cwd", config.WorkDir, "--pure"},
		Env:            append(os.Environ(), openCodeACPPermissionEnv),
		Dir:            config.WorkDir,
		Cwd:            config.WorkDir,
		AllowEdits:     func() bool { return config.PermissionMode == "acceptEdits" },
		Governance:     denyGovernance{},
		MaxOutputBytes: maxAgentOutputBytes,
	}, config.SessionID)
	if err != nil {
		return "", fmt.Errorf("opencode-acp: connect: %w", err)
	}
	defer func() { _ = session.Close() }()

	output, stopReason, err := session.Prompt(ctx, codexPrompt(config.Actor, invocation))
	if err != nil {
		return "", fmt.Errorf("opencode-acp: %w", err)
	}
	return acpResult(output, stopReason, session)
}
