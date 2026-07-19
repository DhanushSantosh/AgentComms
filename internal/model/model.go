package model

import (
	"encoding/json"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const SchemaVersion = "2.0.0"

type Event struct {
	SchemaVersion  string                `json:"schema_version"`
	PayloadVersion int                   `json:"payload_version"`
	ID             string                `json:"id"`
	Sequence       uint64                `json:"sequence"`
	Time           time.Time             `json:"time"`
	Actor          string                `json:"actor"`
	Type           string                `json:"type"`
	EntityID       string                `json:"entity_id,omitempty"`
	Data           json.RawMessage       `json:"data,omitempty"`
	PreviousHash   string                `json:"previous_hash,omitempty"`
	Hash           string                `json:"hash"`
	Signature      string                `json:"signature"`
	KeyFingerprint string                `json:"key_fingerprint"`
	ServerReceipt  *controlplane.Receipt `json:"server_receipt,omitempty"`
	Consistency    string                `json:"consistency,omitempty"`
	Connectivity   string                `json:"connectivity,omitempty"`
}
type Agent struct {
	ID             string        `json:"id"`
	DisplayName    string        `json:"display_name"`
	Status         string        `json:"status"`
	Role           Role          `json:"role"`
	PrincipalType  PrincipalType `json:"principal_type"`
	PublicKey      string        `json:"public_key"`
	KeyFingerprint string        `json:"key_fingerprint"`
	Capabilities   []string      `json:"capabilities"`
	Scopes         []string      `json:"scopes"`
}
type Offer struct {
	ID        string    `json:"id"`
	To        string    `json:"to"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
}
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary,omitempty"`
	Status      string     `json:"status"`
	Owner       string     `json:"owner,omitempty"`
	Repository  string     `json:"repository"`
	Branch      string     `json:"branch"`
	Worktree    string     `json:"worktree,omitempty"`
	Resources   []string   `json:"resources"`
	ExternalRef string     `json:"external_ref,omitempty"`
	Risk        string     `json:"risk"`
	LeaseUntil  time.Time  `json:"lease_until,omitempty"`
	StaleUntil  time.Time  `json:"stale_until,omitempty"`
	HandoffTo   string     `json:"handoff_to,omitempty"`
	Offers      []Offer    `json:"offers,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Archived    bool       `json:"archived"`
}
type Message struct {
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	From       string           `json:"from"`
	To         []string         `json:"to"`
	Subject    string           `json:"subject"`
	Body       string           `json:"body"`
	TaskID     string           `json:"task_id,omitempty"`
	Status     string           `json:"status"`
	Recipients []RecipientState `json:"recipients"`
}
type Invocation struct {
	ID              string     `json:"id"`
	RequestedBy     string     `json:"requested_by"`
	Target          string     `json:"target"`
	MessageID       string     `json:"message_id,omitempty"`
	TaskID          string     `json:"task_id,omitempty"`
	Instruction     string     `json:"instruction"`
	ExpectedResult  string     `json:"expected_result,omitempty"`
	Priority        string     `json:"priority"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	Deadline        *time.Time `json:"deadline,omitempty"`
	ClaimedBy       string     `json:"claimed_by,omitempty"`
	RuntimeID       string     `json:"runtime_id,omitempty"`
	ClaimUntil      *time.Time `json:"claim_until,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	NextAttemptAt   *time.Time `json:"next_attempt_at,omitempty"`
	ResultMessageID string     `json:"result_message_id,omitempty"`
	Summary         string     `json:"summary,omitempty"`
	Reason          string     `json:"reason,omitempty"`
}
type InvocationDelivery struct {
	ID           string     `json:"id"`
	InvocationID string     `json:"invocation_id"`
	RuntimeID    string     `json:"runtime_id,omitempty"`
	Attempt      int        `json:"attempt"`
	Status       string     `json:"status"`
	NotifiedAt   *time.Time `json:"notified_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
	NextRetryAt  *time.Time `json:"next_retry_at,omitempty"`
	Error        string     `json:"error,omitempty"`
}
type Approval struct {
	ID        string   `json:"id"`
	Tier      string   `json:"tier"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	Status    string   `json:"status"`
	Requester string   `json:"requester"`
	Affected  []string `json:"affected,omitempty"`
	Approver  string   `json:"approver,omitempty"`
}
type Decision struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Statement  string   `json:"statement"`
	Supersedes string   `json:"supersedes,omitempty"`
	Status     string   `json:"status"`
	To         []string `json:"to,omitempty"`
}
type Artifact struct {
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Storage   string `json:"storage"`
}
type Document struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Tags       []string `json:"tags,omitempty"`
	Status     string   `json:"status"`
	Version    int      `json:"version"`
	Author     string   `json:"author"`
	Supersedes string   `json:"supersedes,omitempty"`
}
type EnvEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by"`
}
type Integrity struct {
	Verified       bool   `json:"verified"`
	EventCount     int    `json:"event_count"`
	Head           string `json:"head,omitempty"`
	UnknownEvents  int    `json:"unknown_events"`
	Remote         string `json:"remote,omitempty"`
	SyncState      string `json:"sync_state"`
	Consistency    string `json:"consistency,omitempty"`
	ServerSequence uint64 `json:"server_sequence,omitempty"`
	CacheSequence  uint64 `json:"cache_sequence,omitempty"`
	Connectivity   string `json:"connectivity,omitempty"`
}
type State struct {
	Agents               map[string]Agent              `json:"agents"`
	Tasks                map[string]Task               `json:"tasks"`
	Messages             map[string]Message            `json:"messages"`
	Invocations          map[string]Invocation         `json:"invocations"`
	InvocationDeliveries map[string]InvocationDelivery `json:"invocation_deliveries"`
	Approvals            map[string]Approval           `json:"approvals"`
	Decisions            map[string]Decision           `json:"decisions"`
	Documents            map[string]Document           `json:"documents"`
	Env                  map[string]EnvEntry           `json:"env"`
	Sessions             map[string]SessionPayload     `json:"sessions"`
	Artifacts            map[string]Artifact           `json:"artifacts"`
	Integrity            Integrity                     `json:"integrity"`
}
