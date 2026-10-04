package authority

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestBackfillRestoresEntityTimestampsFromHistory covers RFC 0041's
// PostgreSQL migration: a database whose entity state predates
// event-derived timestamps refuses normal startup while it has history to
// replay, and the confirmed migration restores exactly the times the live
// projection records.
func TestBackfillRestoresEntityTimestampsFromHistory(t *testing.T) {
	databaseURL := migrationDatabaseURL(t)
	ctx := context.Background()
	serviceSigner, _ := controlplane.GenerateSigner()
	engine, err := Open(ctx, Config{DatabaseURL: databaseURL}, serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	projectID := "backfill-" + uuid.NewString()
	if err = engine.CreateProject(ctx, projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	mutate := func(typ, entity string, payload any) {
		t.Helper()
		raw, encodeErr := model.EncodePayload(typ, payload)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: "owner", Type: typ, EntityID: entity,
			Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if typ == "agent.register" {
			command.PublicKey = owner.PublicKey()
		}
		if signErr := command.Sign(owner.PrivateKey()); signErr != nil {
			t.Fatal(signErr)
		}
		if _, _, mutateErr := engine.Mutate(ctx, command); mutateErr != nil {
			t.Fatalf("%s %s: %v", typ, entity, mutateErr)
		}
	}
	mutate("agent.register", "owner", model.AgentRegistered{PublicKey: owner.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "owner"})
	mutate("agent.activate", "owner", model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})
	mutate("task.create", "task-a", model.TaskCreated{Title: "A", Repository: "local", Branch: "dev", Resources: []string{"src"}})
	mutate("message.post", "msg-older", model.MessagePosted{Kind: "ACTION", To: []string{"owner"}, Subject: "older", Body: "b"})
	mutate("message.post", "a-newer", model.MessagePosted{Kind: "FYI", To: []string{"owner"}, Subject: "newer", Body: "b"})
	mutate("message.ack", "msg-older", model.MessageResponse{Response: "ACKNOWLEDGED"})
	live, _, err := engine.State(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Messages["a-newer"].CreatedSequence <= live.Messages["msg-older"].CreatedSequence {
		t.Fatal("test setup: live projection lacks creation order")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Make this project look like it was written before RFC 0041.
	for _, table := range []string{"agents", "tasks", "messages", "invocations", "agent_runtimes", "invocation_policies", "approvals", "documents", "artifacts", "environment_entries"} {
		if _, err = db.ExecContext(ctx, `UPDATE `+table+` SET state = state - 'created_at' - 'updated_at' - 'created_sequence' - 'updated_sequence' WHERE project_id=$1`, projectID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=7`); err != nil {
		t.Fatal(err)
	}
	stripped, _, err := engine.State(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !stripped.Messages["msg-older"].CreatedAt.IsZero() {
		t.Fatal("test setup: timestamps were not stripped")
	}

	if err = ApplySchema(ctx, db, false); err == nil {
		t.Fatal("with history to replay, the backfill must require --allow-disruptive")
	}
	if err = ApplySchema(ctx, db, true); err != nil {
		t.Fatalf("confirmed backfill failed: %v", err)
	}
	restored, _, err := engine.State(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]any{
		"agents": {live.Agents, restored.Agents}, "tasks": {live.Tasks, restored.Tasks},
		"messages": {live.Messages, restored.Messages},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Fatalf("%s not restored to the live timestamps:\nlive:     %+v\nrestored: %+v", name, pair[0], pair[1])
		}
	}
	if _, order, _ := model.Inbox(restored, "owner", model.InboxOptions{Limit: 1}); len(order) != 1 || order[0] != "a-newer" {
		t.Fatalf("inbox --limit 1 = %v, want a-newer", order)
	}
}

// TestNewerSchemaIsRefused proves an older binary will not run against a
// database a newer one has migrated.
func TestNewerSchemaIsRefused(t *testing.T) {
	databaseURL := migrationDatabaseURL(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = ApplySchema(ctx, db, true); err != nil {
		t.Fatal(err)
	}
	const future = 900002
	if _, err = db.ExecContext(ctx, `INSERT INTO schema_migrations (version,name,checksum,build_id) VALUES ($1,'future','x','test')`, future); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=$1`, future) }()
	if err = ApplySchema(ctx, db, true); err == nil {
		t.Fatal("a database migrated by a newer binary must be refused")
	}
}

func TestBackfillRefusesUnverifiableHistoryWithoutPublishing(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	ctx := context.Background()
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(ctx, Config{DatabaseURL: databaseURL}, serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	projectID := "backfill-rejection-" + uuid.NewString()
	if err := engine.CreateProject(ctx, projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		typ     string
		payload any
	}{
		{"agent.register", model.AgentRegistered{PublicKey: owner.PublicKey(), PrincipalType: model.PrincipalHuman}},
		{"agent.activate", model.AgentActivated{Role: model.RoleOwner, Scopes: []string{"*"}, Capabilities: []string{"*"}}},
	} {
		raw, err := model.EncodePayload(step.typ, step.payload)
		if err != nil {
			t.Fatal(err)
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: "owner", Type: step.typ, EntityID: "owner",
			PublicKey: owner.PublicKey(), Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if err := command.Sign(owner.PrivateKey()); err != nil {
			t.Fatal(err)
		}
		if _, _, err := engine.Mutate(ctx, command); err != nil {
			t.Fatal(err)
		}
	}
	before, beforeMetadata, err := engine.State(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := engine.Events(ctx, projectID, controlplane.PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, corruption string
	}{
		{"changed-event", `UPDATE events SET entity_id='forged' WHERE project_id=$1 AND sequence=1`},
		{"missing-event", `DELETE FROM events WHERE project_id=$1 AND sequence=1`},
		{"wrong-head", `UPDATE projects SET head_hash='forged' WHERE project_id=$1`},
		{"missing-projection", `DELETE FROM agents WHERE project_id=$1 AND agent_id='owner'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := engine.db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			// All corruption stays inside the transaction. Rolling it back
			// must leave the shared test database's signed history intact.
			if _, err := tx.ExecContext(ctx, test.corruption, projectID); err != nil {
				t.Fatal(err)
			}
			var sequence uint64
			var head string
			if err := tx.QueryRowContext(ctx, `SELECT head_sequence,head_hash FROM projects WHERE project_id=$1 FOR UPDATE`, projectID).Scan(&sequence, &head); err != nil {
				t.Fatal(err)
			}
			if err := backfillProject(ctx, tx, projectID, sequence, head); !errors.Is(err, errBackfillUnverifiable) {
				t.Fatalf("migration must reject unverifiable history/projection: %v", err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			after, metadata, err := engine.State(ctx, projectID)
			if err != nil {
				t.Fatal(err)
			}
			afterEvents, err := engine.Events(ctx, projectID, controlplane.PageRequest{})
			if err != nil {
				t.Fatal(err)
			}
			var restoredHead string
			if err := engine.db.QueryRowContext(ctx, `SELECT head_hash FROM projects WHERE project_id=$1`, projectID).Scan(&restoredHead); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || metadata.ServerSequence != beforeMetadata.ServerSequence || restoredHead != beforeEvents.Items[len(beforeEvents.Items)-1].Event.Hash || !reflect.DeepEqual(beforeEvents.Items, afterEvents.Items) {
				t.Fatal("failed migration changed stored state, event history or head")
			}
		})
	}
}
