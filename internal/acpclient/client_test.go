package acpclient

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

// fakeAgent implements the ACP Agent (and optional AgentLoader) interfaces
// with just enough behavior to exercise Session's plumbing: it echoes two
// message chunks on every prompt turn and, if requestKind is set, issues one
// permission request tagged with that ToolKind before finishing.
type fakeAgent struct {
	conn        *acpsdk.AgentSideConnection
	requestKind *acpsdk.ToolKind
	// replayText, if set, is sent as a SessionUpdate during LoadSession —
	// mirroring the real claude-agent-acp package, which replays prior
	// conversation turns as SessionUpdate notifications before session/load
	// returns.
	replayText string

	loadedSessionID string
	lastOutcome     acpsdk.RequestPermissionOutcome
}

var (
	_ acpsdk.Agent       = (*fakeAgent)(nil)
	_ acpsdk.AgentLoader = (*fakeAgent)(nil)
)

func (a *fakeAgent) Authenticate(context.Context, acpsdk.AuthenticateRequest) (acpsdk.AuthenticateResponse, error) {
	return acpsdk.AuthenticateResponse{}, nil
}

func (a *fakeAgent) Initialize(context.Context, acpsdk.InitializeRequest) (acpsdk.InitializeResponse, error) {
	return acpsdk.InitializeResponse{ProtocolVersion: acpsdk.ProtocolVersionNumber}, nil
}

func (a *fakeAgent) Logout(context.Context, acpsdk.LogoutRequest) (acpsdk.LogoutResponse, error) {
	return acpsdk.LogoutResponse{}, nil
}

func (a *fakeAgent) Cancel(context.Context, acpsdk.CancelNotification) error { return nil }

func (a *fakeAgent) CloseSession(context.Context, acpsdk.CloseSessionRequest) (acpsdk.CloseSessionResponse, error) {
	return acpsdk.CloseSessionResponse{}, nil
}

func (a *fakeAgent) ListSessions(context.Context, acpsdk.ListSessionsRequest) (acpsdk.ListSessionsResponse, error) {
	return acpsdk.ListSessionsResponse{}, nil
}

func (a *fakeAgent) NewSession(context.Context, acpsdk.NewSessionRequest) (acpsdk.NewSessionResponse, error) {
	return acpsdk.NewSessionResponse{SessionId: "fake-session-1"}, nil
}

func (a *fakeAgent) ResumeSession(context.Context, acpsdk.ResumeSessionRequest) (acpsdk.ResumeSessionResponse, error) {
	return acpsdk.ResumeSessionResponse{}, nil
}

func (a *fakeAgent) SetSessionConfigOption(context.Context, acpsdk.SetSessionConfigOptionRequest) (acpsdk.SetSessionConfigOptionResponse, error) {
	return acpsdk.SetSessionConfigOptionResponse{}, nil
}

func (a *fakeAgent) SetSessionMode(context.Context, acpsdk.SetSessionModeRequest) (acpsdk.SetSessionModeResponse, error) {
	return acpsdk.SetSessionModeResponse{}, nil
}

func (a *fakeAgent) LoadSession(ctx context.Context, p acpsdk.LoadSessionRequest) (acpsdk.LoadSessionResponse, error) {
	a.loadedSessionID = string(p.SessionId)
	if a.replayText != "" {
		if err := a.conn.SessionUpdate(ctx, acpsdk.SessionNotification{
			SessionId: p.SessionId,
			Update: acpsdk.SessionUpdate{
				AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{Content: acpsdk.TextBlock(a.replayText)},
			},
		}); err != nil {
			return acpsdk.LoadSessionResponse{}, err
		}
	}
	return acpsdk.LoadSessionResponse{}, nil
}

func (a *fakeAgent) Prompt(ctx context.Context, p acpsdk.PromptRequest) (acpsdk.PromptResponse, error) {
	for _, chunk := range []string{"Hello, ", "world."} {
		if err := a.conn.SessionUpdate(ctx, acpsdk.SessionNotification{
			SessionId: p.SessionId,
			Update: acpsdk.SessionUpdate{
				AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{Content: acpsdk.TextBlock(chunk)},
			},
		}); err != nil {
			return acpsdk.PromptResponse{}, err
		}
	}
	if a.requestKind != nil {
		resp, err := a.conn.RequestPermission(ctx, acpsdk.RequestPermissionRequest{
			SessionId: p.SessionId,
			Options: []acpsdk.PermissionOption{
				{OptionId: "allow-once", Kind: acpsdk.PermissionOptionKindAllowOnce, Name: "Allow"},
				{OptionId: "reject-once", Kind: acpsdk.PermissionOptionKindRejectOnce, Name: "Reject"},
			},
			ToolCall: acpsdk.ToolCallUpdate{ToolCallId: "tc-1", Kind: a.requestKind},
		})
		if err != nil {
			return acpsdk.PromptResponse{}, err
		}
		a.lastOutcome = resp.Outcome
	}
	return acpsdk.PromptResponse{StopReason: acpsdk.StopReasonEndTurn}, nil
}

// fixedApprover is a GovernanceApprover stub that always returns the
// configured decision, recording whether it was called.
type fixedApprover struct {
	approve bool
	called  bool
}

func (f *fixedApprover) Approve(context.Context, string, acpsdk.ToolCallUpdate) (bool, error) {
	f.called = true
	return f.approve, nil
}

func newLinkedSession(t *testing.T, config Config) (*Session, *fakeAgent) {
	t.Helper()
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()

	agent := &fakeAgent{}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })

	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })
	return session, agent
}

func baseConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Cwd:        t.TempDir(),
		AllowEdits: func() bool { return false },
		Governance: &fixedApprover{approve: true},
	}
}

func TestSessionHandshakeAndPromptAccumulatesText(t *testing.T) {
	session, _ := newLinkedSession(t, baseConfig(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if session.SessionID() != "fake-session-1" {
		t.Fatalf("unexpected session id: %q", session.SessionID())
	}
	text, stopReason, err := session.Prompt(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello, world." {
		t.Fatalf("unexpected accumulated text: %q", text)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Fatalf("unexpected stop reason: %q", stopReason)
	}
}

func TestSessionHandshakeResumesViaLoadSession(t *testing.T) {
	session, agent := newLinkedSession(t, baseConfig(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, "existing-session-9"); err != nil {
		t.Fatal(err)
	}
	if session.SessionID() != "existing-session-9" {
		t.Fatalf("unexpected session id: %q", session.SessionID())
	}
	if agent.loadedSessionID != "existing-session-9" {
		t.Fatalf("agent did not observe the resumed session id: %q", agent.loadedSessionID)
	}
}

func TestSessionAutoApprovesReadPermission(t *testing.T) {
	config := baseConfig(t)
	approver := &fixedApprover{approve: false}
	config.Governance = approver
	kind := acpsdk.ToolKindRead
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()
	agent := &fakeAgent{requestKind: &kind}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })
	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if approver.called {
		t.Fatal("read permission should never reach governance")
	}
	if agent.lastOutcome.Selected == nil || agent.lastOutcome.Selected.OptionId != "allow-once" {
		t.Fatalf("expected auto-approved read permission, got %+v", agent.lastOutcome)
	}
}

func TestSessionRoutesExecutePermissionThroughGovernance(t *testing.T) {
	config := baseConfig(t)
	approver := &fixedApprover{approve: false}
	config.Governance = approver
	kind := acpsdk.ToolKindExecute
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()
	agent := &fakeAgent{requestKind: &kind}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })
	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if !approver.called {
		t.Fatal("execute permission should be routed through governance")
	}
	if agent.lastOutcome.Selected == nil || agent.lastOutcome.Selected.OptionId != "reject-once" {
		t.Fatalf("expected governance-denied outcome, got %+v", agent.lastOutcome)
	}
}

func TestSessionModeGatedEditRespectsAllowEditsFalse(t *testing.T) {
	config := baseConfig(t)
	config.AllowEdits = func() bool { return false }
	kind := acpsdk.ToolKindEdit
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()
	agent := &fakeAgent{requestKind: &kind}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })
	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if agent.lastOutcome.Selected == nil || agent.lastOutcome.Selected.OptionId != "reject-once" {
		t.Fatalf("expected edit permission denied when AllowEdits is false, got %+v", agent.lastOutcome)
	}
}

func TestSessionModeGatedEditRespectsAllowEditsTrue(t *testing.T) {
	config := baseConfig(t)
	config.AllowEdits = func() bool { return true }
	kind := acpsdk.ToolKindEdit
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()
	agent := &fakeAgent{requestKind: &kind}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })
	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.Prompt(ctx, "hi"); err != nil {
		t.Fatal(err)
	}
	if agent.lastOutcome.Selected == nil || agent.lastOutcome.Selected.OptionId != "allow-once" {
		t.Fatalf("expected edit permission approved when AllowEdits is true, got %+v", agent.lastOutcome)
	}
}

func TestSessionResumePromptExcludesReplayedHistory(t *testing.T) {
	config := baseConfig(t)
	clientRead, agentWrite := io.Pipe()
	agentRead, clientWrite := io.Pipe()
	agent := &fakeAgent{replayText: "this is replayed history from a prior turn"}
	agentConn := acpsdk.NewAgentSideConnection(agent, agentWrite, agentRead)
	agent.conn = agentConn
	t.Cleanup(func() { _ = agentWrite.Close(); _ = agentRead.Close() })
	session := newPipeSession(config, clientWrite, clientRead)
	t.Cleanup(func() { _ = clientWrite.Close(); _ = clientRead.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, "prior-session-1"); err != nil {
		t.Fatal(err)
	}
	text, _, err := session.Prompt(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello, world." {
		t.Fatalf("expected replayed history excluded from prompt result, got %q", text)
	}
}

func TestSessionOutputTruncatesAtMaxBytes(t *testing.T) {
	config := baseConfig(t)
	config.MaxOutputBytes = 5
	session, _ := newLinkedSession(t, config)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := session.handshake(ctx, ""); err != nil {
		t.Fatal(err)
	}
	text, _, err := session.Prompt(ctx, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello" {
		t.Fatalf("expected output truncated to 5 bytes, got %q", text)
	}
	if !session.Truncated() {
		t.Fatal("expected Truncated to report true")
	}
}

func TestDialRequiresCommand(t *testing.T) {
	_, err := Dial(context.Background(), baseConfig(t), "")
	if err == nil {
		t.Fatal("expected an error when Command is empty")
	}
}

func TestConfigValidateRequiresCwdAllowEditsAndGovernance(t *testing.T) {
	cases := []Config{
		{AllowEdits: func() bool { return true }, Governance: &fixedApprover{}},
		{Cwd: "/tmp", Governance: &fixedApprover{}},
		{Cwd: "/tmp", AllowEdits: func() bool { return true }},
	}
	for _, config := range cases {
		if err := config.validate(); err == nil {
			t.Fatalf("expected validation error for %+v", config)
		}
	}
}

func TestClassifyRoutesEveryToolKind(t *testing.T) {
	cases := map[acpsdk.ToolKind]Decision{
		acpsdk.ToolKindRead:       DecisionAutoApprove,
		acpsdk.ToolKindSearch:     DecisionAutoApprove,
		acpsdk.ToolKindThink:      DecisionAutoApprove,
		acpsdk.ToolKindSwitchMode: DecisionAutoApprove,
		acpsdk.ToolKindEdit:       DecisionModeGated,
		acpsdk.ToolKindMove:       DecisionModeGated,
		acpsdk.ToolKindDelete:     DecisionGoverned,
		acpsdk.ToolKindExecute:    DecisionGoverned,
		acpsdk.ToolKindFetch:      DecisionGoverned,
		acpsdk.ToolKindOther:      DecisionGoverned,
		acpsdk.ToolKind("future"): DecisionGoverned,
	}
	for kind, want := range cases {
		if got := Classify(kind); got != want {
			t.Errorf("Classify(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestUnsupportedClientCapabilitiesReturnError(t *testing.T) {
	session, _ := newLinkedSession(t, baseConfig(t))
	if _, err := session.ReadTextFile(context.Background(), acpsdk.ReadTextFileRequest{Path: "/x", SessionId: "s"}); !errors.Is(err, errNotSupported) {
		t.Fatalf("expected errNotSupported, got %v", err)
	}
}
