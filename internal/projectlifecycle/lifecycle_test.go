package projectlifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/draftstore"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/model"
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
	if !plan.RequiresConfirmation {
		t.Fatal("expected personal_authority/draft_store migrations to require confirmation by default")
	}
	if _, err = Reconcile(context.Background(), Options{
		Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, StopDaemon: false,
	}); err == nil {
		t.Fatal("expected Reconcile without Approved to require confirmation")
	} else if lifecycleErr, ok := err.(*Error); !ok || lifecycleErr.Code != CodeUpgradeRequired {
		t.Fatalf("expected CodeUpgradeRequired, got %v", err)
	}
	result, err := Reconcile(context.Background(), Options{
		Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, Approved: true, StopDaemon: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.Verified || result.BackupPath == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !result.CacheInvalidated {
		t.Fatal("incompatible projection cache was not invalidated")
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

// newUpgradableFixture creates a project whose baseline needs both
// automatic (project format, managed files, toolkit) and disruptive
// (personal-authority, draft-store) actions, with one draft present, so
// Inspect reports RequiresConfirmation and Reconcile has real work to do
// at every stage. It sets up isolated credential/config dirs and mirrors
// the setup in TestReconcileUpgradesBaselineWithoutChangingSignedEventsOrDrafts.
func newUpgradableFixture(t *testing.T) string {
	t.Helper()
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
	raw, _ = json.MarshalIndent(config, "", "  ")
	if err = os.WriteFile(configPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	authorityPath := runtimeinit.DatabasePath(root)
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
	return root
}

// driveJournalToStage performs the real stage-transition work functions
// (the same ones Reconcile itself calls) up through the transition INTO
// target, then returns a journal parked at that stage -- simulating a
// process that crashed immediately after the saveJournal call for that
// stage. config is mutated to reflect the state Reconcile would have
// produced by that point, matching what a fresh post-crash Inspect() would
// see on disk.
func driveJournalToStage(t *testing.T, root string, plan Plan, config store.Config, options Options, target string) journal {
	t.Helper()
	state, err := loadOrCreateJournal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if target == "prepared" {
		if err = saveJournal(root, state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	state.BackupPath, err = backupProject(root, config, state.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.Stage = "backed_up"
	if target == "backed_up" {
		if err = saveJournal(root, state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if err = migrateDatabases(context.Background(), root, config); err != nil {
		t.Fatal(err)
	}
	state.Stage = "databases_migrated"
	if target == "databases_migrated" {
		if err = saveJournal(root, state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	config.ProjectFormatVersion = store.ProjectFormatVersion
	config.ManagedFilesVersion = store.ManagedFilesVersion
	config.ToolkitVersion = options.Version
	config.ToolkitBuildID = options.BuildID
	config.MinimumToolkit = options.Version
	config.SchemaVersion = model.SchemaVersion
	config.ManagedFileHashes = store.ManagedHashes(config)
	if err = publishManagedFiles(root, config); err != nil {
		t.Fatal(err)
	}
	state.Stage = "files_published"
	if target == "files_published" {
		if err = saveJournal(root, state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	if _, err = invalidateCache(root, config, state.Plan); err != nil {
		t.Fatal(err)
	}
	state.Stage = "verified"
	if err = saveJournal(root, state); err != nil {
		t.Fatal(err)
	}
	return state
}

// countBackupsForID counts backup directories under root's backups
// directory belonging to journal id -- used to confirm a resumed
// Reconcile never creates a second, duplicate backup for the same
// interrupted run (finding 17).
func countBackupsForID(t *testing.T, root, id string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, store.Runtime, "backups"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), "-"+id) {
			count++
		}
	}
	return count
}

func TestReconcileResumesFromEachJournalStageWithoutDuplicatingWork(t *testing.T) {
	stages := []string{"prepared", "backed_up", "databases_migrated", "files_published", "verified"}
	for _, stage := range stages {
		stage := stage
		t.Run(stage, func(t *testing.T) {
			root := newUpgradableFixture(t)
			options := Options{Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, Approved: true, StopDaemon: false}
			plan, config, err := Inspect(root, options.Version, options.BuildID)
			if err != nil {
				t.Fatal(err)
			}
			if !plan.RequiresConfirmation {
				t.Fatal("expected fixture to require confirmation before it is driven to a resume point")
			}
			state := driveJournalToStage(t, root, plan, config, options, stage)

			result, err := Reconcile(context.Background(), options)
			if err != nil {
				t.Fatalf("resume from stage %q failed: %v", stage, err)
			}
			if !result.Verified {
				t.Fatalf("resume from stage %q: result not verified: %+v", stage, result)
			}
			if _, statErr := os.Stat(filepath.Join(root, store.Runtime, journalName)); !os.IsNotExist(statErr) {
				t.Fatalf("resume from stage %q: journal was not removed after success: %v", stage, statErr)
			}
			if got := countBackupsForID(t, root, state.ID); got != 1 {
				t.Fatalf("resume from stage %q: found %d backup dirs for id %s, want exactly 1 (no duplicate backup on resume)", stage, got, state.ID)
			}
			finalConfig, err := store.Open(root).ConfigStrict()
			if err != nil {
				t.Fatalf("resume from stage %q: canonical config not strict-decodable: %v", stage, err)
			}
			if finalConfig.ToolkitBuildID != "new-build" || finalConfig.ToolkitVersion != "0.2.0" {
				t.Fatalf("resume from stage %q: project not fully upgraded: %+v", stage, finalConfig)
			}
			drafts, err := draftstore.Open(runtimeinit.DraftPath(root))
			if err != nil {
				t.Fatalf("resume from stage %q: %v", stage, err)
			}
			defer drafts.Close()
			imported, err := drafts.Drafts(context.Background(), finalConfig.ProjectID, 10)
			if err != nil {
				t.Fatalf("resume from stage %q: %v", stage, err)
			}
			if len(imported) != 1 || imported[0].ID != "draft-1" {
				t.Fatalf("resume from stage %q: draft was not preserved: %+v", stage, imported)
			}
		})
	}
}

func TestReconcileRefusesUnrecognizedJournalStage(t *testing.T) {
	root := newUpgradableFixture(t)
	options := Options{Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, Approved: true, StopDaemon: false}
	plan, _, err := Inspect(root, options.Version, options.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := loadOrCreateJournal(plan)
	if err != nil {
		t.Fatal(err)
	}
	state.Stage = "some-future-stage-this-binary-does-not-understand"
	if err = saveJournal(root, state); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, store.Runtime, journalName)

	_, err = Reconcile(context.Background(), options)
	if err == nil {
		t.Fatal("expected Reconcile to refuse an unrecognized journal stage")
	}
	lifecycleErr, ok := err.(*Error)
	if !ok || lifecycleErr.Code != CodeUpgradeFailed {
		t.Fatalf("expected CodeUpgradeFailed, got %v", err)
	}
	if _, statErr := os.Stat(journalPath); statErr != nil {
		t.Fatalf("corrupt journal must survive for inspection, but got: %v", statErr)
	}
}

func TestReconcileRequiresApprovalForDisruptiveActions(t *testing.T) {
	root := newUpgradableFixture(t)
	options := Options{Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, Approved: false, StopDaemon: false}
	plan, _, err := Inspect(root, options.Version, options.BuildID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.RequiresConfirmation {
		t.Fatal("expected fixture to require confirmation")
	}
	_, err = Reconcile(context.Background(), options)
	if err == nil {
		t.Fatal("expected Reconcile to require approval before applying disruptive actions")
	}
	lifecycleErr, ok := err.(*Error)
	if !ok || lifecycleErr.Code != CodeUpgradeRequired {
		t.Fatalf("expected CodeUpgradeRequired, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, store.Runtime, journalName)); !os.IsNotExist(statErr) {
		t.Fatal("Reconcile must not create a journal (or start mutating) before approval is confirmed")
	}
}

func TestInspectRejectsProjectNewerThanRunningBinary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	store.RuntimeVersion = "0.5.0"
	store.RuntimeBuildID = "future-build"
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err := Inspect(root, "0.1.0", "old-build")
	if err == nil {
		t.Fatal("expected Inspect to reject a project whose minimum toolkit is newer than the running binary")
	}
	lifecycleErr, ok := err.(*Error)
	if !ok || lifecycleErr.Code != CodeProjectTooNew {
		t.Fatalf("expected CodeProjectTooNew, got %v", err)
	}
}

func TestInspectFailsOnCorruptConfig(t *testing.T) {
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
	configPath := filepath.Join(root, store.Runtime, "config.json")
	if err := os.WriteFile(configPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Inspect(root, "0.1.0", "current-build"); err == nil {
		t.Fatal("expected Inspect to fail on a malformed config.json")
	}
}

func TestConcurrentReconcileIsSerializedAndConsistent(t *testing.T) {
	root := newUpgradableFixture(t)
	options := Options{Root: root, Version: "0.2.0", BuildID: "new-build", Apply: true, Approved: true, Timeout: 5 * time.Second, StopDaemon: false}

	const workers = 4
	var wg sync.WaitGroup
	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, results[index] = Reconcile(context.Background(), options)
		}(i)
	}
	wg.Wait()
	for index, err := range results {
		if err != nil {
			t.Fatalf("concurrent Reconcile worker %d failed: %v", index, err)
		}
	}
	finalConfig, err := store.Open(root).ConfigStrict()
	if err != nil {
		t.Fatal(err)
	}
	if finalConfig.ToolkitBuildID != "new-build" {
		t.Fatalf("project not upgraded: %+v", finalConfig)
	}
	drafts, err := draftstore.Open(runtimeinit.DraftPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer drafts.Close()
	imported, err := drafts.Drafts(context.Background(), finalConfig.ProjectID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Fatalf("concurrent reconciliation duplicated or lost drafts: %+v", imported)
	}
	backupsDir := filepath.Join(root, store.Runtime, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("concurrent Reconcile calls raced into %d backups, want exactly 1: %v", len(entries), entries)
	}
}

// TestBackupProjectResumesOverAPartialBackupDirectory is a regression test
// for a bug live-verification caught in finding 17's own fix: reusing an
// existing backup directory by journal ID (rather than always creating a
// fresh one) means a resumed backupProject can land on a directory that
// already contains partial files from the crashed attempt --
// copyRegularFile opens each destination with O_EXCL (a deliberate
// TOCTOU/symlink-swap guard for a normal, single-shot backup), which fails
// with "file exists" against a stale partial file rather than silently
// succeeding over corrupt leftovers. backupProject must clear the reused
// directory before repopulating it.
func TestBackupProjectResumesOverAPartialBackupDirectory(t *testing.T) {
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
	config, err := store.Open(root).Config()
	if err != nil {
		t.Fatal(err)
	}

	const id = "partial-attempt"
	partialDir := filepath.Join(root, store.Runtime, "backups", "20200101T000000Z-"+id)
	if err = os.MkdirAll(filepath.Join(partialDir, store.Runtime), 0o700); err != nil {
		t.Fatal(err)
	}
	// Simulate exactly what a kill -9 mid-VACUUM/mid-copy leaves behind: a
	// destination file backupProject would also try to create, already
	// present (here deliberately corrupt/truncated) from the crashed run.
	if err = os.WriteFile(filepath.Join(partialDir, store.Runtime, "config.json"), []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}

	destination, err := backupProject(root, config, id)
	if err != nil {
		t.Fatalf("backupProject did not resume cleanly over a partial backup directory: %v", err)
	}
	if destination != partialDir {
		t.Fatalf("expected backupProject to reuse the existing directory %s, got %s", partialDir, destination)
	}
	raw, err := os.ReadFile(filepath.Join(destination, store.Runtime, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err = json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("expected the stale truncated config.json to be replaced with a real backup copy, got: %s", raw)
	}
}

func TestSQLiteHelpersRejectSymlinkedPaths(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.db")
	if err := os.WriteFile(real, []byte("not actually sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := sqliteVersion(link); err == nil {
		t.Fatal("sqliteVersion must refuse a symlinked path")
	}
	if err := setSQLiteVersion("test_component", link, 1); err == nil {
		t.Fatal("setSQLiteVersion must refuse a symlinked path")
	}
	if err := backupSQLite(link, filepath.Join(dir, "out.db")); err == nil {
		t.Fatal("backupSQLite must refuse a symlinked source path")
	}
	linkedOut := filepath.Join(dir, "linked-out.db")
	if err := os.Symlink(real, linkedOut); err != nil {
		t.Fatal(err)
	}
	if err := backupSQLite(real, linkedOut); err == nil {
		t.Fatal("backupSQLite must refuse a symlinked destination path")
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
