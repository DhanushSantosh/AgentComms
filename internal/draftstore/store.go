package draftstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=FULL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
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
PRAGMA user_version=1;
`

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("draft store path is required")
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
		return nil, fmt.Errorf("initialize draft store: %w", err)
	}
	for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
		if chmodErr := os.Chmod(databaseFile, 0o600); chmodErr != nil && !os.IsNotExist(chmodErr) {
			_ = db.Close()
			return nil, fmt.Errorf("secure draft store: %w", chmodErr)
		}
	}
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) Close() error {
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

func (s *Store) SaveDraft(ctx context.Context, draft controlplane.Draft) error {
	return s.saveDraft(ctx, draft, false)
}

func (s *Store) ImportDraft(ctx context.Context, draft controlplane.Draft) error {
	return s.saveDraft(ctx, draft, true)
}

func (s *Store) saveDraft(ctx context.Context, draft controlplane.Draft, preserveTimes bool) error {
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
	var usedBytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(LENGTH(body)),0) FROM drafts WHERE project_id=?`,
		draft.ProjectID).Scan(&count, &usedBytes); err != nil {
		return err
	}
	var existingBytes int64
	existing := false
	if err := s.db.QueryRowContext(ctx, `SELECT LENGTH(body) FROM drafts WHERE project_id=? AND draft_id=?`,
		draft.ProjectID, draft.ID).Scan(&existingBytes); err == nil {
		existing = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !existing && count >= controlplane.MaxDraftsPerProject {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft count limit reached"}
	}
	if usedBytes-existingBytes+int64(len(draft.Body)) > controlplane.MaxDraftStorageBytes {
		return &controlplane.Error{Code: controlplane.CodeRateLimited, Message: "local draft storage limit reached"}
	}
	now := s.now()
	if draft.CreatedAt.IsZero() {
		draft.CreatedAt = now
	}
	if !preserveTimes || draft.UpdatedAt.IsZero() {
		draft.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO drafts (project_id,draft_id,kind,body,created_at,updated_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(project_id,draft_id) DO UPDATE SET
		kind=excluded.kind,body=excluded.body,updated_at=excluded.updated_at`,
		draft.ProjectID, draft.ID, draft.Kind, draft.Body,
		draft.CreatedAt.Format(time.RFC3339Nano), draft.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) Drafts(ctx context.Context, projectID string, limit int) ([]controlplane.Draft, error) {
	if limit <= 0 {
		limit = controlplane.DefaultPageSize
	}
	if limit > controlplane.MaxPageSize {
		return nil, &controlplane.Error{Code: controlplane.CodeValidation, Message: "draft limit is too large"}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT draft_id,kind,body,created_at,updated_at
		FROM drafts WHERE project_id=? ORDER BY updated_at DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	drafts := []controlplane.Draft{}
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
