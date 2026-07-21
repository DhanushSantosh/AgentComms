package localcache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS projects (
    project_id TEXT PRIMARY KEY,
    server_sequence INTEGER NOT NULL DEFAULT 0,
    server_head TEXT NOT NULL DEFAULT '',
    state_json BLOB NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    project_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_json BLOB NOT NULL,
    receipt_json BLOB NOT NULL,
    PRIMARY KEY (project_id, sequence)
);

CREATE TABLE IF NOT EXISTS drafts (
    project_id TEXT NOT NULL,
    draft_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    body BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (project_id, draft_id)
);

CREATE INDEX IF NOT EXISTS drafts_project_idx ON drafts (project_id, updated_at DESC);
`

type Cache struct {
	db              *sql.DB
	serverPublicKey string
	now             func() time.Time
}

func Open(path, serverPublicKey string) (*Cache, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("cache path is required")
	}
	if strings.TrimSpace(serverPublicKey) == "" {
		return nil, errors.New("server public key is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize local cache: %w", err)
	}
	for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
		if chmodErr := os.Chmod(databaseFile, 0o600); chmodErr != nil && !os.IsNotExist(chmodErr) {
			_ = db.Close()
			return nil, fmt.Errorf("secure local cache: %w", chmodErr)
		}
	}
	return &Cache{db: db, serverPublicKey: serverPublicKey, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Cache) Close() error { return c.db.Close() }

func (c *Cache) Apply(ctx context.Context, event controlplane.Event, receipt controlplane.Receipt) error {
	if receipt.ProjectID != event.ProjectID || receipt.Sequence != event.Sequence ||
		receipt.EventID != event.ID || receipt.EventHash != event.Hash ||
		receipt.ActorIntentHash != event.ActorIntentHash {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "event and receipt do not match"}
	}
	if !controlplane.VerifyReceipt(receipt, c.serverPublicKey) {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "server receipt signature is invalid"}
	}
	hash, err := controlplane.HashEvent(event)
	if err != nil || hash != event.Hash {
		return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "event hash is invalid"}
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	state, sequence, head, err := loadProject(ctx, tx, event.ProjectID)
	if err != nil {
		return err
	}
	if event.Sequence <= sequence {
		var existingHash string
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT event_json FROM events WHERE project_id=? AND sequence=?`,
			event.ProjectID, event.Sequence).Scan(&raw); err != nil {
			return err
		}
		var existing controlplane.Event
		if err = json.Unmarshal(raw, &existing); err != nil {
			return err
		}
		existingHash = existing.Hash
		if existingHash != event.Hash {
			return &controlplane.Error{Code: controlplane.CodeConflict, Message: "cache sequence contains a different event"}
		}
		return tx.Commit()
	}
	if event.Sequence != sequence+1 || event.PreviousHash != head {
		return &controlplane.Error{Code: controlplane.CodeStalePrecondition, Message: "cache event stream has a gap"}
	}
	modelEvent := model.Event{
		SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
		Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
		EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
	}
	if err = service.ApplyEvent(&state, modelEvent); err != nil {
		return err
	}
	eventJSON, _ := json.Marshal(event)
	receiptJSON, _ := json.Marshal(receipt)
	stateJSON, _ := json.Marshal(state)
	if _, err = tx.ExecContext(ctx, `INSERT INTO events (project_id,sequence,event_json,receipt_json) VALUES (?,?,?,?)`,
		event.ProjectID, event.Sequence, eventJSON, receiptJSON); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO projects
		(project_id,server_sequence,server_head,state_json,updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(project_id) DO UPDATE SET server_sequence=excluded.server_sequence,
		server_head=excluded.server_head,state_json=excluded.state_json,updated_at=excluded.updated_at`,
		event.ProjectID, event.Sequence, event.Hash, stateJSON, c.now().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Cache) State(ctx context.Context, projectID string) (model.State, controlplane.ResultMetadata, error) {
	var raw []byte
	var sequence uint64
	var head string
	err := c.db.QueryRowContext(ctx, `SELECT state_json,server_sequence,server_head FROM projects WHERE project_id=?`,
		projectID).Scan(&raw, &sequence, &head)
	if errors.Is(err, sql.ErrNoRows) {
		return model.State{}, controlplane.ResultMetadata{}, &controlplane.Error{Code: controlplane.CodeOffline, Message: "project is not available in the local cache"}
	}
	if err != nil {
		return model.State{}, controlplane.ResultMetadata{}, err
	}
	var state model.State
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, controlplane.ResultMetadata{}, err
	}
	state.Integrity = model.Integrity{
		Verified: true, EventCount: int(sequence), Head: head, SyncState: "verified-cache",
	}
	return state, controlplane.ResultMetadata{
		Consistency: "CACHED", CacheSequence: sequence, Connectivity: "OFFLINE",
	}, nil
}

func (c *Cache) Events(ctx context.Context, projectID string, page controlplane.PageRequest) (controlplane.EventPage, error) {
	limit, err := page.BoundedLimit()
	if err != nil {
		return controlplane.EventPage{}, err
	}
	after, err := controlplane.DecodeCursor(page.Cursor)
	if err != nil {
		return controlplane.EventPage{}, err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT event_json,receipt_json FROM events
		WHERE project_id=? AND sequence>? ORDER BY sequence LIMIT ?`, projectID, after, limit+1)
	if err != nil {
		return controlplane.EventPage{}, err
	}
	defer rows.Close()
	items := make([]controlplane.EventRecord, 0, limit)
	for rows.Next() {
		var raw, receiptRaw []byte
		var event controlplane.Event
		if err = rows.Scan(&raw, &receiptRaw); err != nil {
			return controlplane.EventPage{}, err
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			return controlplane.EventPage{}, err
		}
		var receipt controlplane.Receipt
		if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
			return controlplane.EventPage{}, err
		}
		items = append(items, controlplane.EventRecord{Event: event, Receipt: receipt})
	}
	if err = rows.Err(); err != nil {
		return controlplane.EventPage{}, err
	}
	nextCursor := ""
	if len(items) > limit {
		items = items[:limit]
		nextCursor = controlplane.EncodeCursor(items[len(items)-1].Event.Sequence)
	}
	sequence := after
	if len(items) > 0 {
		sequence = items[len(items)-1].Event.Sequence
	}
	return controlplane.EventPage{
		Items: items, NextCursor: nextCursor,
		Metadata: controlplane.ResultMetadata{
			Consistency: "CACHED", CacheSequence: sequence, Connectivity: "OFFLINE",
		},
	}, nil
}

func (c *Cache) Rebuild(ctx context.Context, projectID string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM projects WHERE project_id=?`, projectID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT event_json,receipt_json FROM events WHERE project_id=? ORDER BY sequence`, projectID)
	if err != nil {
		return err
	}
	type pair struct{ event, receipt []byte }
	var records []pair
	for rows.Next() {
		var record pair
		if err = rows.Scan(&record.event, &record.receipt); err != nil {
			_ = rows.Close()
			return err
		}
		records = append(records, record)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// Preserve the verified event log and rebuild only the projection. Applying
	// outside the transaction keeps one small transaction per event and makes
	// interruption safely resumable.
	for _, record := range records {
		var event controlplane.Event
		var receipt controlplane.Receipt
		if err = json.Unmarshal(record.event, &event); err != nil {
			return err
		}
		if err = json.Unmarshal(record.receipt, &receipt); err != nil {
			return err
		}
		if err = c.Apply(ctx, event, receipt); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) VerifyRange(ctx context.Context, projectID string, from, to uint64) error {
	if from == 0 {
		from = 1
	}
	if to != 0 && to < from {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid verification range"}
	}
	query := `SELECT event_json,receipt_json FROM events WHERE project_id=? AND sequence>=?`
	args := []any{projectID, from}
	if to != 0 {
		query += ` AND sequence<=?`
		args = append(args, to)
	}
	query += ` ORDER BY sequence`
	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := from
	previousHash := ""
	if from > 1 {
		var raw []byte
		if err = c.db.QueryRowContext(ctx, `SELECT event_json FROM events WHERE project_id=? AND sequence=?`,
			projectID, from-1).Scan(&raw); err != nil {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: "verification range predecessor is missing"}
		}
		var previous controlplane.Event
		if err = json.Unmarshal(raw, &previous); err != nil {
			return err
		}
		previousHash = previous.Hash
	}
	for rows.Next() {
		var eventRaw, receiptRaw []byte
		if err = rows.Scan(&eventRaw, &receiptRaw); err != nil {
			return err
		}
		var event controlplane.Event
		var receipt controlplane.Receipt
		if err = json.Unmarshal(eventRaw, &event); err != nil {
			return err
		}
		if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
			return err
		}
		if event.Sequence != expected || event.PreviousHash != previousHash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache chain discontinuity at %s", event.ID)}
		}
		hash, hashErr := controlplane.HashEvent(event)
		if hashErr != nil || hash != event.Hash {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache hash mismatch at %s", event.ID)}
		}
		if !controlplane.VerifyReceipt(receipt, c.serverPublicKey) {
			return &controlplane.Error{Code: controlplane.CodeIntegrity, Message: fmt.Sprintf("cache receipt mismatch at %s", event.ID)}
		}
		previousHash = event.Hash
		expected++
	}
	return rows.Err()
}

func (c *Cache) SaveDraft(ctx context.Context, draft controlplane.Draft) error {
	if strings.TrimSpace(draft.ProjectID) == "" || strings.TrimSpace(draft.ID) == "" {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "project and draft ID are required"}
	}
	switch draft.Kind {
	case "document", "message", "artifact":
	default:
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: "draft kind must be document, message, or artifact"}
	}
	if len(draft.Body) == 0 || len(draft.Body) > controlplane.MaxDraftBytes {
		return &controlplane.Error{Code: controlplane.CodeValidation, Message: fmt.Sprintf("draft must contain 1 to %d bytes", controlplane.MaxDraftBytes)}
	}
	var count int
	var bytes int64
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(LENGTH(body)),0) FROM drafts WHERE project_id=?`,
		draft.ProjectID).Scan(&count, &bytes); err != nil {
		return err
	}
	var existingBytes int64
	existing := false
	if err := c.db.QueryRowContext(ctx, `SELECT LENGTH(body) FROM drafts WHERE project_id=? AND draft_id=?`,
		draft.ProjectID, draft.ID).Scan(&existingBytes); err == nil {
		existing = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !existing && count >= controlplane.MaxDraftsPerProject {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft count limit reached"}
	}
	if bytes-existingBytes+int64(len(draft.Body)) > controlplane.MaxDraftStorageBytes {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft storage limit reached"}
	}
	now := c.now()
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	draft.UpdatedAt = now
	_, err := c.db.ExecContext(ctx, `INSERT INTO drafts (project_id,draft_id,kind,body,created_at,updated_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(project_id,draft_id) DO UPDATE SET
		kind=excluded.kind,body=excluded.body,updated_at=excluded.updated_at`,
		draft.ProjectID, draft.ID, draft.Kind, draft.Body,
		draft.CreatedAt.Format(time.RFC3339Nano), draft.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (c *Cache) Drafts(ctx context.Context, projectID string, limit int) ([]controlplane.Draft, error) {
	if limit <= 0 {
		limit = controlplane.DefaultPageSize
	}
	if limit > controlplane.MaxPageSize {
		return nil, &controlplane.Error{Code: controlplane.CodeValidation, Message: "draft limit is too large"}
	}
	rows, err := c.db.QueryContext(ctx, `SELECT draft_id,kind,body,created_at,updated_at
		FROM drafts WHERE project_id=? ORDER BY updated_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var drafts []controlplane.Draft
	for rows.Next() {
		var draft controlplane.Draft
		var created, updated string
		draft.ProjectID = projectID
		if err = rows.Scan(&draft.ID, &draft.Kind, &draft.Body, &created, &updated); err != nil {
			return nil, err
		}
		draft.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		draft.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		drafts = append(drafts, draft)
	}
	return drafts, rows.Err()
}

func emptyState() model.State {
	return model.State{
		Agents: map[string]model.Agent{}, Tasks: map[string]model.Task{},
		Messages: map[string]model.Message{}, Approvals: map[string]model.Approval{},
		Invocations: map[string]model.Invocation{}, InvocationDeliveries: map[string]model.InvocationDelivery{},
		AgentRuntimes: map[string]model.AgentRuntime{}, InvocationPolicies: map[string]model.InvocationPolicy{},
		Decisions: map[string]model.Decision{}, Documents: map[string]model.Document{},
		Env: map[string]model.EnvEntry{}, Sessions: map[string]model.SessionPayload{},
		Artifacts:       map[string]model.Artifact{},
		ProjectSettings: model.DefaultProjectSettings(),
	}
}

func loadProject(ctx context.Context, tx *sql.Tx, projectID string) (model.State, uint64, string, error) {
	var raw []byte
	var sequence uint64
	var head string
	err := tx.QueryRowContext(ctx, `SELECT state_json,server_sequence,server_head FROM projects WHERE project_id=?`,
		projectID).Scan(&raw, &sequence, &head)
	if errors.Is(err, sql.ErrNoRows) {
		return emptyState(), 0, "", nil
	}
	if err != nil {
		return model.State{}, 0, "", err
	}
	var state model.State
	if err = json.Unmarshal(raw, &state); err != nil {
		return state, 0, "", err
	}
	return state, sequence, head, nil
}
