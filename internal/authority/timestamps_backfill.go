package authority

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projection"
)

// entityTimestampsBackfill is RFC 0041's derived-state migration. Entity
// state is stored as JSONB, so new events already carry created/updated
// times and sequences without any column change; entities written before
// this release have none. The backfill replays each project's signed events
// through the same projection code and rewrites the state of entities whose
// times changed. It verifies the whole hash chain first, and refuses rather
// than guessing when history and stored projection disagree. No signed
// event is rewritten.
//
// The text below is this migration's checksum source; keep it stable.
const entityTimestampsBackfill = `-- go migration: RFC 0041 replay signed events into entity created/updated times (v1)`

var errBackfillUnverifiable = errors.New("history cannot be verified")

// backfillNeedsConfirmation makes the backfill disruptive only when there is
// history to replay; a fresh database has none and applies it automatically.
func backfillNeedsConfirmation(ctx context.Context, tx *sql.Tx) (bool, error) {
	var hasEvents bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM events)`).Scan(&hasEvents)
	return hasEvents, err
}

func backfillEntityTimestamps(ctx context.Context, tx *sql.Tx) error {
	// FOR UPDATE holds every project's head for the migration, so no write
	// can land between reading a head and replaying its events.
	rows, err := tx.QueryContext(ctx, `SELECT project_id,head_sequence,head_hash FROM projects ORDER BY project_id FOR UPDATE`)
	if err != nil {
		return err
	}
	type head struct {
		projectID, hash string
		sequence        uint64
	}
	var projects []head
	for rows.Next() {
		var item head
		if err = rows.Scan(&item.projectID, &item.sequence, &item.hash); err != nil {
			_ = rows.Close()
			return err
		}
		projects = append(projects, item)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, project := range projects {
		if err = backfillProject(ctx, tx, project.projectID, project.sequence, project.hash); err != nil {
			return fmt.Errorf("backfill entity timestamps for project %s: %w", project.projectID, err)
		}
	}
	return nil
}

func backfillProject(ctx context.Context, tx *sql.Tx, projectID string, headSequence uint64, headHash string) error {
	replayed, err := replayProjectEvents(ctx, tx, projectID, headSequence, headHash)
	if err != nil {
		return err
	}
	stored, err := loadState(ctx, tx, projectID)
	if err != nil {
		return err
	}
	collections := []error{
		rewriteChangedStates(ctx, tx, projectID, "agents", "agent_id", stored.Agents, replayed.Agents),
		rewriteChangedStates(ctx, tx, projectID, "tasks", "task_id", stored.Tasks, replayed.Tasks),
		rewriteChangedStates(ctx, tx, projectID, "messages", "message_id", stored.Messages, replayed.Messages),
		rewriteChangedStates(ctx, tx, projectID, "invocations", "invocation_id", stored.Invocations, replayed.Invocations),
		rewriteChangedStates(ctx, tx, projectID, "agent_runtimes", "runtime_id", stored.AgentRuntimes, replayed.AgentRuntimes),
		rewriteChangedStates(ctx, tx, projectID, "invocation_policies", "agent_id", stored.InvocationPolicies, replayed.InvocationPolicies),
		rewriteChangedStates(ctx, tx, projectID, "approvals", "approval_id", stored.Approvals, replayed.Approvals),
		rewriteChangedStates(ctx, tx, projectID, "documents", "document_id", stored.Documents, replayed.Documents),
		rewriteChangedStates(ctx, tx, projectID, "artifacts", "sha256", stored.Artifacts, replayed.Artifacts),
		rewriteChangedStates(ctx, tx, projectID, "environment_entries", "entry_key", stored.Env, replayed.Env),
	}
	return errors.Join(collections...)
}

func replayProjectEvents(ctx context.Context, tx *sql.Tx, projectID string, headSequence uint64, headHash string) (model.State, error) {
	rows, err := tx.QueryContext(ctx, `SELECT project_id,sequence,event_id,event_time,actor_id,actor_key_fingerprint,event_type,
		entity_id,payload,previous_hash,event_hash,actor_intent_hash,idempotency_key
		FROM events WHERE project_id=$1 ORDER BY sequence`, projectID)
	if err != nil {
		return model.State{}, err
	}
	defer func() { _ = rows.Close() }()
	state := model.EmptyState()
	expected, previousHash := uint64(1), ""
	for rows.Next() {
		var event controlplane.Event
		if err = rows.Scan(&event.ProjectID, &event.Sequence, &event.ID, &event.Time, &event.Actor, &event.ActorKeyFingerprint,
			&event.Type, &event.EntityID, &event.Payload, &event.PreviousHash, &event.Hash,
			&event.ActorIntentHash, &event.IdempotencyKey); err != nil {
			return model.State{}, err
		}
		// TIMESTAMPTZ scans in the server's zone; live projection writes UTC.
		// The instant is identical, but stored state must match live writes.
		event.Time = event.Time.UTC()
		if event.Sequence != expected {
			return model.State{}, fmt.Errorf("%w: expected event %d, found %d", errBackfillUnverifiable, expected, event.Sequence)
		}
		if event.PreviousHash != previousHash {
			return model.State{}, fmt.Errorf("%w: event %d does not continue the hash chain", errBackfillUnverifiable, event.Sequence)
		}
		hash, hashErr := controlplane.HashEvent(event)
		if hashErr != nil || hash != event.Hash {
			return model.State{}, fmt.Errorf("%w: event %d hash does not match its contents", errBackfillUnverifiable, event.Sequence)
		}
		if err = projection.ApplyEvent(&state, model.Event{
			SchemaVersion: model.SchemaVersion, PayloadVersion: 1, ID: event.ID,
			Sequence: event.Sequence, Time: event.Time, Actor: event.Actor, Type: event.Type,
			EntityID: event.EntityID, Data: event.Payload, PreviousHash: event.PreviousHash, Hash: event.Hash,
		}); err != nil {
			return model.State{}, fmt.Errorf("replay event %d: %w", event.Sequence, err)
		}
		previousHash = event.Hash
		expected++
	}
	if err = rows.Err(); err != nil {
		return model.State{}, err
	}
	if expected-1 != headSequence || previousHash != headHash {
		return model.State{}, fmt.Errorf("%w: history ends at event %d but the project head is %d", errBackfillUnverifiable, expected-1, headSequence)
	}
	return state, nil
}

// rewriteChangedStates updates only the JSONB state of entities whose
// replayed value differs, leaving each row's other columns (including
// updated_sequence bookkeeping) untouched. Replay and storage must agree on
// which entities exist; a disagreement means the projection does not match
// its history, which the backfill refuses to paper over.
func rewriteChangedStates[V any](ctx context.Context, tx *sql.Tx, projectID, table, idColumn string, stored, replayed map[string]V) error {
	if len(stored) != len(replayed) {
		return fmt.Errorf("%w: %s has %d stored entities but history produces %d", errBackfillUnverifiable, table, len(stored), len(replayed))
	}
	for id, value := range replayed {
		current, ok := stored[id]
		if !ok {
			return fmt.Errorf("%w: %s %q exists in history but not in the stored projection", errBackfillUnverifiable, table, id)
		}
		if reflect.DeepEqual(current, value) {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET state=$1 WHERE project_id=$2 AND %s=$3`, table, idColumn),
			raw, projectID, id); err != nil {
			return err
		}
	}
	return nil
}
