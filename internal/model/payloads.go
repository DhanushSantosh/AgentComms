package model

import (
	"encoding/json"
	"fmt"
	"time"
)

// Role is OWNER, ORCHESTRATOR, or any other freeform label a principal
// chooses for itself (e.g. "Frontend-Architect", "Tester") -- see RFC 0018.
// Only OWNER and ORCHESTRATOR carry any permission effect; everything else
// is purely descriptive, like DisplayName, with no bearing on standing.
type Role string

const (
	RoleOwner        Role = "OWNER"
	RoleOrchestrator Role = "ORCHESTRATOR"
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

// AgentRoleSwitched is the self-service counterpart to AgentActivated --
// see RFC 0018. Unlike AgentActivated, it never carries Capabilities or
// Scopes: a principal relabeling its own role cannot use this to grant
// itself new standing, only a new descriptive label (or, with the
// existing owner-grant-plus-approval gate intact, ORCHESTRATOR).
type AgentRoleSwitched struct {
	Role Role `json:"role"`
}
type AgentKeyRotated struct {
	PublicKey           string `json:"public_key"`
	PreviousFingerprint string `json:"previous_fingerprint"`
}
type AgentElevatedKeyRegistered struct {
	PublicKey string `json:"public_key"`
}
type AgentRenamed struct {
	DisplayName string `json:"display_name"`
}
type AgentDeleted struct {
	Reason string `json:"reason"`
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
	Worktree   string    `json:"worktree,omitempty"`
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
type InvocationRequested struct {
	Target             string       `json:"target"`
	MessageID          string       `json:"message_id,omitempty"`
	TaskID             string       `json:"task_id,omitempty"`
	Instruction        string       `json:"instruction"`
	ExpectedResult     string       `json:"expected_result,omitempty"`
	Scopes             []string     `json:"scopes,omitempty"`
	Priority           string       `json:"priority,omitempty"`
	ConsumerMode       ConsumerMode `json:"consumer_mode,omitempty"`
	PreferredRuntimeID string       `json:"preferred_runtime_id,omitempty"`
	Deadline           *time.Time   `json:"deadline,omitempty"`
}
type InvocationDeliveryAttempted struct {
	DeliveryID   string    `json:"delivery_id"`
	RuntimeID    string    `json:"runtime_id"`
	Transport    string    `json:"transport"`
	HostID       string    `json:"host_id,omitempty"`
	EndpointID   string    `json:"endpoint_id,omitempty"`
	Attempt      int       `json:"attempt,omitempty"`
	Manual       bool      `json:"manual"`
	AttemptUntil time.Time `json:"attempt_until,omitempty"`
}
type InvocationNotified struct {
	DeliveryID string             `json:"delivery_id"`
	RuntimeID  string             `json:"runtime_id,omitempty"`
	Attempt    int                `json:"attempt,omitempty"`
	Transport  string             `json:"transport,omitempty"`
	EndpointID string             `json:"endpoint_id,omitempty"`
	Evidence   []DeliveryEvidence `json:"evidence,omitempty"`
}
type InvocationClaimed struct {
	RuntimeID  string    `json:"runtime_id"`
	ClaimUntil time.Time `json:"claim_until"`
}
type InvocationProgress struct {
	Summary string `json:"summary,omitempty"`
}
type InvocationWaiting struct {
	Reason        string     `json:"reason"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}
type InvocationCompleted struct {
	ResultMessageID string `json:"result_message_id,omitempty"`
	Summary         string `json:"summary"`
}
type InvocationRejected struct {
	Reason string `json:"reason"`
}
type InvocationDeliveryFailed struct {
	DeliveryID string     `json:"delivery_id"`
	RuntimeID  string     `json:"runtime_id,omitempty"`
	Attempt    int        `json:"attempt"`
	Error      string     `json:"error"`
	NextRetry  *time.Time `json:"next_retry,omitempty"`
	Final      bool       `json:"final"`
}
type RuntimeRegistered struct {
	AgentID         string      `json:"agent_id"`
	Kind            RuntimeKind `json:"kind,omitempty"`
	Connector       string      `json:"connector"`
	ConfigReference string      `json:"config_reference,omitempty"`
	HostID          string      `json:"host_id,omitempty"`
	MaxConcurrent   int         `json:"max_concurrent"`
	Scopes          []string    `json:"scopes,omitempty"`
	Capabilities    []string    `json:"capabilities,omitempty"`
}
type RuntimeConfigured struct {
	Kind            RuntimeKind `json:"kind"`
	Connector       string      `json:"connector"`
	ConfigReference string      `json:"config_reference,omitempty"`
	HostID          string      `json:"host_id,omitempty"`
	MaxConcurrent   int         `json:"max_concurrent"`
	Scopes          []string    `json:"scopes,omitempty"`
	Capabilities    []string    `json:"capabilities,omitempty"`
}
type RuntimeHeartbeat struct {
	Health            string   `json:"health"`
	EndpointID        string   `json:"endpoint_id,omitempty"`
	ActiveInvocations []string `json:"active_invocations,omitempty"`
}
type RuntimeStatusChanged struct {
	Reason     string `json:"reason,omitempty"`
	EndpointID string `json:"endpoint_id,omitempty"`
}
type InvocationPolicyUpdated struct {
	Mode                          string         `json:"mode"`
	TrustedActors                 []string       `json:"trusted_actors,omitempty"`
	AllowedScopes                 []string       `json:"allowed_scopes,omitempty"`
	DefaultConsumerMode           ConsumerMode   `json:"default_consumer_mode,omitempty"`
	AllowedConsumerModes          []ConsumerMode `json:"allowed_consumer_modes,omitempty"`
	PreferredInteractiveRuntimeID string         `json:"preferred_interactive_runtime_id,omitempty"`
	RequireHumanForSensitive      bool           `json:"require_human_for_sensitive"`
}
type ProjectSettingsUpdated struct {
	DefaultLease       string `json:"default_lease"`
	StaleGrace         string `json:"stale_grace"`
	ActiveRetention    string `json:"active_retention"`
	SummaryLimit       int    `json:"summary_limit"`
	ArtifactLimitBytes int64  `json:"artifact_limit_bytes"`
	RequireReview      bool   `json:"require_review"`
}
type ApprovalRequested struct {
	Tier          string     `json:"tier"`
	Action        string     `json:"action"`
	SubjectDigest string     `json:"subject_digest,omitempty"`
	Subject       string     `json:"subject,omitempty"`
	Reason        string     `json:"reason"`
	Affected      []string   `json:"affected,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
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
type EnvSetPayload struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type EnvDeletePayload struct {
	Key string `json:"key"`
}
type DocumentPayload struct {
	Title         string   `json:"title,omitempty"`
	Body          string   `json:"body,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ReplacementID string   `json:"replacement_id,omitempty"`
}

var payloadFactories = map[string]func() any{
	"agent.register": func() any { return &AgentRegistered{} }, "agent.activate": func() any { return &AgentActivated{} }, "agent.switch-role": func() any { return &AgentRoleSwitched{} }, "agent.suspend": func() any { return &TaskStatus{} }, "agent.rotate-key": func() any { return &AgentKeyRotated{} }, "agent.rename": func() any { return &AgentRenamed{} }, "agent.revoke": func() any { return &RuntimeStatusChanged{} }, "agent.delete": func() any { return &AgentDeleted{} }, "agent.elevate-key": func() any { return &AgentElevatedKeyRegistered{} },
	"task.create": func() any { return &TaskCreated{} }, "task.offer": func() any { return &TaskOffered{} }, "task.claim": func() any { return &TaskClaimed{} }, "task.start": func() any { return &TaskStatus{} }, "task.renew": func() any { return &TaskRenewed{} }, "task.block": func() any { return &TaskStatus{} }, "task.review": func() any { return &TaskStatus{} }, "task.complete": func() any { return &TaskStatus{} }, "task.cancel": func() any { return &TaskStatus{} }, "task.handoff": func() any { return &TaskHandoff{} }, "task.handoff.accept": func() any { return &TaskStatus{} }, "task.takeover": func() any { return &TaskStatus{} },
	"message.post": func() any { return &MessagePosted{} }, "message.ack": func() any { return &MessageResponse{} }, "message.reject": func() any { return &MessageResponse{} }, "message.complete": func() any { return &MessageResponse{} }, "message.resolve": func() any { return &MessageResponse{} },
	"invocation.request": func() any { return &InvocationRequested{} }, "invocation.delivery-attempt": func() any { return &InvocationDeliveryAttempted{} }, "invocation.notify": func() any { return &InvocationNotified{} }, "invocation.claim": func() any { return &InvocationClaimed{} }, "invocation.start": func() any { return &InvocationProgress{} }, "invocation.wait": func() any { return &InvocationWaiting{} }, "invocation.resume": func() any { return &InvocationProgress{} }, "invocation.complete": func() any { return &InvocationCompleted{} }, "invocation.reject": func() any { return &InvocationRejected{} }, "invocation.expire": func() any { return &InvocationRejected{} }, "invocation.cancel": func() any { return &InvocationRejected{} }, "invocation.delivery-failed": func() any { return &InvocationDeliveryFailed{} },
	"runtime.register": func() any { return &RuntimeRegistered{} }, "runtime.configure": func() any { return &RuntimeConfigured{} }, "runtime.heartbeat": func() any { return &RuntimeHeartbeat{} }, "runtime.offline": func() any { return &RuntimeStatusChanged{} }, "runtime.drain": func() any { return &RuntimeStatusChanged{} }, "runtime.resume": func() any { return &RuntimeStatusChanged{} }, "runtime.revoke": func() any { return &RuntimeStatusChanged{} }, "runtime.delete": func() any { return &RuntimeStatusChanged{} }, "invocation.policy.update": func() any { return &InvocationPolicyUpdated{} },
	"project.settings.update": func() any { return &ProjectSettingsUpdated{} },
	"approval.request":        func() any { return &ApprovalRequested{} }, "approval.approve": func() any { return &ApprovalResponse{} }, "approval.reject": func() any { return &ApprovalResponse{} },
	"decision.create": func() any { return &DecisionPayload{} }, "decision.supersede": func() any { return &DecisionPayload{} },
	"artifact.add": func() any { return &ArtifactAdded{} }, "archive.run": func() any { return &ArchiveRun{} },
	"document.create": func() any { return &DocumentPayload{} }, "document.update": func() any { return &DocumentPayload{} }, "document.supersede": func() any { return &DocumentPayload{} },
	"env.set": func() any { return &EnvSetPayload{} }, "env.delete": func() any { return &EnvDeletePayload{} },
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

// RegisteredEventTypes returns every event type payloadFactories knows how
// to encode/decode. Exported so tests outside this package can cross-check
// their own per-type registries (e.g. internal/projection/apply.go's
// ApplyEvent switch, internal/authority/postgres.go's own decodePayload
// switch) against this one, authoritative list -- catching a type that's
// missing from one of those the moment it's added here, rather than only
// when someone happens to exercise it against that specific backend. Order
// is unspecified.
func RegisteredEventTypes() []string {
	types := make([]string, 0, len(payloadFactories))
	for typ := range payloadFactories {
		types = append(types, typ)
	}
	return types
}
