package personalauthority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=FULL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS projects (
    project_id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    head_sequence INTEGER NOT NULL DEFAULT 0,
    head_hash TEXT NOT NULL DEFAULT '',
    state_json BLOB NOT NULL,
    frozen INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    project_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    intent_hash TEXT NOT NULL,
    event_json BLOB NOT NULL,
    receipt_json BLOB NOT NULL,
    actor_signature TEXT NOT NULL,
    legacy_json BLOB,
    PRIMARY KEY (project_id, sequence),
    UNIQUE (project_id, event_id),
    UNIQUE (project_id, idempotency_key),
    FOREIGN KEY (project_id) REFERENCES projects(project_id)
);
`

type Engine struct {
	db     *sql.DB
	signer *controlplane.Signer
	now    func() time.Time
}

func Open(path string, signer *controlplane.Signer) (*Engine, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("personal authority database path is required")
	}
	if signer == nil {
		return nil, errors.New("personal authority signer is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize personal authority: %w", err)
	}
	if err = ensureColumn(db, "projects", "frozen", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate personal authority schema: %w", err)
	}
	for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
		if chmodErr := os.Chmod(databaseFile, 0o600); chmodErr != nil && !os.IsNotExist(chmodErr) {
			_ = db.Close()
			return nil, fmt.Errorf("secure personal authority database: %w", chmodErr)
		}
	}
	return &Engine{db: db, signer: signer, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (e *Engine) Close() error {
	_, _ = e.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return e.db.Close()
}

func (e *Engine) Healthy(ctx context.Context) error {
	return e.db.PingContext(ctx)
}

func (e *Engine) CreateProject(ctx context.Context, projectID, ownerID string) error {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(ownerID) == "" {
		return controlError(controlplane.CodeValidation, "project and owner are required")
	}
	stateJSON, err := json.Marshal(emptyState())
	if err != nil {
		return err
	}
	_, err = e.db.ExecContext(ctx, `INSERT INTO projects
		(project_id,owner_id,state_json,updated_at) VALUES (?,?,?,?)
		ON CONFLICT(project_id) DO NOTHING`,
		projectID, ownerID, stateJSON, e.now().Format(time.RFC3339Nano))
	return err
}

func (e *Engine) Mutate(ctx context.Context, command controlplane.Command) (controlplane.Event, controlplane.Receipt, error) {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner, previousHash string
	var sequence uint64
	var frozen bool
	var stateJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT owner_id,head_sequence,head_hash,state_json,frozen
		FROM projects WHERE project_id=?`, command.ProjectID).
		Scan(&owner, &sequence, &previousHash, &stateJSON, &frozen); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, "project not found")
		}
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	if frozen {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(
			controlplane.CodeUnavailable, "personal authority is frozen for service migration",
		)
	}
	intentHash, err := command.IntentHash()
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}
	if event, receipt, found, replayErr := replay(ctx, tx, command.ProjectID, command.IdempotencyKey, intentHash); replayErr != nil {
		return event, receipt, replayErr
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
	var state model.State
	if err = json.Unmarshal(stateJSON, &state); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	publicKey, err := commandPublicKey(state, command)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if !command.Verify(publicKey) {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeIntegrity, "actor command signature is invalid")
	}
	if command.ExpectedSequence != 0 && command.ExpectedSequence != sequence {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(
			controlplane.CodeStalePrecondition,
			fmt.Sprintf("expected project sequence %d, current sequence is %d", command.ExpectedSequence, sequence),
		)
	}
	payload, err := decodePayload(command.Type, command.Payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}
	if command.Type == "agent.register" {
		registration := payload.(model.AgentRegistered)
		if registration.PublicKey != command.PublicKey {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeIntegrity, "registration payload and command public keys differ")
		}
	}
	if command.Type == "agent.activate" && sequence == 1 && command.Actor == owner && command.EntityID == owner {
		activation, valid := payload.(model.AgentActivated)
		if !valid || activation.Role != model.RoleOwner {
			return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeAuthorization, "initial owner activation must assign OWNER")
		}
	} else {
		payload, err = service.ValidateTransition(state, command.Actor, command.Type, command.EntityID, payload, now)
		if err != nil {
			return controlplane.Event{}, controlplane.Receipt{}, classify(err)
		}
	}
	normalizedPayload, err := model.EncodePayload(command.Type, payload)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, controlError(controlplane.CodeValidation, err.Error())
	}
	event := controlplane.Event{
		ProjectID: command.ProjectID, Sequence: sequence + 1,
		ID: fmt.Sprintf("evt-%020d", sequence+1), Time: now, Actor: command.Actor,
		Type: command.Type, EntityID: command.EntityID, Payload: normalizedPayload,
		PreviousHash: previousHash, ActorIntentHash: intentHash, IdempotencyKey: command.IdempotencyKey,
	}
	event.Hash, err = controlplane.HashEvent(event)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	receipt := controlplane.Receipt{
		ProjectID: event.ProjectID, Sequence: event.Sequence, EventID: event.ID,
		EventHash: event.Hash, ActorIntentHash: intentHash, CommittedAt: now,
	}
	if err = e.signer.SignReceipt(&receipt); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if err = service.ApplyEvent(&state, model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
	}); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	stateJSON, err = json.Marshal(state)
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events
		(project_id,sequence,event_id,idempotency_key,intent_hash,event_json,receipt_json,actor_signature)
		VALUES (?,?,?,?,?,?,?,?)`,
		event.ProjectID, event.Sequence, event.ID, event.IdempotencyKey, intentHash,
		eventJSON, receiptJSON, command.Signature); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET
		head_sequence=?,head_hash=?,state_json=?,updated_at=? WHERE project_id=?`,
		event.Sequence, event.Hash, stateJSON, now.Format(time.RFC3339Nano), event.ProjectID); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, unavailable(err)
	}
	return event, receipt, nil
}

func (e *Engine) FreezeProject(ctx context.Context, projectID string) error {
	result, err := e.db.ExecContext(ctx, `UPDATE projects SET frozen=1,updated_at=? WHERE project_id=?`,
		e.now().Format(time.RFC3339Nano), projectID)
	if err != nil {
		return unavailable(err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return unavailable(err)
	}
	if affected != 1 {
		return controlError(controlplane.CodeValidation, "project not found")
	}
	return nil
}

func (e *Engine) ImportLegacy(ctx context.Context, projectID string, events []model.Event) (*controlplane.Receipt, error) {
	if len(events) == 0 {
		return nil, controlError(controlplane.CodeValidation, "legacy runtime has no events")
	}
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, unavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var sequence uint64
	var head string
	var stateJSON []byte
	if err = tx.QueryRowContext(ctx, `SELECT head_sequence,head_hash,state_json FROM projects WHERE project_id=?`,
		projectID).Scan(&sequence, &head, &stateJSON); err != nil {
		return nil, unavailable(err)
	}
	if sequence != 0 {
		return nil, controlError(controlplane.CodeConflict, "personal authority project is not empty")
	}
	var state model.State
	if err = json.Unmarshal(stateJSON, &state); err != nil {
		return nil, unavailable(err)
	}
	var finalReceipt *controlplane.Receipt
	for index, legacyEvent := range events {
		expected := uint64(index + 1)
		if legacyEvent.Sequence != expected {
			return nil, controlError(controlplane.CodeIntegrity, fmt.Sprintf("legacy sequence gap at %d", expected))
		}
		if err = service.ApplyEvent(&state, legacyEvent); err != nil {
			return nil, controlError(controlplane.CodeIntegrity, fmt.Sprintf("apply legacy event %s: %s", legacyEvent.ID, err))
		}
		legacyJSON, marshalErr := json.Marshal(legacyEvent)
		if marshalErr != nil {
			return nil, marshalErr
		}
		event := controlplane.Event{
			ProjectID: projectID, Sequence: expected, ID: legacyEvent.ID,
			Time: legacyEvent.Time, Actor: legacyEvent.Actor, Type: legacyEvent.Type,
			EntityID: legacyEvent.EntityID, Payload: legacyEvent.Data, PreviousHash: head,
			ActorIntentHash: legacyEvent.Hash, IdempotencyKey: "legacy:" + legacyEvent.ID, Legacy: true,
		}
		event.Hash, err = controlplane.HashEvent(event)
		if err != nil {
			return nil, err
		}
		receipt := controlplane.Receipt{
			ProjectID: projectID, Sequence: expected, EventID: event.ID,
			EventHash: event.Hash, ActorIntentHash: event.ActorIntentHash,
			CommittedAt: e.now().Truncate(time.Microsecond),
		}
		if err = e.signer.SignReceipt(&receipt); err != nil {
			return nil, err
		}
		eventJSON, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return nil, marshalErr
		}
		receiptJSON, marshalErr := json.Marshal(receipt)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO events
			(project_id,sequence,event_id,idempotency_key,intent_hash,event_json,receipt_json,actor_signature,legacy_json)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			projectID, expected, event.ID, event.IdempotencyKey, event.ActorIntentHash,
			eventJSON, receiptJSON, legacyEvent.Signature, legacyJSON); err != nil {
			return nil, unavailable(err)
		}
		head = event.Hash
		finalReceipt = &receipt
	}
	stateJSON, err = json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET head_sequence=?,head_hash=?,state_json=?,updated_at=?
		WHERE project_id=?`, len(events), head, stateJSON, e.now().Format(time.RFC3339Nano), projectID); err != nil {
		return nil, unavailable(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, unavailable(err)
	}
	return finalReceipt, nil
}

func (e *Engine) Command(ctx context.Context, command controlplane.Command) (controlplane.Event, controlplane.Receipt, error) {
	return e.Mutate(ctx, command)
}

func (e *Engine) State(ctx context.Context, projectID string) (model.State, controlplane.ResultMetadata, error) {
	var stateJSON []byte
	var sequence uint64
	var head string
	if err := e.db.QueryRowContext(ctx, `SELECT state_json,head_sequence,head_hash FROM projects WHERE project_id=?`,
		projectID).Scan(&stateJSON, &sequence, &head); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.State{}, controlplane.ResultMetadata{}, controlError(controlplane.CodeValidation, "project not found")
		}
		return model.State{}, controlplane.ResultMetadata{}, unavailable(err)
	}
	var state model.State
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return state, controlplane.ResultMetadata{}, unavailable(err)
	}
	state.Integrity = model.Integrity{
		Verified: true, EventCount: int(sequence), Head: head, SyncState: "personal-authoritative",
	}
	service.RefreshRuntimePresence(&state, e.now())
	return state, controlplane.ResultMetadata{
		Consistency: "PERSONAL_AUTHORITATIVE", ServerSequence: sequence,
		CacheSequence: sequence, Connectivity: "LOCAL",
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
	rows, err := e.db.QueryContext(ctx, `SELECT event_json,receipt_json FROM events
		WHERE project_id=? AND sequence>? ORDER BY sequence LIMIT ?`, projectID, after, limit+1)
	if err != nil {
		return controlplane.EventPage{}, unavailable(err)
	}
	defer rows.Close()
	items := make([]controlplane.EventRecord, 0, limit+1)
	for rows.Next() {
		var eventJSON, receiptJSON []byte
		if err = rows.Scan(&eventJSON, &receiptJSON); err != nil {
			return controlplane.EventPage{}, unavailable(err)
		}
		var record controlplane.EventRecord
		if err = json.Unmarshal(eventJSON, &record.Event); err != nil {
			return controlplane.EventPage{}, unavailable(err)
		}
		if err = json.Unmarshal(receiptJSON, &record.Receipt); err != nil {
			return controlplane.EventPage{}, unavailable(err)
		}
		items = append(items, record)
	}
	if err = rows.Err(); err != nil {
		return controlplane.EventPage{}, unavailable(err)
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = controlplane.EncodeCursor(items[len(items)-1].Event.Sequence)
	}
	last := after
	if len(items) > 0 {
		last = items[len(items)-1].Event.Sequence
	}
	return controlplane.EventPage{
		Items: items, NextCursor: nextCursor,
		Metadata: controlplane.ResultMetadata{
			Consistency: "PERSONAL_AUTHORITATIVE", ServerSequence: last,
			CacheSequence: last, Connectivity: "LOCAL",
		},
	}, nil
}

func replay(ctx context.Context, tx *sql.Tx, projectID, idempotencyKey, intentHash string) (controlplane.Event, controlplane.Receipt, bool, error) {
	var eventJSON, receiptJSON []byte
	var storedIntent string
	err := tx.QueryRowContext(ctx, `SELECT intent_hash,event_json,receipt_json FROM events
		WHERE project_id=? AND idempotency_key=?`, projectID, idempotencyKey).
		Scan(&storedIntent, &eventJSON, &receiptJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return controlplane.Event{}, controlplane.Receipt{}, false, nil
	}
	if err != nil {
		return controlplane.Event{}, controlplane.Receipt{}, false, unavailable(err)
	}
	if storedIntent != intentHash {
		return controlplane.Event{}, controlplane.Receipt{}, false,
			controlError(controlplane.CodeConflict, "idempotency key was already used for a different command")
	}
	var event controlplane.Event
	var receipt controlplane.Receipt
	if err = json.Unmarshal(eventJSON, &event); err != nil {
		return event, receipt, false, unavailable(err)
	}
	if err = json.Unmarshal(receiptJSON, &receipt); err != nil {
		return event, receipt, false, unavailable(err)
	}
	return event, receipt, true, nil
}

func commandPublicKey(state model.State, command controlplane.Command) (string, error) {
	if command.Type == "agent.register" {
		if command.Actor != command.EntityID || command.PublicKey == "" {
			return "", controlError(controlplane.CodeAuthorization, "registration must be self-signed with a public key")
		}
		return command.PublicKey, nil
	}
	agent, found := state.Agents[command.Actor]
	if !found {
		return "", controlError(controlplane.CodeAuthorization, "actor is not registered")
	}
	return agent.PublicKey, nil
}

func decodePayload(eventType string, raw json.RawMessage) (any, error) {
	decoded, err := model.DecodePayload(eventType, raw)
	if err != nil {
		return nil, err
	}
	value := reflect.ValueOf(decoded)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return decoded, nil
	}
	return value.Elem().Interface(), nil
}

func emptyState() model.State {
	return model.State{
		Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{},
		Messages: map[string]model.Message{}, Invocations: map[string]model.Invocation{},
		InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes:        map[string]model.AgentRuntime{},
		InvocationPolicies:   map[string]model.InvocationPolicy{},
		Approvals:            map[string]model.Approval{}, Decisions: map[string]model.Decision{},
		Documents: map[string]model.Document{}, Env: map[string]model.EnvEntry{},
		Sessions: map[string]model.SessionPayload{}, Artifacts: map[string]model.Artifact{},
	}
}

func controlError(code controlplane.ErrorCode, message string) error {
	return &controlplane.Error{Code: code, Message: message}
}

func unavailable(err error) error {
	var controlErr *controlplane.Error
	if errors.As(err, &controlErr) {
		return err
	}
	return controlError(controlplane.CodeUnavailable, err.Error())
}

func classify(err error) error {
	message := err.Error()
	lower := strings.ToLower(message)
	if strings.Contains(lower, "required") &&
		(strings.Contains(lower, "role") || strings.Contains(lower, "principal") ||
			strings.Contains(lower, "owner") || strings.Contains(lower, "scope")) {
		return controlError(controlplane.CodeAuthorization, message)
	}
	if strings.Contains(lower, "overlap") || strings.Contains(lower, "already leased") ||
		strings.Contains(lower, "already claimed") {
		return controlError(controlplane.CodeConflict, message)
	}
	return controlError(controlplane.CodeValidation, message)
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}
