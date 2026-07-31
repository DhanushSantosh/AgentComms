// Package acpclient provides the provider-agnostic Agent Client Protocol
// (ACP) plumbing that later ACP-backed worker adapters build on: spawning an
// ACP agent subprocess (or, in tests, wiring directly to an in-process
// fake), completing the initialize/session handshake, running one prompt
// turn, and resolving permission requests through the project's hybrid
// approval model (see Classify). It does not know about Claude, Codex, or
// OpenCode specifically — that belongs to the worker.Adapter implementations
// that use this package.
package acpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	acpsdk "github.com/coder/acp-go-sdk"
)

// GovernanceApprover resolves a permission request that Classify has routed
// to real Agent Comms governance rather than deciding locally (ToolKind
// delete, execute, fetch, or other). Implementations should apply the same
// approval semantics as any other governed Agent Comms action.
type GovernanceApprover interface {
	Approve(ctx context.Context, sessionID string, toolCall acpsdk.ToolCallUpdate) (bool, error)
}

// EditGate reports whether edit/move-shaped tool calls (ToolKindEdit,
// ToolKindMove) may be auto-approved for the current invocation — the ACP
// analogue of Claude's acceptEdits permission mode.
type EditGate func() bool

// Config configures a Session's connection to an ACP-speaking provider
// process and how it resolves permission requests.
type Config struct {
	// Command and Args launch the ACP agent subprocess (an ACP adapter
	// binary or a provider CLI with native ACP support). Required for Dial.
	Command string
	Args    []string
	Env     []string
	Dir     string

	// Cwd is the workspace root reported to the agent in session/new or
	// session/load. Required; should match Dir.
	Cwd string

	// AllowEdits reports whether edit/move tool calls may be auto-approved
	// for this invocation. Required.
	AllowEdits EditGate
	// Governance resolves tool calls Classify routes to governance. Required.
	Governance GovernanceApprover

	// OnUpdate, if set, is invoked for every streamed session update —
	// callers use it to surface progress the way Worker.Status does today.
	OnUpdate func(acpsdk.SessionUpdate)

	// MaxOutputBytes bounds the accumulated agent-message text collected
	// across a prompt turn. Zero disables bounding.
	MaxOutputBytes int

	// SessionMeta, if non-nil, is attached as session/new's _meta field
	// verbatim. acpclient does not interpret it — it exists so a specific
	// provider adapter can pass provider-specific extensions (for example,
	// the Claude ACP adapter's `_meta.systemPrompt` convention) without this
	// package needing to know about any one provider. Not resent on
	// session/load: a resumed session already carries whatever meta shaped
	// its first turn.
	SessionMeta map[string]any
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Cwd) == "" {
		return errors.New("acpclient: cwd is required")
	}
	if c.AllowEdits == nil {
		return errors.New("acpclient: AllowEdits gate is required")
	}
	if c.Governance == nil {
		return errors.New("acpclient: governance approver is required")
	}
	return nil
}

// Session is a live, per-invocation connection to an ACP agent: spawn,
// initialize, open (or resume) one session, run exactly one prompt turn,
// then Close. Agent Comms opens a fresh Session per invocation — the same
// lifecycle its exec-based Claude/Codex adapters already use — so ACP
// adapters introduce no new failure mode around stale or leaked connections.
type Session struct {
	config    Config
	cmd       *exec.Cmd
	conn      *acpsdk.ClientSideConnection
	sessionID acpsdk.SessionId

	mu          sync.Mutex
	output      strings.Builder
	truncated   bool
	deniedKinds []string
}

var _ acpsdk.Client = (*Session)(nil)

// Dial spawns the configured ACP agent subprocess and completes the
// protocol handshake: initialize, then session/new, or session/load when
// resumeSessionID is non-empty.
func Dial(ctx context.Context, config Config, resumeSessionID string) (*Session, error) {
	if config.Command == "" {
		return nil, errors.New("acpclient: command is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = config.Dir
	cmd.Env = config.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acpclient: open stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acpclient: open stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acpclient: start agent process: %w", err)
	}
	session := &Session{config: config, cmd: cmd}
	session.conn = acpsdk.NewClientSideConnection(session, stdin, stdout)
	if err := session.handshake(ctx, resumeSessionID); err != nil {
		_ = session.Close()
		return nil, err
	}
	return session, nil
}

// newPipeSession wires a Session directly to a peer input/output pair,
// bypassing process spawning, so the protocol plumbing can be exercised
// against an in-process fake agent in tests.
func newPipeSession(config Config, peerInput io.Writer, peerOutput io.Reader) *Session {
	session := &Session{config: config}
	session.conn = acpsdk.NewClientSideConnection(session, peerInput, peerOutput)
	return session
}

func (s *Session) handshake(ctx context.Context, resumeSessionID string) error {
	if err := s.config.validate(); err != nil {
		return err
	}
	if _, err := s.conn.Initialize(ctx, acpsdk.InitializeRequest{
		ProtocolVersion: acpsdk.ProtocolVersionNumber,
	}); err != nil {
		return fmt.Errorf("acpclient: initialize: %w", err)
	}
	if resumeSessionID != "" {
		if _, err := s.conn.LoadSession(ctx, acpsdk.LoadSessionRequest{
			SessionId:  acpsdk.SessionId(resumeSessionID),
			Cwd:        s.config.Cwd,
			McpServers: []acpsdk.McpServer{},
		}); err != nil {
			return fmt.Errorf("acpclient: load session %s: %w", resumeSessionID, err)
		}
		s.sessionID = acpsdk.SessionId(resumeSessionID)
		return nil
	}
	resp, err := s.conn.NewSession(ctx, acpsdk.NewSessionRequest{
		Cwd:        s.config.Cwd,
		McpServers: []acpsdk.McpServer{},
		Meta:       s.config.SessionMeta,
	})
	if err != nil {
		return fmt.Errorf("acpclient: new session: %w", err)
	}
	s.sessionID = resp.SessionId
	return nil
}

// SessionID returns the provider-native session identifier this Session
// should be resumed with next time. Stable whether it came from Dial's
// resume path or a fresh session/new.
func (s *Session) SessionID() string { return string(s.sessionID) }

// Prompt runs one prompt turn and returns the concatenated agent-message
// text streamed during it, along with the stop reason. The accumulated text
// is returned even when the call itself errors, so callers can inspect
// partial output the way the exec-based adapters expose partial stdout.
//
// The output accumulator is reset before the request is sent: session/load
// replays prior conversation turns as the same SessionUpdate notifications a
// live turn uses, and without this reset that replayed history — or a
// previous Prompt call's leftover text — would bleed into this turn's
// result.
func (s *Session) Prompt(ctx context.Context, text string) (string, acpsdk.StopReason, error) {
	s.resetTurn()
	resp, err := s.conn.Prompt(ctx, acpsdk.PromptRequest{
		SessionId: s.sessionID,
		Prompt:    []acpsdk.ContentBlock{acpsdk.TextBlock(text)},
	})
	if err != nil {
		return s.result(), "", fmt.Errorf("acpclient: prompt: %w", err)
	}
	return s.result(), resp.StopReason, nil
}

func (s *Session) resetTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output.Reset()
	s.truncated = false
	s.deniedKinds = nil
}

func (s *Session) result() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output.String()
}

// Truncated reports whether the accumulated output was cut off at
// Config.MaxOutputBytes.
func (s *Session) Truncated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.truncated
}

// Denied reports whether any permission request was refused (mode-gated or
// governance) during the most recent Prompt call. A denial does not fail
// Prompt itself — the agent may still produce a useful response after being
// told no — but combined with empty output it usually means the agent gave
// up silently rather than explaining it couldn't proceed, which callers
// should treat as a failure rather than a vacuous success.
func (s *Session) Denied() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.deniedKinds) > 0
}

// DeniedKinds returns the ToolKind of every permission request refused
// during the most recent Prompt call, in the order they were denied.
func (s *Session) DeniedKinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deniedKinds...)
}

func (s *Session) recordDenied(kind acpsdk.ToolKind) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deniedKinds = append(s.deniedKinds, string(kind))
}

// Cancel sends session/cancel for the in-flight prompt turn.
func (s *Session) Cancel(ctx context.Context) error {
	return s.conn.Cancel(ctx, acpsdk.CancelNotification{SessionId: s.sessionID})
}

// Close terminates the agent subprocess spawned by Dial. It is a no-op for
// pipe-wired sessions, which own no process.
func (s *Session) Close() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Kill()
}

// SessionUpdate implements acpsdk.Client: it accumulates agent-message text
// and, if configured, forwards every update to Config.OnUpdate for progress
// reporting.
func (s *Session) SessionUpdate(_ context.Context, n acpsdk.SessionNotification) error {
	if s.config.OnUpdate != nil {
		s.config.OnUpdate(n.Update)
	}
	if text := chunkText(n.Update); text != "" {
		s.appendOutput(text)
	}
	return nil
}

func chunkText(update acpsdk.SessionUpdate) string {
	if update.AgentMessageChunk == nil || update.AgentMessageChunk.Content.Text == nil {
		return ""
	}
	return update.AgentMessageChunk.Content.Text.Text
}

func (s *Session) appendOutput(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.MaxOutputBytes > 0 {
		remaining := s.config.MaxOutputBytes - s.output.Len()
		if remaining <= 0 {
			s.truncated = true
			return
		}
		if len(text) > remaining {
			text = text[:remaining]
			s.truncated = true
		}
	}
	s.output.WriteString(text)
}

// RequestPermission implements acpsdk.Client using the hybrid approval
// model: Classify routes the request's ToolKind to an automatic decision, a
// mode-gated decision via Config.AllowEdits, or Config.Governance.
func (s *Session) RequestPermission(ctx context.Context, p acpsdk.RequestPermissionRequest) (acpsdk.RequestPermissionResponse, error) {
	approved, err := s.decide(ctx, p)
	if err != nil {
		return acpsdk.RequestPermissionResponse{}, err
	}
	if !approved {
		kind := acpsdk.ToolKindOther
		if p.ToolCall.Kind != nil {
			kind = *p.ToolCall.Kind
		}
		s.recordDenied(kind)
	}
	optionID, ok := selectOption(p.Options, approved)
	if !ok {
		return acpsdk.RequestPermissionResponse{
			Outcome: acpsdk.RequestPermissionOutcome{Cancelled: &acpsdk.RequestPermissionOutcomeCancelled{}},
		}, nil
	}
	return acpsdk.RequestPermissionResponse{
		Outcome: acpsdk.RequestPermissionOutcome{
			Selected: &acpsdk.RequestPermissionOutcomeSelected{OptionId: optionID},
		},
	}, nil
}

func (s *Session) decide(ctx context.Context, p acpsdk.RequestPermissionRequest) (bool, error) {
	kind := acpsdk.ToolKindOther
	if p.ToolCall.Kind != nil {
		kind = *p.ToolCall.Kind
	}
	switch Classify(kind) {
	case DecisionAutoApprove:
		return true, nil
	case DecisionModeGated:
		return s.config.AllowEdits(), nil
	default:
		return s.config.Governance.Approve(ctx, string(p.SessionId), p.ToolCall)
	}
}

// selectOption picks the option matching the decision, preferring a
// once-only option over an always option so a single decision here never
// silently grants standing permission beyond this tool call.
func selectOption(options []acpsdk.PermissionOption, approve bool) (acpsdk.PermissionOptionId, bool) {
	wantKinds := []acpsdk.PermissionOptionKind{acpsdk.PermissionOptionKindRejectOnce, acpsdk.PermissionOptionKindRejectAlways}
	if approve {
		wantKinds = []acpsdk.PermissionOptionKind{acpsdk.PermissionOptionKindAllowOnce, acpsdk.PermissionOptionKindAllowAlways}
	}
	for _, want := range wantKinds {
		for _, option := range options {
			if option.Kind == want {
				return option.OptionId, true
			}
		}
	}
	return "", false
}

// errNotSupported is returned by the filesystem and terminal Client methods.
// ACP adapters run inside the same sandboxed workspace Agent Comms already
// controls, so Agent Comms does not additionally expose its filesystem or a
// terminal to the provider process through the protocol; Initialize does not
// advertise those capabilities, so a well-behaved agent never calls these.
var errNotSupported = errors.New("acpclient: capability not offered to this session")

func (s *Session) ReadTextFile(context.Context, acpsdk.ReadTextFileRequest) (acpsdk.ReadTextFileResponse, error) {
	return acpsdk.ReadTextFileResponse{}, errNotSupported
}

func (s *Session) WriteTextFile(context.Context, acpsdk.WriteTextFileRequest) (acpsdk.WriteTextFileResponse, error) {
	return acpsdk.WriteTextFileResponse{}, errNotSupported
}

func (s *Session) CreateTerminal(context.Context, acpsdk.CreateTerminalRequest) (acpsdk.CreateTerminalResponse, error) {
	return acpsdk.CreateTerminalResponse{}, errNotSupported
}

func (s *Session) KillTerminal(context.Context, acpsdk.KillTerminalRequest) (acpsdk.KillTerminalResponse, error) {
	return acpsdk.KillTerminalResponse{}, errNotSupported
}

func (s *Session) ReleaseTerminal(context.Context, acpsdk.ReleaseTerminalRequest) (acpsdk.ReleaseTerminalResponse, error) {
	return acpsdk.ReleaseTerminalResponse{}, errNotSupported
}

func (s *Session) TerminalOutput(context.Context, acpsdk.TerminalOutputRequest) (acpsdk.TerminalOutputResponse, error) {
	return acpsdk.TerminalOutputResponse{}, errNotSupported
}

func (s *Session) WaitForTerminalExit(context.Context, acpsdk.WaitForTerminalExitRequest) (acpsdk.WaitForTerminalExitResponse, error) {
	return acpsdk.WaitForTerminalExitResponse{}, errNotSupported
}
