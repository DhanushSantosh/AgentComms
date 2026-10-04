package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/opencodeclient"
)

// openCodeLiveAdapter drives OpenCode through a persistent `opencode
// serve` instance's own REST + SSE API instead of ACP. It exists for
// exactly one reason opencode-acp cannot deliver: confirmed live this
// session that a session driven through an isolated `opencode acp`
// subprocess never appears on a running server's SSE stream, even though
// it's readable afterward via REST polling — true live broadcast (a
// terminal running `opencode attach`, pointed at the same server, watching
// the session update as the invocation happens) requires driving the
// session through that same server, not a side-channel subprocess.
//
// Registered as "opencode-live", a third, distinct OpenCode adapter
// alongside "opencode-acp" — neither replaces the other. Use opencode-acp
// for ordinary automated invocations; use opencode-live specifically when
// someone needs to watch this runtime's activity live via `opencode
// attach`. Its server is private to one worker and persists across its turns,
// not past worker shutdown. No shared server URL is adopted or disposed.
type openCodeLiveAdapter struct {
	mu                                        sync.Mutex
	server                                    *ownedLiveServer
	start                                     func(context.Context, string) (*ownedLiveServer, error)
	workDir, runtimeID, recordPath, sessionID string
	runtimeLock, sessionLock                  *os.File
	closed                                    bool
	cleanupErr                                error
}

func (*openCodeLiveAdapter) Validate(config *Config) error {
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

func (a *openCodeLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (output string, result error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "", errors.New("opencode-live worker has closed")
	}
	if a.cleanupErr != nil {
		return "", fmt.Errorf("opencode-live owned cleanup requires attention: %w", a.cleanupErr)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := a.initialize(config); err != nil {
		return "", err
	}
	if a.server == nil {
		start := a.start
		if start == nil {
			start = startOpenCodeOwnedServer
		}
		server, err := start(ctx, a.workDir)
		if err != nil {
			return "", fmt.Errorf("opencode-live owned startup: %w", err)
		}
		a.server = server
	}
	server := a.server
	stopCancellation := context.AfterFunc(ctx, server.cancel)
	defer func() {
		stopCancellation()
		if result != nil || ctx.Err() != nil {
			result = errors.Join(result, ctx.Err(), a.closeServer())
		}
	}()
	client := opencodeclient.New(server.baseURL, a.workDir)
	if config.Status != nil {
		config.Status("preparing owned OpenCode runtime: waiting for native reset readiness and completion")
	}
	if err := client.DisposeInstance(ctx); err != nil {
		return "", fmt.Errorf("opencode-live owned instance reset failed; prompt refused: %w", err)
	}
	sessionID, agent, err := a.prepareSession(ctx, client, config)
	if err != nil {
		return "", fmt.Errorf("opencode-live preparation: %w", err)
	}
	if config.Status != nil {
		config.Status("watch this runtime's OpenCode activity live in a terminal: " + openCodeAttachCommand(server.baseURL, a.workDir, sessionID))
	}

	watcher := opencodeclient.NewPermissionWatcher(
		client,
		sessionID,
		func() bool { return config.PermissionMode == "acceptEdits" },
		denyGovernanceOpenCode{},
	)
	watchCtx, cancelWatch := context.WithCancel(ctx)
	watcherDone := make(chan struct{})
	events, err := opencodeclient.Subscribe(watchCtx, client)
	if err != nil {
		cancelWatch()
		return "", fmt.Errorf("opencode-live: subscribe to events: %w", err)
	}
	go func() { defer close(watcherDone); watcher.Run(watchCtx, events) }()
	defer func() { cancelWatch(); <-watcherDone }()
	watcher.ResetTurn()

	resp, err := client.Prompt(ctx, sessionID, opencodeclient.PromptRequest{
		Parts:  []opencodeclient.TextPart{opencodeclient.NewTextPart(claudeUserPrompt(invocation))},
		System: claudeSystemPrompt(config.Actor),
		Agent:  agent,
	})
	if err != nil {
		return "", fmt.Errorf("opencode-live: %w", err)
	}
	output = resp.Text()
	if strings.TrimSpace(output) == "" && watcher.Denied() {
		return "", fmt.Errorf("agent produced no result after a permission request was denied for: %s",
			strings.Join(watcher.DeniedKinds(), ", "))
	}
	return output, nil
}

func (a *openCodeLiveAdapter) closeServer() error {
	if a.server == nil {
		return nil
	}
	err := a.server.Close()
	a.server = nil
	if err != nil {
		a.cleanupErr = errors.Join(a.cleanupErr, err)
	}
	return err
}

// Close is called on every Worker.Run exit, including one-shot and errors.
func (a *openCodeLiveAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return a.cleanupErr
	}
	a.closed = true
	a.cleanupErr = errors.Join(a.cleanupErr, a.closeServer(), unlockOpenCodeFile(a.sessionLock), unlockOpenCodeFile(a.runtimeLock))
	a.sessionLock, a.runtimeLock = nil, nil
	return a.cleanupErr
}

// openCodeAttachCommand builds the exact `opencode attach` invocation that
// lands a terminal directly on this runtime's own session, confirmed live
// against `opencode attach --help`: --dir and --session are real, documented
// flags for exactly this. Reporting just the bare server URL isn't enough —
// confirmed live that opening it without --dir/--session lands on whatever
// project happened to be "current" on the server (which, for a long-lived
// server reused across many different working directories, is very often
// not this runtime's own project), showing an unrelated session instead.
func openCodeAttachCommand(baseURL, workDir, sessionID string) string {
	return fmt.Sprintf("opencode attach %s --dir %s --session %s", baseURL, workDir, sessionID)
}

// openCodeLiveSessionPath returns this runtime's locally-cached OpenCode
// session record, the same non-authoritative local-routing convention
// opencodeclient.ServerInfoPath uses for the server address itself: never
// part of the signed project event chain, just what lets a runtime with no
// explicit --session-id keep resuming its own conversation across
// invocations instead of starting a fresh one every time.
func openCodeLiveSessionPath(workDir, runtimeID string) string {
	return filepath.Join(workDir, ".agent-comms", "cache", "opencode-live-session-"+runtimeID+".json")
}

func loadOpenCodeLiveSessionID(workDir, runtimeID string) string {
	raw, err := os.ReadFile(openCodeLiveSessionPath(workDir, runtimeID))
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

func saveOpenCodeLiveSessionID(workDir, runtimeID, sessionID string) error {
	path := openCodeLiveSessionPath(workDir, runtimeID)
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

// denyGovernanceOpenCode implements opencodeclient.GovernanceApprover by
// denying every request, for the same reason denyGovernance does for the
// ACP-based adapters: this project has no per-tool-call blocking approval
// primitive to route it through instead.
type denyGovernanceOpenCode struct{}

func (denyGovernanceOpenCode) Approve(context.Context, string, string, []string) (bool, error) {
	return false, nil
}
