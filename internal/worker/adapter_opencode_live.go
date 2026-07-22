package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

// openCodeLiveAdapter drives OpenCode through a persistent `opencode
// serve` instance's own REST + SSE API instead of ACP. It exists for
// exactly one reason opencode-acp cannot deliver: confirmed live this
// session that a session driven through an isolated `opencode acp`
// subprocess never appears on a running server's SSE stream, even though
// it's readable afterward via REST polling — true live broadcast (a
// user's browser, pointed at the same server, watching OpenCode's own web
// UI update as the invocation happens) requires driving the session
// through that same server, not a side-channel subprocess.
//
// Registered as "opencode-live", a third, distinct OpenCode adapter
// alongside "opencode-acp" — neither replaces the other. Use opencode-acp
// for ordinary automated invocations; use opencode-live specifically when
// someone needs to watch this runtime's activity live in OpenCode's own
// UI. The persistent server this adapter starts survives past any single
// invocation by design (see opencodeclient.EnsureServer) — that's what
// keeps the browser URL stable across repeated invocations, the opposite
// lifecycle from every other adapter in this package.
type openCodeLiveAdapter struct{}

func (openCodeLiveAdapter) Validate(config *Config) error {
	if config.Model != "" {
		return errors.New("opencode-live adapter does not yet support --model overrides")
	}
	if config.PermissionMode == "" {
		config.PermissionMode = "acceptEdits"
	}
	switch config.PermissionMode {
	case "acceptEdits", "auto", "dontAsk", "manual", "plan":
	default:
		return errors.New("opencode-live permission mode must not bypass permissions")
	}
	return nil
}

func (openCodeLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
	baseURL, err := opencodeclient.EnsureServer(ctx, config.WorkDir, config.WorkDir)
	if err != nil {
		return "", fmt.Errorf("opencode-live: ensure server: %w", err)
	}
	config.Status("watch this runtime's OpenCode activity live at " + baseURL)
	client := opencodeclient.New(baseURL)

	sessionID := config.SessionID
	if sessionID != "" {
		if _, err := client.GetSession(ctx, sessionID); err != nil {
			return "", fmt.Errorf("opencode-live: resume session %s: %w", sessionID, err)
		}
	} else {
		session, err := client.CreateSession(ctx, config.WorkDir)
		if err != nil {
			return "", fmt.Errorf("opencode-live: create session: %w", err)
		}
		sessionID = session.ID
	}

	watcher := opencodeclient.NewPermissionWatcher(
		client,
		func() bool { return config.PermissionMode == "acceptEdits" },
		denyGovernanceOpenCode{},
	)
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	events, err := opencodeclient.Subscribe(watchCtx, client)
	if err != nil {
		return "", fmt.Errorf("opencode-live: subscribe to events: %w", err)
	}
	go watcher.Run(watchCtx, events)
	watcher.ResetTurn()

	resp, err := client.Prompt(ctx, sessionID, opencodeclient.PromptRequest{
		Parts:  []opencodeclient.TextPart{opencodeclient.NewTextPart(claudeUserPrompt(invocation))},
		System: claudeSystemPrompt(config.Actor),
	})
	if err != nil {
		return "", fmt.Errorf("opencode-live: %w", err)
	}
	output := resp.Text()
	if strings.TrimSpace(output) == "" && watcher.Denied() {
		return "", fmt.Errorf("agent produced no result after a permission request was denied for: %s",
			strings.Join(watcher.DeniedKinds(), ", "))
	}
	return output, nil
}

// denyGovernanceOpenCode implements opencodeclient.GovernanceApprover by
// denying every request, for the same reason denyGovernance does for the
// ACP-based adapters: this project has no per-tool-call blocking approval
// primitive to route it through instead.
type denyGovernanceOpenCode struct{}

func (denyGovernanceOpenCode) Approve(context.Context, string, string, []string) (bool, error) {
	return false, nil
}
