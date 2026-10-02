//go:build !js

package projectlifecycle_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
	_ "modernc.org/sqlite"
)

// seedTimestampProject writes a little real, signed history: a task, two
// messages and an acknowledgement, so entities have distinct creation and
// update events.
func seedTimestampProject(t *testing.T) string {
	t.Helper()
	svc, root := testsupport.StartPersonalProject(t)
	steps := []struct {
		typ, id string
		payload any
	}{
		{"task.create", "task-a", model.TaskCreated{Title: "A", Repository: "local", Branch: "dev", Resources: []string{"src"}}},
		{"message.post", "msg-older", model.MessagePosted{Kind: "ACTION", To: []string{"owner"}, Subject: "older", Body: "b"}},
		{"message.post", "a-newer", model.MessagePosted{Kind: "FYI", To: []string{"owner"}, Subject: "newer", Body: "b"}},
		{"message.ack", "msg-older", model.MessageResponse{Response: "ACKNOWLEDGED"}},
	}
	for _, step := range steps {
		if _, err := svc.Execute("owner", step.typ, step.id, step.payload); err != nil {
			t.Fatalf("%s %s: %v", step.typ, step.id, err)
		}
	}
	if _, err := svc.Sync(); err != nil {
		t.Fatal(err)
	}
	return root
}

func readSnapshot(t *testing.T, path string) (model.State, []byte) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var raw []byte
	if err = db.QueryRow(`SELECT state_json FROM projects`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state model.State
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	return state, raw
}

func writeSnapshot(t *testing.T, path string, raw []byte) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`UPDATE projects SET state_json=?`, raw); err != nil {
		t.Fatal(err)
	}
}

// stripClocks removes every RFC 0041 field, producing what a pre-upgrade
// snapshot looked like.
func stripClocks(t *testing.T, raw []byte) []byte {
	t.Helper()
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	for _, collection := range []string{"agents", "tasks", "messages", "approvals", "documents", "invocations", "agent_runtimes", "artifacts", "env", "invocation_policies"} {
		var entities map[string]map[string]json.RawMessage
		if len(state[collection]) == 0 || json.Unmarshal(state[collection], &entities) != nil {
			continue
		}
		for _, entity := range entities {
			for _, key := range []string{"created_sequence", "updated_sequence", "created_at", "updated_at"} {
				delete(entity, key)
			}
		}
		state[collection], _ = json.Marshal(entities)
	}
	out, _ := json.Marshal(state)
	return out
}

func TestRebuildFromHistoryReproducesTheLiveSnapshot(t *testing.T) {
	root := seedTimestampProject(t)
	path := runtimeinit.DatabasePath(root)
	live, _ := readSnapshot(t, path)
	if err := projectlifecycle.RebuildPersonalSnapshots(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	rebuilt, _ := readSnapshot(t, path)
	if !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("replay must reproduce the live snapshot exactly\nlive:    %+v\nrebuilt: %+v", live, rebuilt)
	}
}

func TestRebuildRestoresTimestampsOnALegacySnapshot(t *testing.T) {
	root := seedTimestampProject(t)
	path := runtimeinit.DatabasePath(root)
	live, raw := readSnapshot(t, path)
	writeSnapshot(t, path, stripClocks(t, raw))
	legacy, _ := readSnapshot(t, path)
	if !legacy.Messages["msg-older"].CreatedAt.IsZero() || legacy.Tasks["task-a"].CreatedSequence != 0 {
		t.Fatal("test setup: legacy snapshot still carries timestamps")
	}
	if err := projectlifecycle.RebuildPersonalSnapshots(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	rebuilt, _ := readSnapshot(t, path)
	if !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("legacy snapshot was not restored to the live timestamps\nlive:    %+v\nrebuilt: %+v", live, rebuilt)
	}
	older, newer := rebuilt.Messages["msg-older"], rebuilt.Messages["a-newer"]
	if older.CreatedAt.IsZero() || older.CreatedSequence >= newer.CreatedSequence {
		t.Fatalf("creation order lost: older=%d newer=%d", older.CreatedSequence, newer.CreatedSequence)
	}
	if older.UpdatedSequence <= newer.CreatedSequence {
		t.Fatalf("the later acknowledgement must move msg-older's update time: %+v", older)
	}
	// The incident: a newer message must lead the inbox regardless of IDs.
	if _, order, _ := model.Inbox(rebuilt, "owner", model.InboxOptions{Limit: 1}); len(order) != 1 || order[0] != "a-newer" {
		t.Fatalf("inbox --limit 1 = %v, want the newest message a-newer", order)
	}
}

func tamperFirstMessageEvent(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var sequence uint64
	var raw []byte
	if err = db.QueryRow(`SELECT sequence,event_json FROM events WHERE CAST(event_json AS TEXT) LIKE '%message.post%' ORDER BY sequence LIMIT 1`).Scan(&sequence, &raw); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err = json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	event["entity_id"] = "forged"
	raw, _ = json.Marshal(event)
	if _, err = db.Exec(`UPDATE events SET event_json=? WHERE sequence=?`, raw, sequence); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildRefusesTamperedAuthorityHistory(t *testing.T) {
	root := seedTimestampProject(t)
	path := runtimeinit.DatabasePath(root)
	_, before := readSnapshot(t, path)
	tamperFirstMessageEvent(t, path)
	if err := projectlifecycle.RebuildPersonalSnapshots(context.Background(), path); err == nil {
		t.Fatal("a tampered authority history must fail the migration, not publish guessed state")
	}
	if _, after := readSnapshot(t, path); string(before) != string(after) {
		t.Fatal("a failed rebuild must leave the authority snapshot untouched")
	}
}

func TestRebuildDropsAnUnverifiableCacheForRefetch(t *testing.T) {
	root := seedTimestampProject(t)
	path := runtimeinit.ProjectionPath(root)
	tamperFirstMessageEvent(t, path)
	if err := projectlifecycle.RebuildCacheSnapshots(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var projects, events int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM projects),(SELECT COUNT(*) FROM events)`).Scan(&projects, &events); err != nil {
		t.Fatal(err)
	}
	if projects != 0 || events != 0 {
		t.Fatalf("unverifiable cache project must be dropped for refetch: projects=%d events=%d", projects, events)
	}
}
