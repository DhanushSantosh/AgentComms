package projectlifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/draftstore"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	_ "modernc.org/sqlite"
)

func TestReconcileUpgradesBaselineWithoutChangingSignedEventsOrDrafts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	store.RuntimeVersion = "0.1.0"
	store.RuntimeBuildID = "old-build"
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, store.Runtime, "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["project_format_version"] = 0
	config["managed_files_version"] = 0
	config["toolkit_build_id"] = ""
	delete(config, "managed_file_hashes")
	config["legacy_read_only"] = true
	raw, _ = json.MarshalIndent(config, "", "  ")
	if err = os.WriteFile(configPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, store.Runtime, "AGENT_INSTRUCTIONS.md"), []byte("user modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	authorityPath := runtimeinit.DatabasePath(root)
	before := eventBytes(t, authorityPath)
	if err = setSQLiteVersionForTest(authorityPath, 0); err != nil {
		t.Fatal(err)
	}
	var typedConfig store.Config
	raw, _ = os.ReadFile(configPath)
	if err = json.Unmarshal(raw, &typedConfig); err != nil {
		t.Fatal(err)
	}
	cache, err := localcache.Open(runtimeinit.ProjectionPath(root), typedConfig.ServicePublicKey)
	if err != nil {
		t.Fatal(err)
	}
	draft := controlplane.Draft{
		ProjectID: typedConfig.ProjectID, ID: "draft-1", Kind: "message",
		Body:      json.RawMessage(`{"body":"preserve me"}`),
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	if err = cache.SaveDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if err = cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err = setSQLiteVersionForTest(runtimeinit.ProjectionPath(root), 0); err != nil {
		t.Fatal(err)
	}

	plan, _, err := Inspect(root, "0.2.0", "new-build")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) < 4 {
		t.Fatalf("actions=%v, want project, managed files, authority, cache, draft, and toolkit work", plan.Actions)
	}
	result, err := Reconcile(context.Background(), Options{
		Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, StopDaemon: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Verified || result.BackupPath == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	after := eventBytes(t, authorityPath)
	if string(before) != string(after) {
		t.Fatal("signed authority events changed during project reconciliation")
	}
	if _, err = store.Open(root).Config(); err != nil {
		t.Fatalf("canonical config is not strict-decodable: %v", err)
	}
	drafts, err := draftstore.Open(runtimeinit.DraftPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer drafts.Close()
	imported, err := drafts.Drafts(context.Background(), typedConfig.ProjectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 || imported[0].ID != draft.ID || string(imported[0].Body) != string(draft.Body) {
		t.Fatalf("draft was not preserved: %+v", imported)
	}
	if _, err = os.Stat(filepath.Join(result.BackupPath, store.Runtime, "AGENT_INSTRUCTIONS.md")); err != nil {
		t.Fatalf("modified instructions were not backed up: %v", err)
	}
}

func TestLifecycleLockSerializesClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.lock")
	first, err := lockFile(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockFile(first)
	started := time.Now()
	if _, err = lockFile(path, 100*time.Millisecond); err != errLifecycleLocked {
		t.Fatalf("lock error=%v, want %v", err, errLifecycleLocked)
	}
	if time.Since(started) < 100*time.Millisecond {
		t.Fatal("second lifecycle client did not wait for its bounded timeout")
	}
}

func TestFreshProjectDoesNotRequireImmediateUpgrade(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	store.RuntimeVersion = "0.1.0"
	store.RuntimeBuildID = "current-build"
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	plan, _, err := Inspect(root, "0.1.0", "current-build")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Fatalf("fresh project requires upgrade actions: %+v", plan.Actions)
	}
}

func eventBytes(t *testing.T, path string) []byte {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT event_json FROM events ORDER BY sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var combined []byte
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		combined = append(combined, raw...)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return combined
}

func setSQLiteVersionForTest(path string, version int) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec("PRAGMA user_version=" + strconv.Itoa(version))
	return err
}
