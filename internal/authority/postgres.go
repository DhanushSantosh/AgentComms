package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	defaultMaxConnections = 32
	defaultMinConnections = 4
	defaultIdleTimeout    = 5 * time.Minute
)

type Config struct {
	DatabaseURL      string
	MaxConnections   int
	MinConnections   int
	StatementTimeout time.Duration
	Production       bool
}

type Engine struct {
	db     *sql.DB
	signer *controlplane.Signer
	now    func() time.Time
}

func Open(ctx context.Context, cfg Config, signer *controlplane.Signer) (*Engine, error) {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("database URL is required")
	}
	if signer == nil {
		return nil, errors.New("service signer is required")
	}
	if cfg.Production && strings.TrimSpace(signer.PrivateKey()) == "" {
		return nil, errors.New("production mode requires an explicit service signing key")
	}
	connectionConfig, err := pgx.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	statementTimeout := cfg.StatementTimeout
	if statementTimeout <= 0 {
		statementTimeout = controlplane.DefaultRequestTimeout
	}
	connectionConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", statementTimeout.Milliseconds())
	db := stdlib.OpenDB(*connectionConfig)
	maxConnections := cfg.MaxConnections
	if maxConnections <= 0 {
		maxConnections = defaultMaxConnections
	}
	minConnections := cfg.MinConnections
	if minConnections <= 0 {
		minConnections = defaultMinConnections
	}
	if minConnections > maxConnections {
		_ = db.Close()
		return nil, errors.New("minimum connections cannot exceed maximum connections")
	}
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(minConnections)
	db.SetConnMaxIdleTime(defaultIdleTimeout)
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	if err = ApplySchema(ctx, db, false); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply authority schema: %w", err)
	}
	return &Engine{db: db, signer: signer, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (e *Engine) Close() error { return e.db.Close() }

func (e *Engine) Healthy(ctx context.Context) error { return e.db.PingContext(ctx) }

func (e *Engine) State(ctx context.Context, projectID string) (model.State, controlplane.ResultMetadata, error) {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return model.State{}, controlplane.ResultMetadata{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var sequence uint64
	var head string
	if err = tx.QueryRowContext(ctx, `SELECT head_sequence,head_hash FROM projects WHERE project_id=$1`,
		projectID).Scan(&sequence, &head); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.State{}, controlplane.ResultMetadata{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "project not found"}
		}
		return model.State{}, controlplane.ResultMetadata{}, unavailable(err)
	}
	state, err := loadState(ctx, tx, projectID)
	if err != nil {
		return state, controlplane.ResultMetadata{}, err
	}
	state.Integrity = model.Integrity{
		Verified: true, EventCount: int(sequence), Head: head,
		SyncState: "authoritative",
	}
	protocol.RefreshRuntimePresence(&state, e.now())
	if err = tx.Commit(); err != nil {
		return model.State{}, controlplane.ResultMetadata{}, unavailable(err)
	}
	return state, controlplane.ResultMetadata{
		Consistency: "AUTHORITATIVE", ServerSequence: sequence, Connectivity: "ONLINE",
	}, nil
}

func (e *Engine) Events(ctx context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	limit, err := page.BoundedLimit()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	after, err := controlplane.DecodeCursor(page.Cursor)
	if err != nil {
		return controlplane.EventPage{}, err
	}
	rows, err := e.db.QueryContext(ctx, `SELECT project_id,sequence,event_id,event_time,actor_id,actor_key_fingerprint,event_type,
		entity_id,payload,previous_hash,event_hash,actor_intent_hash,idempotency_key,
		receipt FROM events WHERE project_id=$1 AND sequence>$2 ORDER BY sequence LIMIT $3`,
		projectID, after, limit+1)
	if err != nil {
		return controlplane.EventPage{}, unavailable(err)
	}
	defer rows.Close()
	items := make([]controlplane.EventRecord, 0, limit)
	for rows.Next() {
		var event controlplane.Event
		var receiptJSON []byte
		if err = rows.Scan(&event.ProjectID, &event.Sequence, &event.ID, &event.Time, &event.Actor, &event.ActorKeyFingerprint,
			&event.Type, &event.EntityID, &event.Payload, &event.PreviousHash, &event.Hash,
			&event.ActorIntentHash, &event.IdempotencyKey, &receiptJSON); err != nil {
			return controlplane.EventPage{}, unavailable(err)
		}
		var receipt controlplane.Receipt
		if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
			return controlplane.EventPage{}, unavailable(err)
		}
		items = append(items, controlplane.EventRecord{Event: event, Receipt: receipt})
	}
	if err = rows.Err(); err != nil {
		return controlplane.EventPage{}, unavailable(err)
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = controlplane.EncodeCursor(items[len(items)-1].Event.Sequence)
	}
	serverSequence := after
	if len(items) > 0 {
		serverSequence = items[len(items)-1].Event.Sequence
	}
	return controlplane.EventPage{
		Items: items, NextCursor: nextCursor,
		Metadata: controlplane.ResultMetadata{
			Consistency: "AUTHORITATIVE", ServerSequence: serverSequence, Connectivity: "ONLINE",
		},
	}, nil
}

func (e *Engine) VerifyRange(ctx context.Context, projectID string, from, to uint64) error {
	if from == 0 {
		from = 1
	}
	if to != 0 && to < from {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid verification range"}
	}
	query := `SELECT sequence,event_id,event_time,actor_id,actor_key_fingerprint,event_type,entity_id,payload,previous_hash,
		event_hash,actor_intent_hash,idempotency_key,receipt
		FROM events WHERE project_id=$1 AND sequence >= $2`
	args := []any{projectID, from}
	if to != 0 {
		query += ` AND sequence <= $3`
		args = append(args, to)
	}
	query += ` ORDER BY sequence`
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return unavailable(err)
	}
	defer rows.Close()
	var expectedSequence = from
	var previousHash string
	if from > 1 {
		if err = e.db.QueryRowContext(ctx, `SELECT event_hash FROM events WHERE project_id=$1 AND sequence=$2`,
			projectID, from-1).Scan(&previousHash); err != nil {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "verification range predecessor is missing"}
		}
	}
	for rows.Next() {
		var event controlplane.Event
		var receiptJSON []byte
		event.ProjectID = projectID
		if err = rows.Scan(&event.Sequence, &event.ID, &event.Time, &event.Actor, &event.ActorKeyFingerprint, &event.Type,
			&event.EntityID, &event.Payload, &event.PreviousHash, &event.Hash,
			&event.ActorIntentHash, &event.IdempotencyKey, &receiptJSON); err != nil {
			return unavailable(err)
		}
		if event.Sequence != expectedSequence || event.PreviousHash != previousHash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("chain discontinuity at %s", event.ID)}
		}
		if !controlplane.VerifyEventHash(event) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("event hash mismatch at %s", event.ID)}
		}
		var receipt controlplane.Receipt
		if err = json.Unmarshal(receiptJSON, &receipt); err != nil || !controlplane.VerifyReceipt(receipt, e.signer.PublicKey()) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("receipt signature mismatch at %s", event.ID)}
		}
		previousHash = event.Hash
		expectedSequence++
	}
	return rows.Err()
}

func (e *Engine) CreateProject(ctx context.Context, projectID, ownerID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(ownerID) == "" {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "project and owner are required"}
	}
	_, err := e.db.ExecContext(ctx,
		`INSERT INTO projects (project_id, owner_id) VALUES ($1, $2)
		 ON CONFLICT (project_id) DO NOTHING`, projectID, ownerID)
	return err
}

// DeleteProject permanently removes command.ProjectID and every row scoped
// to it -- see RFC 0020. Deliberately not routed through Mutate: that
// pipeline's entire contract is appending to and projecting a *surviving*
// event log, and a deletion event would need to be inserted and then
// immediately cascade-deleted by the very row it authorized. Authorization
// is verified fully here, server-side, independent of whatever the caller
// already checked client-side: actor must be the project's OWNER (from
// live state, not the client's own claim) and must have a registered
// elevated key, and command must carry that exact key's signature.
// Idempotent: deleting an already-deleted or never-existing project_id
// returns a plain CodeValidation "not found" rather than erroring
// ambiguously, so a client retrying after a dropped response is always
// safe.
func (e *Engine) DeleteProject(ctx context.Context, command controlplane.Command) error {
	now := e.now().Truncate(time.Microsecond)
	if err := command.Validate(now); err != nil {
		return err
	}
	if command.Type != "project.delete" {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "command type must be project.delete"}
	}
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()

	var ownerID string
	if err = tx.QueryRowContext(ctx, `SELECT owner_id FROM projects WHERE project_id = $1 FOR UPDATE`,
		command.ProjectID).Scan(&ownerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &controlplane.Error{Code: controlplane.CodeValidation, Message: "project not found or already deleted"}
		}
		return unavailable(err)
	}

	var stateJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT state FROM agents WHERE project_id=$1 AND agent_id=$2`,
		command.ProjectID, command.Actor).Scan(&stateJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &controlplane.Error{Code: controlplane.CodeAuthorization, Message: "actor is not registered"}
		}
		return unavailable(err)
	}
	var agent model.Agent
	if err = json.Unmarshal(stateJSON, &agent); err != nil {
		return err
	}
	if agent.Role != model.RoleOwner {
		return &controlplane.Error{Code: controlplane.CodeAuthorization, Message: "only the project owner can delete a project"}
	}
	if agent.ElevatedPublicKey == "" {
		return &controlplane.Error{Code: controlplane.CodeAuthorization,
			Message: "the owner must register an elevated key (agent elevate-key) before a project can be deleted"}
	}
	if !command.Verify(agent.ElevatedPublicKey) {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "actor command signature is invalid"}
	}
	actorKeyFingerprint := identity.Fingerprint(agent.ElevatedPublicKey)
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO deleted_projects (project_id, owner_id, deleted_by, actor_key_fingerprint) VALUES ($1,$2,$3,$4)`,
		command.ProjectID, ownerID, command.Actor, actorKeyFingerprint,
	); err != nil {
		return unavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM projects WHERE project_id = $1`, command.ProjectID); err != nil {
		return unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return unavailable(err)
	}
	return nil
}

func (e *Engine) Mutate(ctx context.Context, command controlplane.Command) (controlplane.Event, controlplane.Receipt, error) {
	tx, err := e.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner, previousHash string
	var headSequence uint64
	if err = tx.QueryRowContext(ctx,
		`SELECT owner_id, head_sequence, head_hash FROM projects WHERE project_id = $1 FOR UPDATE`,
		command.ProjectID).Scan(&owner, &headSequence, &previousHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: "project not found"}
		}
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}

	intentHash, err := command.IntentHash()
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: err.Error()}
	}
	if event, receipt, found, lookupErr := replay(ctx, tx, command.ProjectID, command.IdempotencyKey, intentHash); lookupErr != nil {
		return controlplane.Event{}, controlplane.Receipt{}, lookupErr
	} else if found {
		if err = tx.Commit(); err != nil {
			return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
		}
		return event, receipt, nil
	}

	now := e.now().Truncate(time.Microsecond)
	if err = command.Validate(now); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	// Payload is decoded here, ahead of the full loadState below, so
	// commandPublicKey can classify the transitions governed by
	// protocol.RequiresElevatedKey without
	// paying loadState's cost on every signature check -- it uses its own
	// small, targeted queries instead (kept cheap deliberately: signature
	// verification runs before an attacker's invalid-signature command gets
	// to touch the full project state).
	payload, err := model.DecodePayloadValue(command.Type, command.Payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: err.Error()}
	}
	publicKey, err := commandPublicKey(ctx, tx, command, payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if !command.Verify(publicKey) {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "actor command signature is invalid"}
	}
	actorKeyFingerprint := identity.Fingerprint(publicKey)
	if command.ExpectedSequence != 0 && command.ExpectedSequence != headSequence {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{
			Code:    controlplane.CodeStalePrecondition,
			Message: fmt.Sprintf("expected project sequence %d, current sequence is %d", command.ExpectedSequence, headSequence),
		}
	}

	state, err := loadState(ctx, tx, command.ProjectID)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if command.Type == "agent.register" {
		registration := payload.(model.AgentRegistered)
		if registration.PublicKey != command.PublicKey {
			return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{
				Code: controlplane.CodeIntegrity, Message: "registration payload and command public keys differ",
			}
		}
	}
	if command.Type == "agent.activate" && headSequence == 1 && command.Actor == owner && command.EntityID == owner {
		activation, ok := payload.(model.AgentActivated)
		if !ok || activation.Role != model.RoleOwner {
			return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeAuthorization, Message: "initial owner activation must assign OWNER"}
		}
	} else {
		payload, err = protocol.ValidateTransition(state, command.Actor, command.Type, command.EntityID, payload, now)
		if err != nil {
			return controlplane.Event{}, controlplane.Receipt{}, controlplane.ClassifyValidationError(err)
		}
	}
	normalizedPayload, err := model.EncodePayload(command.Type, payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: err.Error()}
	}

	sequence := headSequence + 1
	event := controlplane.Event{
		ProjectID: command.ProjectID, Sequence: sequence,
		ID: fmt.Sprintf("evt-%020d", sequence), Time: now, Actor: command.Actor,
		ActorKeyFingerprint: actorKeyFingerprint,
		Type:                command.Type, EntityID: command.EntityID, Payload: normalizedPayload,
		PreviousHash: previousHash, ActorIntentHash: intentHash, IdempotencyKey: command.IdempotencyKey,
	}
	event.Hash, err = controlplane.HashEvent(event)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	receipt := controlplane.Receipt{
		ProjectID: command.ProjectID, Sequence: sequence, EventID: event.ID,
		EventHash: event.Hash, ActorIntentHash: intentHash, CommittedAt: now,
	}
	if err = e.signer.SignReceipt(&receipt); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}

	next := cloneState(state)
	if err = projection.ApplyEvent(&next, model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
	}); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if err = persistProjectionChanges(ctx, tx, command.ProjectID, sequence, state, next); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	eventJSON, _ := json.Marshal(event)
	receiptJSON, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO events
		(project_id, sequence, event_id, event_time, actor_id, actor_key_fingerprint, event_type, entity_id,
		 payload, previous_hash, event_hash, actor_intent_hash, actor_signature,
		 idempotency_key, receipt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		event.ProjectID, event.Sequence, event.ID, event.Time, event.Actor, event.ActorKeyFingerprint, event.Type,
		event.EntityID, event.Payload, event.PreviousHash, event.Hash, intentHash,
		command.Signature, command.IdempotencyKey, receiptJSON); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, conflictOrUnavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET head_sequence=$2, head_hash=$3, updated_at=$4 WHERE project_id=$1`,
		event.ProjectID, event.Sequence, event.Hash, now); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox (project_id, sequence, event) VALUES ($1,$2,$3)`,
		event.ProjectID, event.Sequence, eventJSON); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, conflictOrUnavailable(err)
	}
	return event, receipt, nil
}

func replay(ctx context.Context, tx *sql.Tx, projectID, key, intentHash string) (controlplane.Event, controlplane.Receipt, bool, error) {
	var event controlplane.Event
	var receiptJSON []byte
	var storedIntent string
	err := tx.QueryRowContext(ctx, `SELECT project_id,sequence,event_id,event_time,actor_id,actor_key_fingerprint,event_type,
		entity_id,payload,previous_hash,event_hash,actor_intent_hash,idempotency_key,receipt
		FROM events WHERE project_id=$1 AND idempotency_key=$2`, projectID, key).
		Scan(&event.ProjectID, &event.Sequence, &event.ID, &event.Time, &event.Actor, &event.ActorKeyFingerprint, &event.Type,
			&event.EntityID, &event.Payload, &event.PreviousHash, &event.Hash, &storedIntent,
			&event.IdempotencyKey, &receiptJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return controlplane.Event{}, controlplane.Receipt{}, false, nil
	}
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, false, unavailable(err)
	}
	if storedIntent != intentHash {
		return controlplane.Event{}, controlplane.Receipt{}, false,
			&controlplane.Error{Code: controlplane.CodeConflict, Message: "idempotency key was already used for a different command"}
	}
	var receipt controlplane.Receipt
	event.ActorIntentHash = storedIntent
	if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
		return event, receipt, false, err
	}
	return event, receipt, true, nil
}

func commandPublicKey(ctx context.Context, tx *sql.Tx, command controlplane.Command, payload any) (string, error) {
	if command.Type == "agent.register" {
		if command.Actor != command.EntityID || command.PublicKey == "" {
			return "", &controlplane.Error{Code: controlplane.CodeAuthorization, Message: "registration must be self-signed with a public key"}
		}
		return command.PublicKey, nil
	}
	var stateJSON []byte
	err := tx.QueryRowContext(ctx, `SELECT state FROM agents WHERE project_id=$1 AND agent_id=$2`,
		command.ProjectID, command.Actor).Scan(&stateJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", &controlplane.Error{Code: controlplane.CodeAuthorization, Message: "actor is not registered"}
	}
	if err != nil {
		return "", unavailable(err)
	}
	var agent model.Agent
	if err = json.Unmarshal(stateJSON, &agent); err != nil {
		return "", err
	}
	if agent.ElevatedPublicKey == "" {
		return agent.PublicKey, nil
	}
	scopedState, err := scopedElevationState(ctx, tx, command)
	if err != nil {
		return "", err
	}
	if protocol.RequiresElevatedKey(scopedState, command.Actor, command.Type, command.EntityID, payload) {
		return agent.ElevatedPublicKey, nil
	}
	return agent.PublicKey, nil
}

// scopedElevationState builds the minimal model.State protocol.RequiresElevatedKey
// actually reads for each transition type it classifies -- st.Approvals[id]
// for approval.approve, st.Agents[id] (the TARGET being revoked or deleted,
// distinct from the actor's own row already fetched above) -- via a
// single targeted row lookup, instead of the full loadState. Keeps
// signature verification cheap regardless of which transition type it's
// classifying: an attacker spamming invalid-signature commands never gets
// to force a full project state load. If RequiresElevatedKey ever needs to
// read a different field for a new transition type, this function needs a
// matching case or the two will silently drift -- see the comment on
// RequiresElevatedKey itself.
func scopedElevationState(ctx context.Context, tx *sql.Tx, command controlplane.Command) (model.State, error) {
	switch command.Type {
	case "approval.approve":
		var stateJSON []byte
		err := tx.QueryRowContext(ctx, `SELECT state FROM approvals WHERE project_id=$1 AND approval_id=$2`,
			command.ProjectID, command.EntityID).Scan(&stateJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return model.State{}, nil
		}
		if err != nil {
			return model.State{}, unavailable(err)
		}
		var approval model.Approval
		if err = json.Unmarshal(stateJSON, &approval); err != nil {
			return model.State{}, err
		}
		return model.State{Approvals: map[string]model.Approval{command.EntityID: approval}}, nil
	case "agent.revoke", "agent.delete":
		var stateJSON []byte
		err := tx.QueryRowContext(ctx, `SELECT state FROM agents WHERE project_id=$1 AND agent_id=$2`,
			command.ProjectID, command.EntityID).Scan(&stateJSON)
		if errors.Is(err, sql.ErrNoRows) {
			return model.State{}, nil
		}
		if err != nil {
			return model.State{}, unavailable(err)
		}
		var target model.Agent
		if err = json.Unmarshal(stateJSON, &target); err != nil {
			return model.State{}, err
		}
		return model.State{Agents: map[string]model.Agent{command.EntityID: target}}, nil
	default:
		return model.State{}, nil
	}
}

func conflictOrUnavailable(err error) error {
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate key") || strings.Contains(lower, "serialization") ||
		strings.Contains(lower, "unique constraint") {
		return &controlplane.Error{Code: controlplane.CodeConflict, Message: "concurrent mutation conflicted; retry with the same idempotency key"}
	}
	return unavailable(err)
}

func unavailable(err error) error {
	return &controlplane.Error{Code: controlplane.CodeUnavailable, Message: err.Error()}
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
