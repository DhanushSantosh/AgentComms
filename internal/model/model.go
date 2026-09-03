package model

import (
	"encoding/json"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const SchemaVersion = "2.2.0"

type RuntimeKind string

const (
	RuntimeKindWorker      RuntimeKind = "WORKER"
	RuntimeKindInteractive RuntimeKind = "INTERACTIVE"
)

type ConsumerMode string

const (
	ConsumerModeInteractiveOnly ConsumerMode = "INTERACTIVE_ONLY"
	ConsumerModeWorkerOnly      ConsumerMode = "WORKER_ONLY"
	ConsumerModeEither          ConsumerMode = "EITHER"
)

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
	// ElevatedPublicKey, when set, must sign the security-sensitive
	// transitions classified by internal/protocol.RequiresElevatedKey
	// instead of PublicKey. Registered self-service via agent.elevate-key;
	// empty means no elevated key exists yet and those transitions still
	// verify against PublicKey.
	ElevatedPublicKey      string `json:"elevated_public_key,omitempty"`
	ElevatedKeyFingerprint string `json:"elevated_key_fingerprint,omitempty"`
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
	ID                 string       `json:"id"`
	RequestedBy        string       `json:"requested_by"`
	Target             string       `json:"target"`
	MessageID          string       `json:"message_id,omitempty"`
	TaskID             string       `json:"task_id,omitempty"`
	Instruction        string       `json:"instruction"`
	ExpectedResult     string       `json:"expected_result,omitempty"`
	Scopes             []string     `json:"scopes,omitempty"`
	Priority           string       `json:"priority"`
	ConsumerMode       ConsumerMode `json:"consumer_mode"`
	PreferredRuntimeID string       `json:"preferred_runtime_id,omitempty"`
	Status             string       `json:"status"`
	CreatedAt          time.Time    `json:"created_at"`
	Deadline           *time.Time   `json:"deadline,omitempty"`
	ClaimedBy          string       `json:"claimed_by,omitempty"`
	ClaimedAt          *time.Time   `json:"claimed_at,omitempty"`
	RuntimeID          string       `json:"runtime_id,omitempty"`
	ClaimUntil         *time.Time   `json:"claim_until,omitempty"`
	StartedAt          *time.Time   `json:"started_at,omitempty"`
	CompletedAt        *time.Time   `json:"completed_at,omitempty"`
	NextAttemptAt      *time.Time   `json:"next_attempt_at,omitempty"`
	ResultMessageID    string       `json:"result_message_id,omitempty"`
	Summary            string       `json:"summary,omitempty"`
	Reason             string       `json:"reason,omitempty"`
}
type DeliveryEvidence struct {
	Stage string    `json:"stage"`
	At    time.Time `json:"at"`
}
type InvocationDelivery struct {
	ID           string             `json:"id"`
	InvocationID string             `json:"invocation_id"`
	RuntimeID    string             `json:"runtime_id,omitempty"`
	Transport    string             `json:"transport,omitempty"`
	HostID       string             `json:"host_id,omitempty"`
	EndpointID   string             `json:"endpoint_id,omitempty"`
	Attempt      int                `json:"attempt"`
	Manual       bool               `json:"manual"`
	Status       string             `json:"status"`
	AttemptedAt  *time.Time         `json:"attempted_at,omitempty"`
	AttemptUntil *time.Time         `json:"attempt_until,omitempty"`
	NotifiedAt   *time.Time         `json:"notified_at,omitempty"`
	FailedAt     *time.Time         `json:"failed_at,omitempty"`
	NextRetryAt  *time.Time         `json:"next_retry_at,omitempty"`
	Evidence     []DeliveryEvidence `json:"evidence,omitempty"`
	Error        string             `json:"error,omitempty"`
}
type AgentRuntime struct {
	ID                 string                   `json:"id"`
	AgentID            string                   `json:"agent_id"`
	Kind               RuntimeKind              `json:"kind"`
	Connector          string                   `json:"connector"`
	ConfigReference    string                   `json:"config_reference,omitempty"`
	HostID             string                   `json:"host_id,omitempty"`
	EndpointID         string                   `json:"endpoint_id,omitempty"`
	Status             string                   `json:"status"`
	Health             string                   `json:"health"`
	MaxConcurrent      int                      `json:"max_concurrent"`
	ActiveInvocations  []string                 `json:"active_invocations,omitempty"`
	Scopes             []string                 `json:"scopes,omitempty"`
	Capabilities       []string                 `json:"capabilities,omitempty"`
	RegisteredAt       time.Time                `json:"registered_at"`
	LastSeenAt         time.Time                `json:"last_seen_at,omitempty"`
	LastChangedBy      string                   `json:"last_changed_by"`
	Reason             string                   `json:"reason,omitempty"`
	InteractiveSession *InteractiveSessionState `json:"interactive_session,omitempty"`
}
type InteractiveSessionState struct {
	Local      bool   `json:"local"`
	Alive      bool   `json:"alive"`
	Busy       bool   `json:"busy"`
	SocketPath string `json:"socket_path,omitempty"`
}
type InvocationPolicy struct {
	AgentID                       string         `json:"agent_id"`
	Mode                          string         `json:"mode"`
	TrustedActors                 []string       `json:"trusted_actors,omitempty"`
	AllowedScopes                 []string       `json:"allowed_scopes,omitempty"`
	DefaultConsumerMode           ConsumerMode   `json:"default_consumer_mode"`
	AllowedConsumerModes          []ConsumerMode `json:"allowed_consumer_modes,omitempty"`
	PreferredInteractiveRuntimeID string         `json:"preferred_interactive_runtime_id,omitempty"`
	RequireHumanForSensitive      bool           `json:"require_human_for_sensitive"`
	UpdatedBy                     string         `json:"updated_by"`
	UpdatedAt                     time.Time      `json:"updated_at"`
}
type Approval struct {
	ID            string     `json:"id"`
	Tier          string     `json:"tier"`
	Action        string     `json:"action"`
	SubjectDigest string     `json:"subject_digest,omitempty"`
	Subject       string     `json:"subject,omitempty"`
	Reason        string     `json:"reason"`
	Status        string     `json:"status"`
	Requester     string     `json:"requester"`
	Affected      []string   `json:"affected,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Approver      string     `json:"approver,omitempty"`
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
type ProjectSettings struct {
	DefaultLease       string    `json:"default_lease"`
	StaleGrace         string    `json:"stale_grace"`
	ActiveRetention    string    `json:"active_retention"`
	SummaryLimit       int       `json:"summary_limit"`
	ArtifactLimitBytes int64     `json:"artifact_limit_bytes"`
	RequireReview      bool      `json:"require_review"`
	UpdatedBy          string    `json:"updated_by,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

func DefaultProjectSettings() ProjectSettings {
	return ProjectSettings{
		DefaultLease: "4h", StaleGrace: "1h", ActiveRetention: "168h",
		SummaryLimit: 1200, ArtifactLimitBytes: 5 * 1024 * 1024,
	}
}

func EffectiveProjectSettings(settings ProjectSettings) ProjectSettings {
	defaults := DefaultProjectSettings()
	if settings.DefaultLease == "" {
		settings.DefaultLease = defaults.DefaultLease
	}
	if settings.StaleGrace == "" {
		settings.StaleGrace = defaults.StaleGrace
	}
	if settings.ActiveRetention == "" {
		settings.ActiveRetention = defaults.ActiveRetention
	}
	if settings.SummaryLimit == 0 {
		settings.SummaryLimit = defaults.SummaryLimit
	}
	if settings.ArtifactLimitBytes == 0 {
		settings.ArtifactLimitBytes = defaults.ArtifactLimitBytes
	}
	return settings
}

type State struct {
	Agents               map[string]Agent              `json:"agents"`
	Tasks                map[string]Task               `json:"tasks"`
	Messages             map[string]Message            `json:"messages"`
	Invocations          map[string]Invocation         `json:"invocations"`
	InvocationDeliveries map[string]InvocationDelivery `json:"invocation_deliveries"`
	AgentRuntimes        map[string]AgentRuntime       `json:"agent_runtimes"`
	InvocationPolicies   map[string]InvocationPolicy   `json:"invocation_policies"`
	Approvals            map[string]Approval           `json:"approvals"`
	Documents            map[string]Document           `json:"documents"`
	Env                  map[string]EnvEntry           `json:"env"`
	Artifacts            map[string]Artifact           `json:"artifacts"`
	ProjectSettings      ProjectSettings               `json:"project_settings"`
	Integrity            Integrity                     `json:"integrity"`
}
