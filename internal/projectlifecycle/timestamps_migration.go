//go:build !js

package projectlifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
)

// errUnverifiableHistory marks a project whose stored events cannot be
// proven to be the complete, unbroken chain behind its snapshot.
var errUnverifiableHistory = errors.New("event history cannot be verified")

// snapshotStore names the columns that differ between the personal
// authority and the projection cache; both keep projects(state_json) next
// to events(event_json) in signed sequence order.
type snapshotStore struct {
	component            string
	headSequence, headID string
	// disposable stores (the projection cache) drop a project they cannot
	// verify so the daemon refetches it; the authority fails instead.
	disposable bool
}

var (
	personalAuthoritySnapshots = snapshotStore{component: "personal_authority", headSequence: "head_sequence", headID: "head_hash"}
	projectionCacheSnapshots   = snapshotStore{component: "projection_cache", headSequence: "server_sequence", headID: "server_head", disposable: true}
)

// rebuildSnapshotsFromEvents replays each project's signed events, in
// sequence order from an empty state, through the same projection code the
// authority uses, and replaces state_json with the result (RFC 0041). It
// gives entities created before event-derived timestamps their real
// creation and update times. Nothing is guessed: every event must continue
// the hash chain from sequence 1 and the chain must end at the recorded
// head, or the project fails (authority) or is dropped for refetch (cache).
// No signed event is rewritten, and replay is idempotent.
func rebuildSnapshotsFromEvents(ctx context.Context, dbPath string, target snapshotStore) error {
	if err := rejectSymlinkPath(dbPath); err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	db, err := sqliteOpen(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	projectIDs, err := snapshotProjectIDs(ctx, db)
	if err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if err = rebuildOneSnapshot(ctx, db, projectID, target); err != nil {
			return fmt.Errorf("%s: rebuild project %s from its event history: %w", target.component, projectID, err)
		}
	}
	return nil
}

func snapshotProjectIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT project_id FROM projects ORDER BY project_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func rebuildOneSnapshot(ctx context.Context, db *sql.DB, projectID string, target snapshotStore) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var headSequence uint64
	var headID string
	if err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s,%s FROM projects WHERE project_id=?`, target.headSequence, target.headID),
		projectID).Scan(&headSequence, &headID); err != nil {
		return err
	}
	state, replayErr := replayVerifiedEvents(ctx, tx, projectID, headSequence, headID)
	if replayErr != nil {
		if !target.disposable || !errors.Is(replayErr, errUnverifiableHistory) {
			return replayErr
		}
		// The cache is a disposable copy: forget this project so the next
		// sync refetches its whole history from the authority.
		for _, statement := range []string{`DELETE FROM events WHERE project_id=?`, `DELETE FROM projects WHERE project_id=?`} {
			if _, err = tx.ExecContext(ctx, statement, projectID); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE projects SET state_json=? WHERE project_id=?`, stateJSON, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func replayVerifiedEvents(ctx context.Context, tx *sql.Tx, projectID string, headSequence uint64, headID string) (model.State, error) {
	rows, err := tx.QueryContext(ctx, `SELECT sequence,event_json FROM events WHERE project_id=? ORDER BY sequence`, projectID)
	if err != nil {
		return model.State{}, err
	}
	defer func() { _ = rows.Close() }()
	state := model.EmptyState()
	expected, previousHash := uint64(1), ""
	for rows.Next() {
		var sequence uint64
		var raw []byte
		if err = rows.Scan(&sequence, &raw); err != nil {
			return model.State{}, err
		}
		var event controlplane.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return model.State{}, fmt.Errorf("%w: event %d is not readable: %v", errUnverifiableHistory, sequence, err)
		}
		if sequence != expected || event.Sequence != expected || event.ProjectID != projectID {
			return model.State{}, fmt.Errorf("%w: expected event %d, found %d", errUnverifiableHistory, expected, sequence)
		}
		if event.PreviousHash != previousHash {
			return model.State{}, fmt.Errorf("%w: event %d does not continue the hash chain", errUnverifiableHistory, sequence)
		}
		hash, hashErr := controlplane.HashEvent(event)
		if hashErr != nil || hash != event.Hash {
			return model.State{}, fmt.Errorf("%w: event %d hash does not match its contents", errUnverifiableHistory, sequence)
		}
		if err = projection.ApplyEvent(&state, model.Event{
			SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
			Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
			EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
		}); err != nil {
			return model.State{}, fmt.Errorf("replay event %d: %w", sequence, err)
		}
		previousHash = event.Hash
		expected++
	}
	if err = rows.Err(); err != nil {
		return model.State{}, err
	}
	if expected-1 != headSequence || previousHash != headID {
		return model.State{}, fmt.Errorf("%w: history ends at event %d but the project head is %d", errUnverifiableHistory, expected-1, headSequence)
	}
	return state, nil
}
