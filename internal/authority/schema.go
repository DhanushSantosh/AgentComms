package authority

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"fmt"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
)

//go:embed schema.sql
var schema string

const CurrentSchemaVersion = 1

type SchemaMigrationStatus struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Pending   bool   `json:"pending"`
	Automatic bool   `json:"automatic"`
}

func SchemaPlan(ctx context.Context, db *sql.DB) ([]SchemaMigrationStatus, error) {
	var tableName sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.schema_migrations')`).Scan(&tableName); err != nil {
		return nil, err
	}
	pending := !tableName.Valid
	if !pending {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(
			SELECT 1 FROM schema_migrations WHERE version=$1
		)`, CurrentSchemaVersion).Scan(&exists); err != nil {
			return nil, err
		}
		pending = !exists
	}
	return []SchemaMigrationStatus{{
		Version: CurrentSchemaVersion, Name: "initial-hybrid-control-plane",
		Pending: pending, Automatic: true,
	}}, nil
}

func ApplySchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2073326601)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		build_id TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(schema))
	checksum := hex.EncodeToString(sum[:])
	var recordedChecksum string
	queryErr := tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`,
		CurrentSchemaVersion).Scan(&recordedChecksum)
	switch {
	case queryErr == nil:
		if recordedChecksum != checksum {
			return fmt.Errorf("authority migration %d checksum mismatch", CurrentSchemaVersion)
		}
	case queryErr == sql.ErrNoRows:
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations
			(version,name,checksum,build_id) VALUES ($1,$2,$3,$4)`,
			CurrentSchemaVersion, "initial-hybrid-control-plane", checksum, buildinfo.ResolvedBuildID()); err != nil {
			return err
		}
	default:
		return queryErr
	}
	return tx.Commit()
}
