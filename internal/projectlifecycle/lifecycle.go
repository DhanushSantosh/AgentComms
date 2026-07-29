package projectlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/draftstore"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const (
	PersonalAuthoritySchemaVersion = 1
	ProjectionCacheSchemaVersion   = 3
	DraftStoreSchemaVersion        = 1
	journalName                    = "upgrade-state.json"
)

var errLifecycleLocked = errors.New("project lifecycle is locked by another process")

type ErrorCode string

const (
	CodeUpgradeRequired    ErrorCode = "UPGRADE_REQUIRED"
	CodeProjectTooNew      ErrorCode = "PROJECT_TOO_NEW"
	CodeUpgradeUnsupported ErrorCode = "UPGRADE_UNSUPPORTED"
	CodeUpgradeFailed      ErrorCode = "UPGRADE_FAILED"
	CodeConflict           ErrorCode = "CONFLICT"
)

type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return e.Message }

type Action struct {
	Component string `json:"component"`
	From      string `json:"from"`
	To        string `json:"to"`
	Operation string `json:"operation"`
	Automatic bool   `json:"automatic"`
}

type Plan struct {
	ProjectRoot          string   `json:"project_root"`
	ProjectID            string   `json:"project_id"`
	CurrentBuildID       string   `json:"current_build_id,omitempty"`
	TargetBuildID        string   `json:"target_build_id"`
	Actions              []Action `json:"actions"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	Interrupted          bool     `json:"interrupted"`
}

type Result struct {
	Plan             Plan   `json:"plan"`
	Changed          bool   `json:"changed"`
	Resumed          bool   `json:"resumed"`
	BackupPath       string `json:"backup_path,omitempty"`
	DaemonStopped    bool   `json:"daemon_stopped"`
	CacheInvalidated bool   `json:"cache_invalidated"`
	Verified         bool   `json:"verified"`
}

type Options struct {
	Root       string
	Version    string
	BuildID    string
	Apply      bool
	Approved   bool
	Timeout    time.Duration
	StopDaemon bool
}

type journal struct {
	ID         string    `json:"id"`
	Stage      string    `json:"stage"`
	StartedAt  time.Time `json:"started_at"`
	BackupPath string    `json:"backup_path,omitempty"`
	Plan       Plan      `json:"plan"`
}

func Inspect(root, version, buildID string) (Plan, store.Config, error) {
	root, err := cleanProjectRoot(root)
	if err != nil {
		return Plan{}, store.Config{}, err
	}
	raw, err := os.ReadFile(filepath.Join(root, store.Runtime, "config.json"))
	if err != nil {
		return Plan{}, store.Config{}, err
	}
	var config store.Config
	if err = json.Unmarshal(raw, &config); err != nil {
		return Plan{}, store.Config{}, fmt.Errorf("decode project manifest: %w", err)
	}
	if config.RuntimeMode != "personal" && config.RuntimeMode != "service" {
		// A typed error here matters specifically because this is exactly
		// the shape a project stuck on a removed legacy runtime mode
		// takes (the legacy runtime/migration paths were removed in an
		// earlier refactor this project went through) -- an untyped error
		// is invisible to internal/failure's classifier and to the
		// per-project isolation in reconcileUserInstallation, which needs
		// to tell "this one project is unsupported" apart from a generic
		// failure to decide whether to hard-fail or just warn and skip.
		return Plan{}, store.Config{}, &Error{Code: CodeUpgradeUnsupported, Message: fmt.Sprintf(
			"unsupported runtime mode %q", config.RuntimeMode)}
	}
	plan := Plan{
		ProjectRoot: root, ProjectID: config.ProjectID,
		CurrentBuildID: config.ToolkitBuildID, TargetBuildID: buildID,
	}
	if config.MinimumToolkit != "" && versionOlder(version, config.MinimumToolkit) {
		return plan, config, &Error{Code: CodeProjectTooNew, Message: fmt.Sprintf(
			"project requires toolkit %s or newer, running %s", config.MinimumToolkit, version)}
	}
	if config.ProjectFormatVersion > store.ProjectFormatVersion {
		return plan, config, &Error{Code: CodeProjectTooNew, Message: fmt.Sprintf(
			"project format %d is newer than supported format %d",
			config.ProjectFormatVersion, store.ProjectFormatVersion)}
	}
	if config.ProjectFormatVersion < 0 {
		return plan, config, &Error{Code: CodeUpgradeUnsupported, Message: "project format is invalid"}
	}
	if config.ProjectFormatVersion < store.ProjectFormatVersion {
		plan.Actions = append(plan.Actions, Action{
			Component: "project_format", From: strconv.Itoa(config.ProjectFormatVersion),
			To: strconv.Itoa(store.ProjectFormatVersion), Operation: "canonicalize manifest", Automatic: true,
		})
	}
	if config.ManagedFilesVersion > store.ManagedFilesVersion {
		return plan, config, &Error{Code: CodeProjectTooNew, Message: fmt.Sprintf(
			"managed-file format %d is newer than supported format %d",
			config.ManagedFilesVersion, store.ManagedFilesVersion)}
	}
	if config.ManagedFilesVersion < store.ManagedFilesVersion || !managedFilesCurrent(root, config) {
		plan.Actions = append(plan.Actions, Action{
			Component: "managed_files", From: strconv.Itoa(config.ManagedFilesVersion),
			To: strconv.Itoa(store.ManagedFilesVersion), Operation: "backup drift and publish canonical files", Automatic: true,
		})
	}
	if config.ToolkitVersion != version || config.ToolkitBuildID != buildID {
		plan.Actions = append(plan.Actions, Action{
			Component: "toolkit", From: config.ToolkitVersion + "+" + config.ToolkitBuildID,
			To: version + "+" + buildID, Operation: "record installed build", Automatic: true,
		})
	}
	databaseVersions, versionErr := inspectDatabases(root, config)
	if versionErr != nil {
		return plan, config, versionErr
	}
	for _, databaseVersion := range databaseVersions {
		if databaseVersion.current > databaseVersion.target {
			return plan, config, &Error{Code: CodeProjectTooNew, Message: fmt.Sprintf(
				"%s schema %d is newer than supported schema %d",
				databaseVersion.component, databaseVersion.current, databaseVersion.target)}
		}
		if databaseVersion.current < databaseVersion.target {
			plan.Actions = append(plan.Actions, Action{
				Component: databaseVersion.component, From: strconv.Itoa(databaseVersion.current),
				To: strconv.Itoa(databaseVersion.target), Operation: databaseVersion.operation, Automatic: databaseVersion.automatic,
			})
		}
	}
	_, journalErr := os.Stat(filepath.Join(root, store.Runtime, journalName))
	plan.Interrupted = journalErr == nil
	if journalErr != nil && !os.IsNotExist(journalErr) {
		return plan, config, journalErr
	}
	sort.SliceStable(plan.Actions, func(left, right int) bool {
		return plan.Actions[left].Component < plan.Actions[right].Component
	})
	for _, action := range plan.Actions {
		plan.RequiresConfirmation = plan.RequiresConfirmation || !action.Automatic
	}
	return plan, config, nil
}

// versionOlder reports whether toolkit version a is older than b, comparing
// dotted numeric segments (e.g. "0.1.0" vs "0.2.0"). A non-numeric segment
// compares as 0 rather than erroring, so an unexpected version string never
// panics or blocks an upgrade -- it just can't win a comparison it can't
// parse.
func versionOlder(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}

func Reconcile(ctx context.Context, options Options) (Result, error) {
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	plan, config, err := Inspect(options.Root, options.Version, options.BuildID)
	if err != nil {
		return Result{}, err
	}
	result := Result{Plan: plan, Resumed: plan.Interrupted}
	if !options.Apply || len(plan.Actions) == 0 && !plan.Interrupted {
		result.Verified = len(plan.Actions) == 0 && !plan.Interrupted
		return result, nil
	}
	if plan.RequiresConfirmation && !options.Approved {
		return result, &Error{Code: CodeUpgradeRequired, Message: "project upgrade requires explicit confirmation"}
	}
	lock, err := lockFile(filepath.Join(plan.ProjectRoot, store.Runtime, "upgrade.lock"), options.Timeout)
	if err != nil {
		if errors.Is(err, errLifecycleLocked) {
			return result, &Error{Code: CodeConflict, Message: err.Error()}
		}
		return result, err
	}
	defer unlockFile(lock)
	plan, config, err = Inspect(options.Root, options.Version, options.BuildID)
	if err != nil {
		return result, err
	}
	result.Plan = plan
	// Re-check confirmation against the freshly re-Inspect()'d, post-lock
	// plan, not just the pre-lock one checked above: on-disk state could
	// change during the lock-wait window (e.g. a concurrent process
	// advances a database) such that a new disruptive action appears.
	// Checking only the stale pre-lock plan would let that slip through
	// unconfirmed.
	if plan.RequiresConfirmation && !options.Approved {
		return result, &Error{Code: CodeUpgradeRequired, Message: "project upgrade requires explicit confirmation"}
	}
	if len(plan.Actions) == 0 && !plan.Interrupted {
		result.Verified = true
		return result, nil
	}
	if options.StopDaemon {
		result.DaemonStopped, err = stopDaemon(ctx, config)
		if err != nil {
			return result, upgradeFailed("stop existing daemon", err)
		}
	}
	state, err := loadOrCreateJournal(plan)
	if err != nil {
		return result, upgradeFailed("create reconciliation journal", err)
	}
	switch state.Stage {
	case "prepared", "backed_up", "databases_migrated", "files_published", "verified":
		// known stage; fall through to the resume cascade below.
	default:
		// Refuse to proceed rather than silently skipping every remaining
		// stage. Critically: return here, before the final Inspect()+
		// journal-deletion below, so a corrupt or unrecognized journal
		// never reports false success and the journal itself survives for
		// a human to inspect or use to restore the last completed backup.
		return result, &Error{Code: CodeUpgradeFailed, Message: fmt.Sprintf(
			"reconciliation journal has unrecognized stage %q (%s); refusing to proceed without deleting it -- "+
				"inspect %s, or restore the most recent completed backup under %s and delete the journal manually",
			state.Stage, journalBuildMismatchHint(state.Plan.TargetBuildID, options.BuildID),
			filepath.Join(plan.ProjectRoot, store.Runtime, journalName),
			filepath.Join(plan.ProjectRoot, store.Runtime, "backups"))}
	}
	if state.Stage == "prepared" {
		state.BackupPath, err = backupProject(plan.ProjectRoot, config, state.ID)
		if err != nil {
			return result, upgradeFailed("back up project", err)
		}
		state.Stage = "backed_up"
		if err = saveJournal(plan.ProjectRoot, state); err != nil {
			return result, upgradeFailed("record backup", err)
		}
	}
	result.BackupPath = state.BackupPath
	if state.Stage == "backed_up" {
		if err = migrateDatabases(ctx, plan.ProjectRoot, config); err != nil {
			return result, upgradeFailed("migrate project databases", err)
		}
		state.Stage = "databases_migrated"
		if err = saveJournal(plan.ProjectRoot, state); err != nil {
			return result, upgradeFailed("record database migration", err)
		}
	}
	if state.Stage == "databases_migrated" {
		config.ProjectFormatVersion = store.ProjectFormatVersion
		config.ManagedFilesVersion = store.ManagedFilesVersion
		config.ToolkitVersion = options.Version
		config.ToolkitBuildID = options.BuildID
		config.MinimumToolkit = options.Version
		config.SchemaVersion = model.SchemaVersion
		config.ManagedFileHashes = store.ManagedHashes(config)
		if err = publishManagedFiles(plan.ProjectRoot, config); err != nil {
			return result, upgradeFailed("publish managed files", err)
		}
		state.Stage = "files_published"
		if err = saveJournal(plan.ProjectRoot, state); err != nil {
			return result, upgradeFailed("record managed files", err)
		}
	}
	if state.Stage == "files_published" {
		result.CacheInvalidated, err = invalidateCache(plan.ProjectRoot, config, state.Plan)
		if err != nil {
			return result, upgradeFailed("invalidate projection cache", err)
		}
		state.Stage = "verified"
		if err = saveJournal(plan.ProjectRoot, state); err != nil {
			return result, upgradeFailed("record verification", err)
		}
	}
	if _, _, err = Inspect(plan.ProjectRoot, options.Version, options.BuildID); err != nil {
		return result, upgradeFailed("verify reconciled project", err)
	}
	if err = os.Remove(filepath.Join(plan.ProjectRoot, store.Runtime, journalName)); err != nil && !os.IsNotExist(err) {
		return result, upgradeFailed("complete reconciliation", err)
	}
	result.Changed = true
	result.Verified = true
	return result, nil
}

type databaseVersion struct {
	component string
	current   int
	target    int
	operation string
	// automatic is set per migration step, not per database: a database
	// component may gain a genuinely disruptive migration in the future
	// while an earlier step on the same component stays safe. Only the
	// projection cache defaults to automatic today -- it is explicitly
	// disposable and rebuilt, never backed up, per the project's own
	// design (docs/rfcs/0011-managed-project-lifecycle-and-upgrades.md).
	// personal_authority (signing/trust state) and draft_store (unique
	// user data) default to disruptive until a specific future migration
	// step is individually reviewed and proven safe enough to flip.
	automatic bool
}

func inspectDatabases(root string, config store.Config) ([]databaseVersion, error) {
	result := []databaseVersion{}
	if config.RuntimeMode == "personal" {
		version, err := sqliteVersion(filepath.Join(root, store.Runtime, "cache", "personal-authority.db"))
		if err != nil {
			return nil, err
		}
		result = append(result, databaseVersion{"personal_authority", version, PersonalAuthoritySchemaVersion, "apply transactional schema migrations", false})
	}
	cachePath, pathErr := projectionPath(root, config)
	if pathErr != nil {
		return nil, pathErr
	}
	if cachePath != "" {
		version, err := sqliteVersion(cachePath)
		if err != nil {
			return nil, err
		}
		if version == 0 && config.ProjectFormatVersion == store.ProjectFormatVersion {
			if _, statErr := os.Stat(cachePath); os.IsNotExist(statErr) {
				version = ProjectionCacheSchemaVersion
			}
		}
		result = append(result, databaseVersion{"projection_cache", version, ProjectionCacheSchemaVersion, "mark cache for rebuild", true})
	}
	draftPath := filepath.Join(root, store.Runtime, "data", "drafts.db")
	draftVersion, err := sqliteVersion(draftPath)
	if err != nil {
		return nil, err
	}
	if draftVersion == 0 {
		if _, statErr := os.Stat(draftPath); os.IsNotExist(statErr) {
			// drafts.db doesn't exist yet -- only treat this as "nothing to
			// migrate" if the old projection cache genuinely has no
			// un-migrated draft rows sitting in it. Relying only on
			// config.ProjectFormatVersion already looking current (the
			// previous check) is a proxy that can be fooled: drafts.db
			// could be missing for a reason other than "already migrated"
			// (deleted, an incomplete restore, runtime state not copied to
			// a new machine) while the manifest still reports current
			// format, silently orphaning real drafts forever.
			hasUnmigrated, draftsErr := cacheHasUnmigratedDrafts(cachePath)
			if draftsErr != nil {
				return nil, draftsErr
			}
			if !hasUnmigrated {
				draftVersion = DraftStoreSchemaVersion
			}
		} else if statErr != nil {
			return nil, statErr
		}
	}
	result = append(result, databaseVersion{"draft_store", draftVersion, DraftStoreSchemaVersion, "preserve drafts in durable storage", false})
	return result, nil
}

// sqliteOpen opens a SQLite database with a busy timeout, so a connection
// that finds another connection mid-transaction on the same file waits
// briefly and retries instead of failing immediately with SQLITE_BUSY.
// This matters specifically because Reconcile's own concurrency guard --
// the process-level upgrade.lock file -- only serializes the WRITE stages;
// Inspect's read-only version checks run both before that lock is taken
// and from unrelated commands (any `agent-comms` invocation reconciles the
// project it targets via PersistentPreRunE), so two ordinary commands
// running at the same time can legitimately open the same SQLite file
// concurrently.
func sqliteOpen(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)")
}

// cacheHasUnmigratedDrafts reports whether the old projection cache at
// cachePath still has draft rows that haven't been copied into the
// dedicated draft store yet. A missing cache, or a cache with no drafts
// table or zero rows, means there is genuinely nothing to migrate.
func cacheHasUnmigratedDrafts(cachePath string) (bool, error) {
	if cachePath == "" {
		return false, nil
	}
	if err := rejectSymlinkPath(cachePath); err != nil {
		return false, err
	}
	if _, statErr := os.Stat(cachePath); os.IsNotExist(statErr) {
		return false, nil
	} else if statErr != nil {
		return false, statErr
	}
	db, err := sqliteOpen(cachePath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var tableExists int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='drafts'`).Scan(&tableExists); err != nil {
		return false, err
	}
	if tableExists == 0 {
		return false, nil
	}
	var draftCount int
	if err = db.QueryRow(`SELECT COUNT(*) FROM drafts`).Scan(&draftCount); err != nil {
		return false, err
	}
	return draftCount > 0, nil
}

// rejectSymlinkPath returns an error if path exists and is a symlink. A
// missing path is not an error -- callers create it fresh in that case.
// Guards every SQLite path this package opens or writes against a symlink
// swap (e.g. .agent-comms/cache/personal-authority.db replaced with a
// symlink pointing at a different project's database), the same class of
// check cleanProjectRoot/managedFilesCurrent/copyRegularFile already apply
// to the project root and managed files -- SQLite paths need the identical
// treatment, not just plain files.
func rejectSymlinkPath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink", path)
	}
	return nil
}

func sqliteVersion(path string) (int, error) {
	if err := rejectSymlinkPath(path); err != nil {
		return 0, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	db, err := sqliteOpen(path)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func migrateDatabases(ctx context.Context, root string, config store.Config) error {
	if config.RuntimeMode == "personal" {
		path := filepath.Join(root, store.Runtime, "cache", "personal-authority.db")
		if err := setSQLiteVersion("personal_authority", path, PersonalAuthoritySchemaVersion); err != nil {
			return err
		}
	}
	if err := migrateDrafts(ctx, root, config); err != nil {
		return err
	}
	cachePath, err := projectionPath(root, config)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(cachePath); statErr == nil {
		if err = setSQLiteVersion("projection_cache", cachePath, ProjectionCacheSchemaVersion); err != nil {
			return err
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	return nil
}

func migrateDrafts(ctx context.Context, root string, config store.Config) error {
	destination := filepath.Join(root, store.Runtime, "data", "drafts.db")
	if err := rejectSymlinkPath(destination); err != nil {
		return err
	}
	storeInstance, err := draftstore.Open(destination)
	if err != nil {
		return err
	}
	defer storeInstance.Close()
	cachePath, err := projectionPath(root, config)
	if err != nil {
		return err
	}
	if err = rejectSymlinkPath(cachePath); err != nil {
		return err
	}
	if _, err = os.Stat(cachePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	cache, err := sqliteOpen(cachePath)
	if err != nil {
		return err
	}
	defer cache.Close()
	var exists int
	if err = cache.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='drafts'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	rows, err := cache.Query(`SELECT project_id,draft_id,kind,body,created_at,updated_at FROM drafts`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var draft controlplane.Draft
		var created, updated string
		if err = rows.Scan(&draft.ProjectID, &draft.ID, &draft.Kind, &draft.Body, &created, &updated); err != nil {
			return err
		}
		draft.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return err
		}
		draft.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return err
		}
		if err = storeInstance.ImportDraft(ctx, draft); err != nil {
			return err
		}
	}
	return rows.Err()
}

func projectionPath(root string, config store.Config) (string, error) {
	if config.RuntimeMode == "personal" {
		return filepath.Join(root, store.Runtime, "cache", "personal-projection.db"), nil
	}
	if configured := strings.TrimSpace(os.Getenv("AGENT_COMMS_CACHE_PATH")); configured != "" {
		return configured, nil
	}
	configDirectory, err := identity.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirectory, "cache.db"), nil
}

// sqliteMigrationKey identifies one (component, fromVersion) migration
// step. sqliteMigrations is the real place a future genuine schema change
// adds transformation logic. Today every existing version bump has no
// actual DDL delta -- the embedded CREATE TABLE IF NOT EXISTS schema
// already produces the target shape, and the bump only exists to stamp
// pre-versioning databases -- so no migration is registered yet, and
// setSQLiteVersion's stamp is correct as a no-op transformation. Without
// this slot, a future real schema change would have nowhere to put
// transformation logic other than blindly stamping user_version over
// un-migrated data.
type sqliteMigrationKey struct {
	component string
	from      int
}

var sqliteMigrations = map[sqliteMigrationKey]func(*sql.Tx) error{}

func setSQLiteVersion(component, path string, target int) error {
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	db, err := sqliteOpen(path)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRow(`PRAGMA user_version`).Scan(&current); err != nil {
		return err
	}
	if current > target {
		return &Error{Code: CodeProjectTooNew, Message: "database schema is newer than this binary"}
	}
	for version := current; version < target; version++ {
		if migrate, ok := sqliteMigrations[sqliteMigrationKey{component, version}]; ok {
			if err = migrate(tx); err != nil {
				return fmt.Errorf("migrate %s from schema %d: %w", component, version, err)
			}
		}
	}
	if _, err = tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", target)); err != nil {
		return err
	}
	return tx.Commit()
}

func managedFilesCurrent(root string, config store.Config) bool {
	expected := store.ManagedFiles(config)
	hashes := store.ManagedHashes(config)
	if config.ManagedFilesVersion != store.ManagedFilesVersion || len(config.ManagedFileHashes) != len(hashes) {
		return false
	}
	for relative, content := range expected {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, content) {
			return false
		}
		if config.ManagedFileHashes[filepath.ToSlash(relative)] != hashes[filepath.ToSlash(relative)] {
			return false
		}
	}
	return true
}

func cleanProjectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("project root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	runtimePath := filepath.Join(absolute, store.Runtime)
	info, err := os.Lstat(runtimePath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("project runtime must be a real directory")
	}
	return absolute, nil
}

func loadOrCreateJournal(plan Plan) (journal, error) {
	path := filepath.Join(plan.ProjectRoot, store.Runtime, journalName)
	raw, err := os.ReadFile(path)
	if err == nil {
		var existing journal
		if decodeErr := json.Unmarshal(raw, &existing); decodeErr != nil {
			return journal{}, decodeErr
		}
		return existing, nil
	}
	if !os.IsNotExist(err) {
		return journal{}, err
	}
	state := journal{ID: uuid.NewString(), Stage: "prepared", StartedAt: time.Now().UTC(), Plan: plan}
	return state, saveJournal(plan.ProjectRoot, state)
}

func saveJournal(root string, state journal) error {
	return writeJSONAtomic(filepath.Join(root, store.Runtime, journalName), state, 0o600)
}

// existingBackup returns the path of a previously created backup directory
// for this journal ID, if one exists, so a Reconcile retry that lands back
// on the "prepared" stage (e.g. after a crash partway through backupProject
// itself, before the journal ever advances to "backed_up") reuses and
// overwrites it instead of leaving an orphaned partial backup behind and
// creating a fresh one on every retry.
func existingBackup(backupsDir, id string) (string, error) {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	suffix := "-" + id
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(backupsDir, entry.Name()), nil
		}
	}
	return "", nil
}

func backupProject(root string, config store.Config, id string) (string, error) {
	backupsDir := filepath.Join(root, store.Runtime, "backups")
	destination, err := existingBackup(backupsDir, id)
	if err != nil {
		return "", err
	}
	if destination == "" {
		name := time.Now().UTC().Format("20060102T150405Z") + "-" + id
		destination = filepath.Join(backupsDir, name)
	} else {
		// Reusing a directory from an interrupted attempt: wipe whatever
		// partial content it holds first. copyRegularFile below opens each
		// destination file with O_EXCL (a deliberate protection against a
		// symlink-swap TOCTOU on a normal, single-shot backup), which would
		// otherwise fail with "file exists" against a file a prior,
		// crashed attempt already created -- live-verified failure mode,
		// not theoretical.
		if err := os.RemoveAll(destination); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return "", err
	}
	files := []string{filepath.Join(store.Runtime, "config.json")}
	for relative := range store.ManagedFiles(config) {
		files = append(files, relative)
	}
	for _, relative := range files {
		source := filepath.Join(root, relative)
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		target := filepath.Join(destination, filepath.FromSlash(filepath.ToSlash(relative)))
		if err := copyRegularFile(source, target); err != nil {
			return "", err
		}
	}
	if config.RuntimeMode == "personal" {
		source := filepath.Join(root, store.Runtime, "cache", "personal-authority.db")
		target := filepath.Join(destination, "personal-authority.db")
		if err := backupSQLite(source, target); err != nil {
			return "", err
		}
	}
	draftPath := filepath.Join(root, store.Runtime, "data", "drafts.db")
	if _, err := os.Stat(draftPath); err == nil {
		if err = backupSQLite(draftPath, filepath.Join(destination, "drafts.db")); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	cachePath, pathErr := projectionPath(root, config)
	if pathErr != nil {
		return "", pathErr
	}
	if _, err := os.Stat(draftPath); os.IsNotExist(err) {
		if _, cacheErr := os.Stat(cachePath); cacheErr == nil {
			if err = backupSQLite(cachePath, filepath.Join(destination, "legacy-projection-with-drafts.db")); err != nil {
				return "", err
			}
		} else if !os.IsNotExist(cacheErr) {
			return "", cacheErr
		}
	}
	return destination, pruneBackups(filepath.Join(root, store.Runtime, "backups"), destination)
}

func backupSQLite(source, destination string) error {
	if err := rejectSymlinkPath(source); err != nil {
		return err
	}
	if err := rejectSymlinkPath(destination); err != nil {
		return err
	}
	db, err := sqliteOpen(source)
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err = db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return fmt.Errorf("SQLite quick_check: %s", check)
	}
	escaped := strings.ReplaceAll(destination, "'", "''")
	if _, err = db.Exec(`VACUUM INTO '` + escaped + `'`); err != nil {
		return err
	}
	// VACUUM INTO creates its output via SQLite's own open(), subject to
	// process umask -- explicitly harden it rather than trusting the
	// umask to produce 0600 for a file that may contain signed history.
	return os.Chmod(destination, 0o600)
}

func copyRegularFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to copy non-regular file %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func pruneBackups(directory, current string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type backup struct {
		path string
		time time.Time
	}
	backups := make([]backup, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		backups = append(backups, backup{filepath.Join(directory, entry.Name()), info.ModTime()})
	}
	sort.Slice(backups, func(left, right int) bool { return backups[left].time.After(backups[right].time) })
	for index := 3; index < len(backups); index++ {
		if backups[index].path == current {
			continue
		}
		if err = os.RemoveAll(backups[index].path); err != nil {
			return err
		}
	}
	return nil
}

func publishManagedFiles(root string, config store.Config) error {
	for relative, content := range store.ManagedFiles(config) {
		mode := os.FileMode(0o644)
		if err := writeAtomic(filepath.Join(root, relative), content, mode); err != nil {
			return err
		}
	}
	return writeJSONAtomic(filepath.Join(root, store.Runtime, "config.json"), config, 0o600)
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(raw, '\n'), mode)
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to replace symlink %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".upgrade-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(mode); err == nil {
		_, err = temporary.Write(content)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryName, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func invalidateCache(root string, config store.Config, plan Plan) (bool, error) {
	rebuild := false
	for _, action := range plan.Actions {
		if action.Component == "projection_cache" {
			rebuild = true
			break
		}
	}
	if !rebuild {
		return false, nil
	}
	path, err := projectionPath(root, config)
	if err != nil {
		return false, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err = os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	return true, nil
}

func stopDaemon(ctx context.Context, config store.Config) (bool, error) {
	client, err := daemonclient.New(config.DaemonEndpoint, time.Second)
	if err != nil {
		return false, err
	}
	healthCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, healthErr := client.Health(healthCtx)
	cancel()
	if healthErr != nil {
		return false, nil
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	err = client.Shutdown(shutdownCtx)
	shutdownCancel()
	if err != nil {
		return false, err
	}
	for attempt := 0; attempt < 50; attempt++ {
		probeCtx, probeCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_, probeErr := client.Health(probeCtx)
		probeCancel()
		if probeErr != nil {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false, errors.New("daemon did not stop before the upgrade deadline")
}

func upgradeFailed(operation string, err error) error {
	var lifecycleError *Error
	if errors.As(err, &lifecycleError) {
		return err
	}
	return &Error{Code: CodeUpgradeFailed, Message: operation + ": " + err.Error()}
}

// journalBuildMismatchHint phrases an unrecognized-journal-stage error as
// version skew (a different binary build wrote this journal) rather than
// generic corruption, when that's distinguishable from the journal's own
// recorded target build.
func journalBuildMismatchHint(journalTargetBuildID, runningBuildID string) string {
	if journalTargetBuildID != "" && journalTargetBuildID != runningBuildID {
		return fmt.Sprintf("this journal targeted build %s, this binary is build %s", journalTargetBuildID, runningBuildID)
	}
	return "this may be disk corruption or a manually edited journal file"
}

func FileHash(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:]), nil
}
