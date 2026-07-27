package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
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
	if err := client.reconcileUserInstallation(context.Background(), roots[0]); err != nil {
		t.Fatal(err)
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
	if err = client.reconcileUserInstallation(context.Background(), roots[0]); err != nil {
		t.Fatalf("repeat user reconciliation was not idempotent: %v", err)
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
