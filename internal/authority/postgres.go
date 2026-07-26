package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
	if err = ApplySchema(ctx, db); err != nil {
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
	rows, err := e.db.QueryContext(ctx, `SELECT project_id,sequence,event_id,event_time,actor_id,event_type,
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
		if err = rows.Scan(&event.ProjectID, &event.Sequence, &event.ID, &event.Time, &event.Actor,
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
	query := `SELECT sequence,event_id,event_time,actor_id,event_type,entity_id,payload,previous_hash,
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
		if err = rows.Scan(&event.Sequence, &event.ID, &event.Time, &event.Actor, &event.Type,
			&event.EntityID, &event.Payload, &event.PreviousHash, &event.Hash,
			&event.ActorIntentHash, &event.IdempotencyKey, &receiptJSON); err != nil {
			return unavailable(err)
		}
		if event.Sequence != expectedSequence || event.PreviousHash != previousHash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("chain discontinuity at %s", event.ID)}
		}
		hash, hashErr := controlplane.HashEvent(event)
		if hashErr != nil || hash != event.Hash {
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
	publicKey, err := commandPublicKey(ctx, tx, command)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if !command.Verify(publicKey) {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "actor command signature is invalid"}
	}
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
	payload, err := decodePayload(command.Type, command.Payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, &controlplane.Error{Code: controlplane.CodeValidation, Message: err.Error()}
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
			return controlplane.Event{}, controlplane.Receipt{}, classify(err)
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
		Type: command.Type, EntityID: command.EntityID, Payload: normalizedPayload,
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
		(project_id, sequence, event_id, event_time, actor_id, event_type, entity_id,
		 payload, previous_hash, event_hash, actor_intent_hash, actor_signature,
		 idempotency_key, receipt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		event.ProjectID, event.Sequence, event.ID, event.Time, event.Actor, event.Type,
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
	err := tx.QueryRowContext(ctx, `SELECT project_id,sequence,event_id,event_time,actor_id,event_type,
		entity_id,payload,previous_hash,event_hash,actor_intent_hash,idempotency_key,receipt
		FROM events WHERE project_id=$1 AND idempotency_key=$2`, projectID, key).
		Scan(&event.ProjectID, &event.Sequence, &event.ID, &event.Time, &event.Actor, &event.Type,
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

func commandPublicKey(ctx context.Context, tx *sql.Tx, command controlplane.Command) (string, error) {
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
	return agent.PublicKey, nil
}

func decodePayload(eventType string, raw json.RawMessage) (any, error) {
	decoded, err := model.DecodePayload(eventType, raw)
	if err != nil {
		return nil, err
	}
	switch value := decoded.(type) {
	case *model.AgentRegistered:
		return *value, nil
	case *model.AgentActivated:
		return *value, nil
	case *model.AgentKeyRotated:
		return *value, nil
	case *model.TaskCreated:
		return *value, nil
	case *model.TaskOffered:
		return *value, nil
	case *model.TaskClaimed:
		return *value, nil
	case *model.TaskRenewed:
		return *value, nil
	case *model.TaskHandoff:
		return *value, nil
	case *model.TaskStatus:
		return *value, nil
	case *model.MessagePosted:
		return *value, nil
	case *model.MessageResponse:
		return *value, nil
	case *model.InvocationRequested:
		return *value, nil
	case *model.InvocationNotified:
		return *value, nil
	case *model.InvocationClaimed:
		return *value, nil
	case *model.InvocationProgress:
		return *value, nil
	case *model.InvocationWaiting:
		return *value, nil
	case *model.InvocationCompleted:
		return *value, nil
	case *model.InvocationRejected:
		return *value, nil
	case *model.InvocationDeliveryFailed:
		return *value, nil
	case *model.RuntimeRegistered:
		return *value, nil
	case *model.RuntimeHeartbeat:
		return *value, nil
	case *model.RuntimeStatusChanged:
		return *value, nil
	case *model.InvocationPolicyUpdated:
		return *value, nil
	case *model.ProjectSettingsUpdated:
		return *value, nil
	case *model.ApprovalRequested:
		return *value, nil
	case *model.ApprovalResponse:
		return *value, nil
	case *model.DecisionPayload:
		return *value, nil
	case *model.SessionPayload:
		return *value, nil
	case *model.ArtifactAdded:
		return *value, nil
	case *model.ArchiveRun:
		return *value, nil
	case *model.DocumentPayload:
		return *value, nil
	case *model.EnvSetPayload:
		return *value, nil
	case *model.EnvDeletePayload:
		return *value, nil
	default:
		return nil, fmt.Errorf("unsupported payload for %s", eventType)
	}
}

func classify(err error) error {
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "required") && (strings.Contains(lower, "role") || strings.Contains(lower, "principal") ||
		strings.Contains(lower, "owner") || strings.Contains(lower, "scope")) {
		return &controlplane.Error{Code: controlplane.CodeAuthorization, Message: message}
	}
	if strings.Contains(lower, "overlap") || strings.Contains(lower, "already leased") {
		return &controlplane.Error{Code: controlplane.CodeConflict, Message: message}
	}
	return &controlplane.Error{Code: controlplane.CodeValidation, Message: message}
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

func cloneState(state model.State) model.State {
	raw, _ := json.Marshal(state)
	var clone model.State
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func loadState(ctx context.Context, tx *sql.Tx, projectID string) (model.State, error) {
	state := model.State{
		Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{},
		Messages: map[string]model.Message{}, Approvals: map[string]model.Approval{},
		Invocations: map[string]model.Invocation{}, InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes: map[string]model.AgentRuntime{}, InvocationPolicies: map[string]model.InvocationPolicy{},
		Decisions: map[string]model.Decision{}, Documents: map[string]model.Document{},
		Env: map[string]model.EnvEntry{}, Sessions: map[string]model.SessionPayload{},
		Artifacts:       map[string]model.Artifact{},
		ProjectSettings: model.DefaultProjectSettings(),
	}
	loaders := []func() error{
		func() error {
			return loadProjection(ctx, tx, "agents", "agent_id", projectID, func(id string, raw []byte) error {
				var v model.Agent
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Agents[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "tasks", "task_id", projectID, func(id string, raw []byte) error {
				var v model.Task
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Tasks[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "messages", "message_id", projectID, func(id string, raw []byte) error {
				var v model.Message
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Messages[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocations", "invocation_id", projectID, func(id string, raw []byte) error {
				var value model.Invocation
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.Invocations[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocation_deliveries", "delivery_id", projectID, func(id string, raw []byte) error {
				var value model.InvocationDelivery
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.InvocationDeliveries[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "agent_runtimes", "runtime_id", projectID, func(id string, raw []byte) error {
				var value model.AgentRuntime
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.AgentRuntimes[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "invocation_policies", "agent_id", projectID, func(id string, raw []byte) error {
				var value model.InvocationPolicy
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
				state.InvocationPolicies[id] = value
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "approvals", "approval_id", projectID, func(id string, raw []byte) error {
				var v model.Approval
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Approvals[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "decisions", "decision_id", projectID, func(id string, raw []byte) error {
				var v model.Decision
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Decisions[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "documents", "document_id", projectID, func(id string, raw []byte) error {
				var v model.Document
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Documents[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "artifacts", "sha256", projectID, func(id string, raw []byte) error {
				var v model.Artifact
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Artifacts[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "environment_entries", "entry_key", projectID, func(id string, raw []byte) error {
				var v model.EnvEntry
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Env[id] = v
				return nil
			})
		},
		func() error {
			return loadProjection(ctx, tx, "sessions", "session_id", projectID, func(id string, raw []byte) error {
				var v model.SessionPayload
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				state.Sessions[id] = v
				return nil
			})
		},
	}
	for _, load := range loaders {
		if err := load(); err != nil {
			return state, unavailable(err)
		}
	}
	var settingsRaw []byte
	err := tx.QueryRowContext(ctx, "SELECT state FROM project_settings WHERE project_id=$1", projectID).Scan(&settingsRaw)
	if err == nil {
		if err = json.Unmarshal(settingsRaw, &state.ProjectSettings); err != nil {
			return state, unavailable(err)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, unavailable(err)
	}
	return state, nil
}

func loadProjection(ctx context.Context, tx *sql.Tx, table, idColumn, projectID string, accept func(string, []byte) error) error {
	allowed := map[string]bool{
		"agents.agent_id": true, "tasks.task_id": true, "messages.message_id": true,
		"invocations.invocation_id": true, "invocation_deliveries.delivery_id": true,
		"agent_runtimes.runtime_id": true, "invocation_policies.agent_id": true,
		"approvals.approval_id": true, "decisions.decision_id": true,
		"documents.document_id": true, "artifacts.sha256": true,
		"environment_entries.entry_key": true, "sessions.session_id": true,
	}
	if !allowed[table+"."+idColumn] {
		return errors.New("invalid projection query")
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT %s, state FROM %s WHERE project_id=$1", idColumn, table), projectID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return err
		}
		if err = accept(id, raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

func persistProjectionChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after model.State) error {
	if !reflect.DeepEqual(before.ProjectSettings, after.ProjectSettings) {
		raw, err := json.Marshal(after.ProjectSettings)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO project_settings (project_id,state,updated_sequence)
			VALUES ($1,$2,$3) ON CONFLICT (project_id) DO UPDATE SET
			state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`, projectID, raw, sequence); err != nil {
			return err
		}
	}
	for id, value := range after.Agents {
		if reflect.DeepEqual(before.Agents[id], value) {
			continue
		}
		if err := upsertAgent(ctx, tx, projectID, sequence, value); err != nil {
			return err
		}
	}
	for id, value := range after.Tasks {
		if reflect.DeepEqual(before.Tasks[id], value) {
			continue
		}
		if err := upsertTask(ctx, tx, projectID, sequence, value); err != nil {
			return err
		}
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "messages", "message_id", before.Messages, after.Messages); err != nil {
		return err
	}
	if err := persistInvocationChanges(ctx, tx, projectID, sequence, before.Invocations, after.Invocations); err != nil {
		return err
	}
	if err := persistInvocationDeliveryChanges(ctx, tx, projectID, sequence, before.InvocationDeliveries, after.InvocationDeliveries); err != nil {
		return err
	}
	if err := persistRuntimeChanges(ctx, tx, projectID, sequence, before.AgentRuntimes, after.AgentRuntimes); err != nil {
		return err
	}
	if err := persistInvocationPolicyChanges(ctx, tx, projectID, sequence, before.InvocationPolicies, after.InvocationPolicies); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "approvals", "approval_id", before.Approvals, after.Approvals); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "decisions", "decision_id", before.Decisions, after.Decisions); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "documents", "document_id", before.Documents, after.Documents); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "artifacts", "sha256", before.Artifacts, after.Artifacts); err != nil {
		return err
	}
	if err := persistSimpleChanges(ctx, tx, projectID, sequence, "environment_entries", "entry_key", before.Env, after.Env); err != nil {
		return err
	}
	return persistSessions(ctx, tx, projectID, sequence, before.Sessions, after.Sessions)
}

func persistRuntimeChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.AgentRuntime) error {
	for id, runtime := range after {
		if reflect.DeepEqual(before[id], runtime) {
			continue
		}
		raw, err := json.Marshal(runtime)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runtimes
			(project_id,runtime_id,agent_id,connector,status,health,last_seen_at,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (project_id,runtime_id) DO UPDATE SET agent_id=EXCLUDED.agent_id,
			connector=EXCLUDED.connector,status=EXCLUDED.status,health=EXCLUDED.health,
			last_seen_at=EXCLUDED.last_seen_at,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, runtime.AgentID, runtime.Connector, runtime.Status, runtime.Health,
			nullableTime(runtime.LastSeenAt), raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationPolicyChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.InvocationPolicy) error {
	for id, policy := range after {
		if reflect.DeepEqual(before[id], policy) {
			continue
		}
		raw, err := json.Marshal(policy)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocation_policies
			(project_id,agent_id,mode,state,updated_sequence) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (project_id,agent_id) DO UPDATE SET mode=EXCLUDED.mode,
			state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, policy.Mode, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.Invocation) error {
	for id, invocation := range after {
		if reflect.DeepEqual(before[id], invocation) {
			continue
		}
		raw, err := json.Marshal(invocation)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocations
			(project_id,invocation_id,target_id,requested_by,status,deadline,claim_until,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (project_id,invocation_id) DO UPDATE SET target_id=EXCLUDED.target_id,
			requested_by=EXCLUDED.requested_by,status=EXCLUDED.status,deadline=EXCLUDED.deadline,
			claim_until=EXCLUDED.claim_until,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, invocation.Target, invocation.RequestedBy, invocation.Status,
			invocation.Deadline, invocation.ClaimUntil, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistInvocationDeliveryChanges(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.InvocationDelivery) error {
	for id, delivery := range after {
		if reflect.DeepEqual(before[id], delivery) {
			continue
		}
		raw, err := json.Marshal(delivery)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO invocation_deliveries
			(project_id,delivery_id,invocation_id,runtime_id,attempt,status,next_retry_at,state,updated_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (project_id,delivery_id) DO UPDATE SET invocation_id=EXCLUDED.invocation_id,
			runtime_id=EXCLUDED.runtime_id,attempt=EXCLUDED.attempt,status=EXCLUDED.status,
			next_retry_at=EXCLUDED.next_retry_at,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, delivery.InvocationID, delivery.RuntimeID, delivery.Attempt,
			delivery.Status, delivery.NextRetryAt, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func upsertAgent(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, agent model.Agent) error {
	raw, _ := json.Marshal(agent)
	_, err := tx.ExecContext(ctx, `INSERT INTO agents (project_id,agent_id,state,updated_sequence) VALUES ($1,$2,$3,$4)
		ON CONFLICT (project_id,agent_id) DO UPDATE SET state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
		projectID, agent.ID, raw, sequence)
	if err != nil {
		return err
	}
	if agent.PublicKey != "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO actor_keys (project_id,actor_id,fingerprint,public_key,valid_from_sequence)
			VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
			projectID, agent.ID, identity.Fingerprint(agent.PublicKey), agent.PublicKey, sequence)
	}
	return err
}

func upsertTask(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, task model.Task) error {
	raw, _ := json.Marshal(task)
	_, err := tx.ExecContext(ctx, `INSERT INTO tasks
		(project_id,task_id,status,owner_id,worktree,lease_until,archived,state,updated_sequence)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (project_id,task_id) DO UPDATE SET status=EXCLUDED.status,owner_id=EXCLUDED.owner_id,
		worktree=EXCLUDED.worktree,lease_until=EXCLUDED.lease_until,archived=EXCLUDED.archived,
		state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
		projectID, task.ID, task.Status, task.Owner, task.Worktree, nullableTime(task.LeaseUntil), task.Archived, raw, sequence)
	if err != nil {
		return err
	}
	active := !task.Archived && task.Status != "COMPLETED" && task.Status != "CANCELLED"
	if _, err = tx.ExecContext(ctx, `UPDATE task_resources SET active=$3 WHERE project_id=$1 AND task_id=$2`,
		projectID, task.ID, active); err != nil {
		return err
	}
	for _, resource := range task.Resources {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_resources (project_id,task_id,resource,active)
			VALUES ($1,$2,$3,$4) ON CONFLICT (project_id,task_id,resource) DO UPDATE SET active=EXCLUDED.active`,
			projectID, task.ID, resource, active); err != nil {
			return err
		}
	}
	return nil
}

func persistSimpleChanges[V any](ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, table, idColumn string, before, after map[string]V) error {
	allowed := map[string]bool{
		"messages.message_id": true, "approvals.approval_id": true,
		"decisions.decision_id": true, "documents.document_id": true,
		"artifacts.sha256": true, "environment_entries.entry_key": true,
	}
	if !allowed[table+"."+idColumn] {
		return errors.New("invalid projection update")
	}
	for id, value := range after {
		if reflect.DeepEqual(before[id], value) {
			continue
		}
		raw, _ := json.Marshal(value)
		query := fmt.Sprintf(`INSERT INTO %s (project_id,%s,state,updated_sequence) VALUES ($1,$2,$3,$4)
			ON CONFLICT (project_id,%s) DO UPDATE SET state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			table, idColumn, idColumn)
		if table == "messages" {
			message := any(value).(model.Message)
			query = `INSERT INTO messages (project_id,message_id,kind,sender_id,status,task_id,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (project_id,message_id) DO UPDATE SET
				kind=EXCLUDED.kind,sender_id=EXCLUDED.sender_id,status=EXCLUDED.status,task_id=EXCLUDED.task_id,
				state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, message.Kind, message.From, message.Status, message.TaskID, raw, sequence); err != nil {
				return err
			}
			for _, recipient := range message.Recipients {
				if _, err := tx.ExecContext(ctx, `INSERT INTO message_recipients
					(project_id,message_id,principal_id,status,responded_at) VALUES ($1,$2,$3,$4,$5)
					ON CONFLICT (project_id,message_id,principal_id) DO UPDATE SET status=EXCLUDED.status,responded_at=EXCLUDED.responded_at`,
					projectID, id, recipient.Principal, recipient.Status, recipient.At); err != nil {
					return err
				}
			}
			continue
		}
		if table == "approvals" {
			approval := any(value).(model.Approval)
			query = `INSERT INTO approvals (project_id,approval_id,action,status,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (project_id,approval_id) DO UPDATE SET
				action=EXCLUDED.action,status=EXCLUDED.status,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, approval.Action, approval.Status, raw, sequence); err != nil {
				return err
			}
			continue
		}
		if table == "documents" {
			document := any(value).(model.Document)
			query = `INSERT INTO documents (project_id,document_id,status,state,updated_sequence)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT (project_id,document_id) DO UPDATE SET
				status=EXCLUDED.status,state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`
			if _, err := tx.ExecContext(ctx, query, projectID, id, document.Status, raw, sequence); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, query, projectID, id, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func persistSessions(ctx context.Context, tx *sql.Tx, projectID string, sequence uint64, before, after map[string]model.SessionPayload) error {
	for id := range before {
		if _, exists := after[id]; !exists {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE project_id=$1 AND session_id=$2`, projectID, id); err != nil {
				return err
			}
		}
	}
	for id, session := range after {
		if reflect.DeepEqual(before[id], session) {
			continue
		}
		raw, _ := json.Marshal(session)
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions (project_id,session_id,state,updated_sequence)
			VALUES ($1,$2,$3,$4) ON CONFLICT (project_id,session_id) DO UPDATE SET state=EXCLUDED.state,updated_sequence=EXCLUDED.updated_sequence`,
			projectID, id, raw, sequence); err != nil {
			return err
		}
	}
	return nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
