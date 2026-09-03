//go:build !js

package projectlifecycle

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestFoldDecisionsIntoDocuments covers RFC 0029's personal-mode /
// projection-cache migration: a stored state_json snapshot with a
// `decisions` map is rewritten so those become `decision`-tagged
// documents, and running it again is a no-op.
func TestFoldDecisionsIntoDocuments(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE projects (project_id TEXT PRIMARY KEY, state_json BLOB NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"decisions": map[string]any{
			"d1": map[string]any{"id": "d1", "title": "Adopt trunk", "statement": "Land on main", "status": "ACTIVE"},
			"d2": map[string]any{"id": "d2", "title": "Revised", "statement": "Short-lived branches", "status": "SUPERSEDED", "supersedes": ""},
		},
		"documents": map[string]any{
			"doc1": map[string]any{"id": "doc1", "title": "Runbook", "body": "steps", "tags": []string{}, "status": "ACTIVE"},
		},
	}
	blob, _ := json.Marshal(state)
	if _, err = db.Exec(`INSERT INTO projects VALUES (?, ?)`, "proj", blob); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	for pass := 0; pass < 2; pass++ {
		if err = foldDecisionsIntoDocuments(dbPath); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	db, _ = sql.Open("sqlite", dbPath)
	defer func() { _ = db.Close() }()
	var out []byte
	if err = db.QueryRow(`SELECT state_json FROM projects WHERE project_id='proj'`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err = json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["decisions"]; ok {
		t.Fatal("decisions key should be gone")
	}
	var docs map[string]map[string]any
	if err = json.Unmarshal(got["documents"], &docs); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"doc1", "d1", "d2"} {
		if _, ok := docs[id]; !ok {
			t.Fatalf("expected document %q", id)
		}
	}
	if docs["d1"]["body"] != "Land on main" {
		t.Fatalf("d1 body = %v, want the decision statement", docs["d1"]["body"])
	}
	tags, _ := docs["d2"]["tags"].([]any)
	if len(tags) != 1 || tags[0] != "decision" {
		t.Fatalf("d2 tags = %v, want [decision]", docs["d2"]["tags"])
	}
	if docs["d2"]["status"] != "SUPERSEDED" {
		t.Fatalf("d2 status = %v, want SUPERSEDED", docs["d2"]["status"])
	}
}

func TestFoldDecisionsIntoDocumentsMissingDBIsNoOp(t *testing.T) {
	if err := foldDecisionsIntoDocuments(filepath.Join(t.TempDir(), "nope.db")); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove("nope.db")
}
