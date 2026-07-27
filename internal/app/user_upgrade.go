package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

const userLifecycleStateName = "lifecycle.json"

type userLifecycleState struct {
	BuildID      string    `json:"build_id"`
	RegistryHash string    `json:"registry_hash"`
	CompletedAt  time.Time `json:"completed_at"`
}

func (c *cli) reconcileUserInstallation(ctx context.Context, currentRoot string) error {
	roots, err := c.knownProjectRoots(currentRoot)
	if err != nil {
		return err
	}
	registryHash := projectRegistryHash(roots)
	currentState, err := loadUserLifecycleState()
	if err != nil {
		return err
	}
	buildID := buildinfo.ResolvedBuildID()
	if currentState.BuildID == buildID && currentState.RegistryHash == registryHash {
		return nil
	}

	type candidate struct {
		root string
		plan projectlifecycle.Plan
	}
	candidates := make([]candidate, 0, len(roots))
	for _, root := range roots {
		if !initializedProject(root) {
			continue
		}
		plan, _, inspectErr := projectlifecycle.Inspect(root, Version, buildID)
		if inspectErr != nil {
			return inspectErr
		}
		if plan.RequiresConfirmation {
			return &projectlifecycle.Error{
				Code:    projectlifecycle.CodeUpgradeRequired,
				Message: "the user-level installation has confirmation-required project migrations; run `agent-comms project upgrade --all-known`",
			}
		}
		candidates = append(candidates, candidate{root: root, plan: plan})
	}
	for _, candidate := range candidates {
		if len(candidate.plan.Actions) == 0 && !candidate.plan.Interrupted {
			continue
		}
		if _, err = projectlifecycle.Reconcile(ctx, projectlifecycle.Options{
			Root: candidate.root, Version: Version, BuildID: buildID,
			Apply: true, Timeout: c.timeout, StopDaemon: true,
		}); err != nil {
			return err
		}
	}
	return saveUserLifecycleState(userLifecycleState{
		BuildID: buildID, RegistryHash: registryHash, CompletedAt: time.Now().UTC(),
	})
}

func (c *cli) knownProjectRoots(currentRoot string) ([]string, error) {
	config, err := identity.LoadUserConfig()
	if err != nil {
		return nil, err
	}
	unique := map[string]struct{}{}
	for _, profile := range config.Profiles {
		if strings.TrimSpace(profile.ProjectRoot) == "" {
			continue
		}
		absolute, absoluteErr := filepath.Abs(profile.ProjectRoot)
		if absoluteErr != nil {
			return nil, absoluteErr
		}
		unique[filepath.Clean(absolute)] = struct{}{}
	}
	if currentRoot != "" && initializedProject(currentRoot) {
		absolute, absoluteErr := filepath.Abs(currentRoot)
		if absoluteErr != nil {
			return nil, absoluteErr
		}
		unique[filepath.Clean(absolute)] = struct{}{}
	}
	roots := make([]string, 0, len(unique))
	for root := range unique {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots, nil
}

func initializedProject(root string) bool {
	info, err := os.Lstat(filepath.Join(root, store.Runtime))
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func projectRegistryHash(roots []string) string {
	sum := sha256.Sum256([]byte(strings.Join(roots, "\x00")))
	return hex.EncodeToString(sum[:])
}

func loadUserLifecycleState() (userLifecycleState, error) {
	configDirectory, err := identity.ConfigDir()
	if err != nil {
		return userLifecycleState{}, err
	}
	raw, err := os.ReadFile(filepath.Join(configDirectory, userLifecycleStateName))
	if os.IsNotExist(err) {
		return userLifecycleState{}, nil
	}
	if err != nil {
		return userLifecycleState{}, err
	}
	var state userLifecycleState
	if err = json.Unmarshal(raw, &state); err != nil {
		return userLifecycleState{}, err
	}
	return state, nil
}

func saveUserLifecycleState(state userLifecycleState) error {
	configDirectory, err := identity.ConfigDir()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(configDirectory, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(configDirectory, userLifecycleStateName)
	temporary, err := os.CreateTemp(configDirectory, ".lifecycle-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(append(raw, '\n'))
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
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(configDirectory)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func markUserInstallationCurrent(currentRoot string) error {
	config := &cli{}
	roots, err := config.knownProjectRoots(currentRoot)
	if err != nil {
		return err
	}
	return saveUserLifecycleState(userLifecycleState{
		BuildID: buildinfo.ResolvedBuildID(), RegistryHash: projectRegistryHash(roots),
		CompletedAt: time.Now().UTC(),
	})
}
