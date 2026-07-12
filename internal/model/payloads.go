package model

import (
	"encoding/json"
	"fmt"
	"time"
)

type Role string

const (
	RoleOwner        Role = "OWNER"
	RoleOrchestrator Role = "ORCHESTRATOR"
	RoleAgent        Role = "AGENT"
	RoleObserver     Role = "OBSERVER"
)

type PrincipalType string

const (
	PrincipalHuman PrincipalType = "HUMAN"
	PrincipalAgent PrincipalType = "AGENT"
)

type AgentRegistered struct {
	PublicKey     string        `json:"public_key"`
	PrincipalType PrincipalType `json:"principal_type"`
	DisplayName   string        `json:"display_name,omitempty"`
}
type AgentActivated struct {
	Role         Role     `json:"role"`
	Capabilities []string `json:"capabilities"`
	Scopes       []string `json:"scopes"`
}
type AgentKeyRotated struct {
	PublicKey           string `json:"public_key"`
	PreviousFingerprint string `json:"previous_fingerprint"`
}
type TaskCreated struct {
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	Repository  string   `json:"repository"`
	Branch      string   `json:"branch"`
	Worktree    string   `json:"worktree,omitempty"`
	Resources   []string `json:"resources"`
	ExternalRef string   `json:"external_ref,omitempty"`
	Risk        string   `json:"risk,omitempty"`
}
type TaskOffered struct {
	To        string    `json:"to"`
	ExpiresAt time.Time `json:"expires_at"`
}
type TaskClaimed struct {
	LeaseUntil time.Time `json:"lease_until"`
	OfferID    string    `json:"offer_id,omitempty"`
}
type TaskRenewed struct {
	LeaseUntil time.Time `json:"lease_until"`
	Progress   string    `json:"progress"`
}
type TaskHandoff struct {
	To      string `json:"to"`
	Summary string `json:"summary"`
}
type TaskStatus struct {
	Summary  string   `json:"summary,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	Reviewer string   `json:"reviewer,omitempty"`
}
type RecipientState struct {
	Principal string     `json:"principal"`
	Status    string     `json:"status"`
	At        *time.Time `json:"at,omitempty"`
}
type MessagePosted struct {
	Kind    string   `json:"kind"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	TaskID  string   `json:"task_id,omitempty"`
}
type MessageResponse struct {
	Response string `json:"response"`
	Note     string `json:"note,omitempty"`
}
type ApprovalRequested struct {
	Tier      string     `json:"tier"`
	Action    string     `json:"action"`
	Reason    string     `json:"reason"`
	Affected  []string   `json:"affected,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
type ApprovalResponse struct {
	Note string `json:"note,omitempty"`
}
type DecisionPayload struct {
	Title      string   `json:"title"`
	Statement  string   `json:"statement"`
	Supersedes string   `json:"supersedes,omitempty"`
	To         []string `json:"to,omitempty"`
}
type SessionPayload struct {
	AgentID string `json:"agent_id"`
	Host    string `json:"host,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Summary string `json:"summary,omitempty"`
}
type ArtifactAdded struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Storage   string `json:"storage"`
}
type ArchiveRun struct {
	Before  time.Time `json:"before"`
	TaskIDs []string  `json:"task_ids"`
}
type DocumentPayload struct {
	Title         string   `json:"title,omitempty"`
	Body          string   `json:"body,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ReplacementID string   `json:"replacement_id,omitempty"`
}

var payloadFactories = map[string]func() any{
	"agent.register": func() any { return &AgentRegistered{} }, "agent.activate": func() any { return &AgentActivated{} }, "agent.suspend": func() any { return &TaskStatus{} }, "agent.rotate-key": func() any { return &AgentKeyRotated{} },
	"task.create": func() any { return &TaskCreated{} }, "task.offer": func() any { return &TaskOffered{} }, "task.claim": func() any { return &TaskClaimed{} }, "task.start": func() any { return &TaskStatus{} }, "task.renew": func() any { return &TaskRenewed{} }, "task.block": func() any { return &TaskStatus{} }, "task.review": func() any { return &TaskStatus{} }, "task.complete": func() any { return &TaskStatus{} }, "task.cancel": func() any { return &TaskStatus{} }, "task.handoff": func() any { return &TaskHandoff{} }, "task.handoff.accept": func() any { return &TaskStatus{} }, "task.takeover": func() any { return &TaskStatus{} },
	"message.post": func() any { return &MessagePosted{} }, "message.ack": func() any { return &MessageResponse{} }, "message.reject": func() any { return &MessageResponse{} }, "message.complete": func() any { return &MessageResponse{} }, "message.resolve": func() any { return &MessageResponse{} },
	"approval.request": func() any { return &ApprovalRequested{} }, "approval.approve": func() any { return &ApprovalResponse{} }, "approval.reject": func() any { return &ApprovalResponse{} },
	"decision.create": func() any { return &DecisionPayload{} }, "decision.supersede": func() any { return &DecisionPayload{} }, "session.start": func() any { return &SessionPayload{} }, "session.end": func() any { return &SessionPayload{} },
	"artifact.add": func() any { return &ArtifactAdded{} }, "archive.run": func() any { return &ArchiveRun{} },
	"document.create": func() any { return &DocumentPayload{} }, "document.update": func() any { return &DocumentPayload{} }, "document.supersede": func() any { return &DocumentPayload{} },
}

func EncodePayload(typ string, value any) (json.RawMessage, error) {
	if _, ok := payloadFactories[typ]; !ok {
		return nil, fmt.Errorf("unsupported event type %q", typ)
	}
	b, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	var probe map[string]any
	if e = json.Unmarshal(b, &probe); e != nil {
		return nil, e
	}
	return b, nil
}
func DecodePayload(typ string, raw json.RawMessage) (any, error) {
	f, ok := payloadFactories[typ]
	if !ok {
		return nil, fmt.Errorf("unsupported event type %q", typ)
	}
	v := f()
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if e := json.Unmarshal(raw, v); e != nil {
		return nil, e
	}
	return v, nil
}
func KnownEventType(typ string) bool { _, ok := payloadFactories[typ]; return ok }
