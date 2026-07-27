package controlplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	LocalDaemonProtocolVersion = 3
	MaxCommandBytes            = 256 * 1024
	MaxRecipients              = 100
	DefaultPageSize            = 100
	MaxPageSize                = 500
	MaxDraftBytes              = 5 * 1024 * 1024
	MaxDraftsPerProject        = 1_000
	MaxDraftStorageBytes       = 50 * 1024 * 1024
	MaxInvocationBytes         = 16 * 1024
	MaxInvocationListen        = 10 * time.Second
	MaxDeliveryAttempts        = 10
	MaxInvocationTTL           = 7 * 24 * time.Hour
	DefaultClaimLease          = 15 * time.Minute
	MaxClaimLease              = time.Hour
	MaxRuntimesPerProject      = 500
	MaxRuntimeConcurrency      = 100
	MinHeartbeatInterval       = 10 * time.Second
	RuntimeOfflineAfter        = 45 * time.Second
	CommandClockSkew           = 5 * time.Minute
	DefaultRequestTimeout      = 15 * time.Second
)

type ErrorCode string

const (
	CodeOffline           ErrorCode = "OFFLINE"
	CodeConflict          ErrorCode = "CONFLICT"
	CodeRateLimited       ErrorCode = "RATE_LIMITED"
	CodeStalePrecondition ErrorCode = "STALE_PRECONDITION"
	CodeUnavailable       ErrorCode = "UNAVAILABLE"
	CodeAuthorization     ErrorCode = "AUTHORIZATION"
	CodeValidation        ErrorCode = "VALIDATION"
	CodeIntegrity         ErrorCode = "INTEGRITY"
)

type Error struct {
	Code       ErrorCode
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string { return e.Message }

type Command struct {
	ProjectID        string          `json:"project_id"`
	Actor            string          `json:"actor"`
	Type             string          `json:"type"`
	EntityID         string          `json:"entity_id,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	IdempotencyKey   string          `json:"idempotency_key"`
	IssuedAt         time.Time       `json:"issued_at"`
	ExpectedSequence uint64          `json:"expected_sequence,omitempty"`
	PublicKey        string          `json:"public_key,omitempty"`
	Signature        string          `json:"signature"`
}

type commandCanonical struct {
	ProjectID        string `json:"project_id"`
	Actor            string `json:"actor"`
	Type             string `json:"type"`
	EntityID         string `json:"entity_id,omitempty"`
	PayloadHash      string `json:"payload_hash"`
	IdempotencyKey   string `json:"idempotency_key"`
	IssuedAt         string `json:"issued_at"`
	ExpectedSequence uint64 `json:"expected_sequence,omitempty"`
	PublicKey        string `json:"public_key,omitempty"`
}

type Event struct {
	ProjectID       string          `json:"project_id"`
	Sequence        uint64          `json:"sequence"`
	ID              string          `json:"id"`
	Time            time.Time       `json:"time"`
	Actor           string          `json:"actor"`
	Type            string          `json:"type"`
	EntityID        string          `json:"entity_id,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	PreviousHash    string          `json:"previous_hash,omitempty"`
	Hash            string          `json:"hash"`
	ActorIntentHash string          `json:"actor_intent_hash"`
	IdempotencyKey  string          `json:"idempotency_key"`
}

type eventCanonical struct {
	ProjectID       string `json:"project_id"`
	Sequence        uint64 `json:"sequence"`
	ID              string `json:"id"`
	Time            string `json:"time"`
	Actor           string `json:"actor"`
	Type            string `json:"type"`
	EntityID        string `json:"entity_id,omitempty"`
	PayloadHash     string `json:"payload_hash"`
	PreviousHash    string `json:"previous_hash,omitempty"`
	ActorIntentHash string `json:"actor_intent_hash"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type Receipt struct {
	ProjectID       string    `json:"project_id"`
	Sequence        uint64    `json:"sequence"`
	EventID         string    `json:"event_id"`
	EventHash       string    `json:"event_hash"`
	ActorIntentHash string    `json:"actor_intent_hash"`
	CommittedAt     time.Time `json:"committed_at"`
	KeyFingerprint  string    `json:"key_fingerprint"`
	Signature       string    `json:"signature"`
}

type ResultMetadata struct {
	Consistency    string   `json:"consistency"`
	ServerSequence uint64   `json:"server_sequence,omitempty"`
	Receipt        *Receipt `json:"receipt,omitempty"`
	CacheSequence  uint64   `json:"cache_sequence,omitempty"`
	Connectivity   string   `json:"connectivity"`
}

type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type EventPage struct {
	Items      []EventRecord  `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Metadata   ResultMetadata `json:"metadata"`
}

type EventRecord struct {
	Event   Event   `json:"event"`
	Receipt Receipt `json:"receipt"`
}

type Draft struct {
	ProjectID string          `json:"project_id"`
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (p PageRequest) BoundedLimit() (int, error) {
	if p.Limit < 0 || p.Limit > MaxPageSize {
		return 0, fmt.Errorf("limit must be between 0 and %d", MaxPageSize)
	}
	if p.Limit == 0 {
		return DefaultPageSize, nil
	}
	return p.Limit, nil
}

func (c Command) Validate(now time.Time) error {
	if strings.TrimSpace(c.ProjectID) == "" || strings.TrimSpace(c.Actor) == "" ||
		strings.TrimSpace(c.Type) == "" || strings.TrimSpace(c.IdempotencyKey) == "" {
		return &Error{Code: CodeValidation, Message: "project, actor, type, and idempotency key are required"}
	}
	if len(c.Payload) > MaxCommandBytes {
		return &Error{Code: CodeValidation, Message: fmt.Sprintf("command payload exceeds %d bytes", MaxCommandBytes)}
	}
	if c.IssuedAt.IsZero() || c.IssuedAt.Before(now.Add(-CommandClockSkew)) || c.IssuedAt.After(now.Add(CommandClockSkew)) {
		return &Error{Code: CodeValidation, Message: "command issuance time is outside the allowed clock skew"}
	}
	if _, err := c.IntentHash(); err != nil {
		return &Error{Code: CodeValidation, Message: err.Error()}
	}
	return nil
}

func (c Command) canonical() ([]byte, error) {
	var normalized any
	if len(c.Payload) == 0 {
		normalized = map[string]any{}
	} else if err := json.Unmarshal(c.Payload, &normalized); err != nil {
		return nil, fmt.Errorf("payload is not valid JSON: %w", err)
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	payloadHash := sha256.Sum256(payload)
	return json.Marshal(commandCanonical{
		ProjectID: c.ProjectID, Actor: c.Actor, Type: c.Type, EntityID: c.EntityID,
		PayloadHash: hex.EncodeToString(payloadHash[:]), IdempotencyKey: c.IdempotencyKey,
		IssuedAt:         c.IssuedAt.UTC().Format(time.RFC3339Nano),
		ExpectedSequence: c.ExpectedSequence, PublicKey: c.PublicKey,
	})
}

func (c Command) IntentHash() (string, error) {
	b, err := c.canonical()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (c *Command) Sign(privateKey string) error {
	raw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return errors.New("invalid Ed25519 private key")
	}
	hash, err := c.IntentHash()
	if err != nil {
		return err
	}
	c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(raw), []byte(hash)))
	return nil
}

func (c Command) Verify(publicKey string) bool {
	pub, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return false
	}
	hash, err := c.IntentHash()
	return err == nil && ed25519.Verify(ed25519.PublicKey(pub), []byte(hash), sig)
}

type Signer struct {
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}

func GenerateSigner() (*Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Signer{publicKey: pub, privateKey: priv}, nil
}

func NewSigner(privateKey string) (*Signer, error) {
	raw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid Ed25519 service private key")
	}
	priv := ed25519.PrivateKey(raw)
	return &Signer{publicKey: priv.Public().(ed25519.PublicKey), privateKey: priv}, nil
}

func (s *Signer) PrivateKey() string {
	return base64.StdEncoding.EncodeToString(s.privateKey)
}

func (s *Signer) PublicKey() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

func (s *Signer) Fingerprint() string {
	h := sha256.Sum256(s.publicKey)
	return hex.EncodeToString(h[:8])
}

func (s *Signer) SignReceipt(receipt *Receipt) error {
	receipt.KeyFingerprint = s.Fingerprint()
	canonical, err := receiptCanonical(*receipt)
	if err != nil {
		return err
	}
	receipt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.privateKey, canonical))
	return nil
}

func VerifyReceipt(receipt Receipt, publicKey string) bool {
	pub, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(receipt.Signature)
	if err != nil {
		return false
	}
	canonical, err := receiptCanonical(receipt)
	return err == nil && ed25519.Verify(ed25519.PublicKey(pub), canonical, sig)
}

func receiptCanonical(receipt Receipt) ([]byte, error) {
	receipt.Signature = ""
	return json.Marshal(receipt)
}

func HashEvent(event Event) (string, error) {
	var payload any
	if len(event.Payload) == 0 {
		payload = map[string]any{}
	} else if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return "", err
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	payloadHash := sha256.Sum256(normalized)
	b, err := json.Marshal(eventCanonical{
		ProjectID: event.ProjectID, Sequence: event.Sequence, ID: event.ID,
		Time: event.Time.UTC().Format(time.RFC3339Nano), Actor: event.Actor,
		Type: event.Type, EntityID: event.EntityID,
		PayloadHash: hex.EncodeToString(payloadHash[:]), PreviousHash: event.PreviousHash,
		ActorIntentHash: event.ActorIntentHash, IdempotencyKey: event.IdempotencyKey,
	})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// VerifyEventHash validates native events by recomputing their canonical hash.
// Events imported by the removed legacy migration path are distinguishable by
// their immutable idempotency-key namespace. Their original model envelope was
// verified before import but was not retained in the normalized authority
// record, so the service-signed receipt is the durable content-attestation
// boundary for those records. Structural validation here prevents malformed
// imported records from entering a rebuilt projection; callers must also
// verify the matching receipt and chain link.
func VerifyEventHash(event Event) bool {
	if strings.HasPrefix(event.IdempotencyKey, "legacy:") {
		if event.ProjectID == "" || event.Sequence == 0 || event.ID == "" ||
			event.Time.IsZero() || event.Actor == "" || event.Type == "" ||
			event.ActorIntentHash == "" || len(event.Payload) == 0 {
			return false
		}
		decoded, err := hex.DecodeString(event.Hash)
		return err == nil && len(decoded) == sha256.Size && json.Valid(event.Payload)
	}
	hash, err := HashEvent(event)
	return err == nil && hash == event.Hash
}

func EncodeCursor(sequence uint64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d", sequence)))
}

func DecodeCursor(cursor string) (uint64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, &Error{Code: CodeValidation, Message: "invalid pagination cursor"}
	}
	var sequence uint64
	if _, err = fmt.Sscanf(string(raw), "%d", &sequence); err != nil {
		return 0, &Error{Code: CodeValidation, Message: "invalid pagination cursor"}
	}
	return sequence, nil
}
