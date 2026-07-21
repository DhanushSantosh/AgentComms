package personalmigration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

const AuthorityActor = "__personal_authority__"

type Result struct {
	ProjectID      string                `json:"project_id"`
	ImportedEvents uint64                `json:"imported_events"`
	ProjectionHash string                `json:"projection_hash"`
	State          string                `json:"state"`
	Receipt        *controlplane.Receipt `json:"receipt"`
}

func DatabasePath(projectRoot string) string {
	return filepath.Join(projectRoot, store.Runtime, "cache", "personal-authority.db")
}

func ProjectionPath(projectRoot string) string {
	return filepath.Join(projectRoot, store.Runtime, "cache", "personal-projection.db")
}

func DaemonEndpoint(projectRoot, projectID string) string {
	if runtime.GOOS == "windows" {
		return `\\.\pipe\agent-comms-` + projectID
	}
	return filepath.Join(projectRoot, store.Runtime, "cache", "daemon.sock")
}

func Migrate(ctx context.Context, projectStore *store.Store) (Result, error) {
	if projectStore == nil {
		return Result{}, errors.New("project store is required")
	}
	if err := projectStore.Verify(); err != nil {
		return Result{}, fmt.Errorf("verify legacy runtime: %w", err)
	}
	config, err := projectStore.Config()
	if err != nil {
		return Result{}, err
	}
	if config.RuntimeMode == "personal" {
		return Result{}, errors.New("project is already in personal mode")
	}
	if config.RuntimeMode == "service" || config.LegacyReadOnly {
		return Result{}, errors.New("project is not an active legacy runtime")
	}
	events, err := projectStore.Events()
	if err != nil {
		return Result{}, err
	}
	if len(events) == 0 {
		return Result{}, errors.New("legacy runtime has no events")
	}
	legacyState, err := service.New(projectStore.Root).State()
	if err != nil {
		return Result{}, err
	}
	expectedProjection := service.ProjectionHash(legacyState)
	credential, err := identity.Generate(config.ProjectID, AuthorityActor)
	if err != nil {
		return Result{}, err
	}
	if err = projectStore.Credentials.Put(credential); err != nil {
		return Result{}, fmt.Errorf("store personal authority key: %w", err)
	}
	keepCredential := false
	defer func() {
		if !keepCredential {
			_ = projectStore.Credentials.Delete(config.ProjectID, AuthorityActor)
		}
	}()
	signer, err := controlplane.NewSigner(credential.PrivateKey)
	if err != nil {
		return Result{}, err
	}
	databasePath := DatabasePath(projectStore.Root)
	temporaryPath := databasePath + ".import"
	if err = removeImportFiles(temporaryPath); err != nil {
		return Result{}, err
	}
	engine, err := personalauthority.Open(temporaryPath, signer)
	if err != nil {
		return Result{}, err
	}
	cleanupEngine := true
	defer func() {
		if cleanupEngine {
			_ = engine.Close()
		}
		_ = removeImportFiles(temporaryPath)
	}()
	if err = engine.CreateProject(ctx, config.ProjectID, config.Owner); err != nil {
		return Result{}, err
	}
	receipt, err := engine.ImportLegacy(ctx, config.ProjectID, events)
	if err != nil {
		return Result{}, err
	}
	importedState, _, err := engine.State(ctx, config.ProjectID)
	if err != nil {
		return Result{}, err
	}
	actualProjection := service.ProjectionHash(importedState)
	if actualProjection != expectedProjection {
		return Result{}, fmt.Errorf("personal projection mismatch: got %s, want %s", actualProjection, expectedProjection)
	}
	if err = engine.Close(); err != nil {
		return Result{}, err
	}
	cleanupEngine = false
	if err = os.Rename(temporaryPath, databasePath); err != nil {
		return Result{}, fmt.Errorf("publish personal authority database: %w", err)
	}
	endpoint := DaemonEndpoint(projectStore.Root, config.ProjectID)
	if err = projectStore.ActivatePersonalMode(signer.PublicKey(), endpoint, receipt); err != nil {
		_ = removeImportFiles(databasePath)
		return Result{}, fmt.Errorf("personal import succeeded but cutover failed: %w", err)
	}
	keepCredential = true
	return Result{
		ProjectID: config.ProjectID, ImportedEvents: uint64(len(events)),
		ProjectionHash: actualProjection, State: "ACTIVATED", Receipt: receipt,
	}, nil
}

func removeImportFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(candidate); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
