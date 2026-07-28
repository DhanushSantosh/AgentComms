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

const CurrentSchemaVersion = 2

const addActorKeyFingerprintMigration = `
ALTER TABLE events
ADD COLUMN IF NOT EXISTS actor_key_fingerprint TEXT NOT NULL DEFAULT '';
`

// schemaMigration is one ordered, checksummed step. Automatic migrations
// apply at every normal server startup; a migration with Automatic:false
// only applies via `agent-comms-server migrate apply --yes
// --allow-disruptive` -- normal startup refuses to start rather than
// silently run against a schema a pending disruptive migration hasn't
// been applied to. The registered migrations are currently automatic and
// non-disruptive; this structure gives any future disruptive migration a
// real place to be classified, instead of making every migration
// unconditionally automatic with no mechanism to mark one otherwise.
type schemaMigration struct {
	Version   int
	Name      string
	Automatic bool
	SQL       string
}

var schemaMigrations = []schemaMigration{
	{Version: 1, Name: "initial-hybrid-control-plane", Automatic: true, SQL: schema},
	{Version: 2, Name: "event-actor-key-fingerprint", Automatic: true, SQL: addActorKeyFingerprintMigration},
}

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
	tableExists := tableName.Valid
	statuses := make([]SchemaMigrationStatus, 0, len(schemaMigrations))
	for _, migration := range schemaMigrations {
		pending := !tableExists
		if tableExists {
			var exists bool
			if err := db.QueryRowContext(ctx, `SELECT EXISTS(
				SELECT 1 FROM schema_migrations WHERE version=$1
			)`, migration.Version).Scan(&exists); err != nil {
				return nil, err
			}
			pending = !exists
		}
		statuses = append(statuses, SchemaMigrationStatus{
			Version: migration.Version, Name: migration.Name,
			Pending: pending, Automatic: migration.Automatic,
		})
	}
	return statuses, nil
}

// ApplySchema applies every pending Automatic migration, in order, inside
// one transaction protected by a Postgres advisory lock held for the
// transaction's full duration (pg_advisory_xact_lock releases automatically
// at commit/rollback, so there is no pool-checkout gap between acquiring it
// and applying migrations). If a pending migration is not Automatic and
// allowDisruptive is false, ApplySchema refuses to proceed at all -- normal
// server startup (allowDisruptive=false) must never silently run against a
// schema a disruptive migration hasn't been applied to; only
// `agent-comms-server migrate apply --yes --allow-disruptive` passes true.
func ApplySchema(ctx context.Context, db *sql.DB, allowDisruptive bool) error {
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
	for _, migration := range schemaMigrations {
		sum := sha256.Sum256([]byte(migration.SQL))
		checksum := hex.EncodeToString(sum[:])
		var recordedChecksum string
		queryErr := tx.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`,
			migration.Version).Scan(&recordedChecksum)
		switch {
		case queryErr == nil:
			if recordedChecksum != checksum {
				return fmt.Errorf("authority migration %d checksum mismatch", migration.Version)
			}
		case queryErr == sql.ErrNoRows:
			if !migration.Automatic && !allowDisruptive {
				return fmt.Errorf(
					"authority migration %d (%s) is disruptive and has not been applied; run `agent-comms-server migrate apply --yes --allow-disruptive`",
					migration.Version, migration.Name)
			}
			if _, err = tx.ExecContext(ctx, migration.SQL); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations
				(version,name,checksum,build_id) VALUES ($1,$2,$3,$4)`,
				migration.Version, migration.Name, checksum, buildinfo.ResolvedBuildID()); err != nil {
				return err
			}
		default:
			return queryErr
		}
	}
	return tx.Commit()
}
