package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func TestUserReconciliationUpgradesEveryRegisteredProjectOncePerBuild(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	store.RuntimeVersion = Version
	store.RuntimeBuildID = "previous-build"
	roots := []string{t.TempDir(), t.TempDir()}
	for _, root := range roots {
		if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
			ProjectRoot: root, Owner: "owner", Mode: "personal",
		}); err != nil {
			t.Fatal(err)
		}
		downgradeProjectBuild(t, root)
	}

	client := &cli{timeout: time.Second}
	if warnings, err := client.reconcileUserInstallation(context.Background(), roots[0]); err != nil {
		t.Fatal(err)
	} else if len(warnings) != 0 {
		t.Fatalf("expected no warnings on a clean reconciliation, got: %v", warnings)
	}
	for _, root := range roots {
		config, err := store.Open(root).ConfigStrict()
		if err != nil {
			t.Fatal(err)
		}
		if config.ToolkitBuildID != buildinfo.ResolvedBuildID() {
			t.Fatalf("project %s build=%s, want %s", root, config.ToolkitBuildID, buildinfo.ResolvedBuildID())
		}
	}
	state, err := loadUserLifecycleState()
	if err != nil {
		t.Fatal(err)
	}
	if state.BuildID != buildinfo.ResolvedBuildID() || state.RegistryHash == "" {
		t.Fatalf("unexpected user lifecycle state: %+v", state)
	}
	if _, err = client.reconcileUserInstallation(context.Background(), roots[0]); err != nil {
		t.Fatalf("repeat user reconciliation was not idempotent: %v", err)
	}
}

// TestUserReconciliationWarnsPersistentlyAboutABrokenOtherProject verifies
// finding 3's fix: a broken project that is NOT the one the current command
// targets must never brick every other command. It should surface as a
// warning, not a hard error, and -- because the run was not fully clean --
// the completion marker must never be persisted, so the same warning keeps
// resurfacing on every later command until the broken project is actually
// fixed (per the user's confirmed preference, rather than going silent
// after a single warning).
func TestUserReconciliationWarnsPersistentlyAboutABrokenOtherProject(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	store.RuntimeVersion = Version
	store.RuntimeBuildID = "previous-build"
	healthy, broken := t.TempDir(), t.TempDir()
	for _, root := range []string{healthy, broken} {
		if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
			ProjectRoot: root, Owner: "owner", Mode: "personal",
		}); err != nil {
			t.Fatal(err)
		}
	}
	corruptProjectRuntimeMode(t, broken)

	client := &cli{timeout: time.Second}
	for attempt := 0; attempt < 2; attempt++ {
		warnings, err := client.reconcileUserInstallation(context.Background(), healthy)
		if err != nil {
			t.Fatalf("attempt %d: broken sibling project should warn, not hard-fail: %v", attempt, err)
		}
		if len(warnings) != 1 {
			t.Fatalf("attempt %d: warnings=%v, want exactly one warning about %s", attempt, warnings, broken)
		}
		state, stateErr := loadUserLifecycleState()
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		if !state.CompletedAt.IsZero() {
			t.Fatalf("attempt %d: completion marker must not persist while a registered project is broken: %+v", attempt, state)
		}
	}
}

// TestUserReconciliationHardFailsWhenCurrentProjectIsBroken verifies the
// other half of finding 3: when the broken project IS the one the current
// command targets, reconciliation must still hard-fail -- the command
// genuinely cannot proceed against its own target.
func TestUserReconciliationHardFailsWhenCurrentProjectIsBroken(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	store.RuntimeVersion = Version
	store.RuntimeBuildID = "previous-build"
	broken := t.TempDir()
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: broken, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	corruptProjectRuntimeMode(t, broken)

	client := &cli{timeout: time.Second}
	if _, err := client.reconcileUserInstallation(context.Background(), broken); err == nil {
		t.Fatal("expected a hard error when the current command's own project is broken")
	}
}

// TestUserReconciliationRescansWhenRegistryChanges verifies that adding a
// newly registered project between two calls invalidates the cached
// "build+registry" short-circuit -- the new project must be picked up and
// reconciled on the very next call, not silently skipped until some later
// unrelated cache invalidation.
func TestUserReconciliationRescansWhenRegistryChanges(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	store.RuntimeVersion = Version
	store.RuntimeBuildID = "previous-build"
	first := t.TempDir()
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: first, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}

	client := &cli{timeout: time.Second}
	if _, err := client.reconcileUserInstallation(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	firstState, err := loadUserLifecycleState()
	if err != nil {
		t.Fatal(err)
	}
	if firstState.CompletedAt.IsZero() {
		t.Fatal("expected the first clean reconciliation to persist a completion marker")
	}

	second := t.TempDir()
	if _, err = runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: second, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	downgradeProjectBuild(t, second)
	registerProjectProfile(t, second)

	if _, err = client.reconcileUserInstallation(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	config, err := store.Open(second).ConfigStrict()
	if err != nil {
		t.Fatal(err)
	}
	if config.ToolkitBuildID != buildinfo.ResolvedBuildID() {
		t.Fatalf("newly registered project %s was not reconciled after registry change: build=%s", second, config.ToolkitBuildID)
	}
	secondState, err := loadUserLifecycleState()
	if err != nil {
		t.Fatal(err)
	}
	if secondState.RegistryHash == firstState.RegistryHash {
		t.Fatal("registry hash did not change after registering a new project")
	}
}

func registerProjectProfile(t *testing.T, root string) {
	t.Helper()
	config, err := identity.LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]identity.Profile{}
	}
	config.Profiles[filepath.Base(root)] = identity.Profile{
		Name: filepath.Base(root), ProjectID: filepath.Base(root),
		Actor: "owner", ProjectRoot: root,
	}
	if err = identity.SaveUserConfig(config); err != nil {
		t.Fatal(err)
	}
}

func corruptProjectRuntimeMode(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, store.Runtime, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["runtime_mode"] = "legacy"
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func downgradeProjectBuild(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, store.Runtime, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["toolkit_build_id"] = "previous-build"
	raw, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
