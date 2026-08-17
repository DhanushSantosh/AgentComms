package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/wasmdemo"
)

// TestSeedDemoProjectSupportsAddingAnArtifact is the regression test for a
// real, reachable bug a code-review pass caught: internal/runtimeinit.go's
// writeRuntimeFiles creates "artifacts/sha256" (among other directories)
// under the runtime directory, but bootstrapProject only created "cache" --
// service.Service.AddArtifact (reachable from a live TUI session via
// internal/tui/artifacts.go) writes straight to
// <root>/.agent-comms/artifacts/sha256/<sum> via os.WriteFile with no
// MkdirAll of its own, assuming a real project's directory already exists.
// A live visitor attaching an artifact during the demo session would have
// hit a filesystem error a real project could never produce. Exercises the
// exact real path: bootstrapDemoService -> seedDemoProject -> a real
// svc.AddArtifact call against a real file on disk.
func TestSeedDemoProjectSupportsAddingAnArtifact(t *testing.T) {
	svc, err := bootstrapDemoService()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedDemoProject(svc); err != nil {
		t.Fatal(err)
	}

	artifactPath := filepath.Join(t.TempDir(), "release-notes.txt")
	if err := os.WriteFile(artifactPath, []byte("auth/session release notes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddArtifact(demoOwner, artifactPath); err != nil {
		t.Fatalf("AddArtifact: %v -- artifacts/sha256 is probably missing under the runtime directory", err)
	}

	state, err := svc.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Artifacts) == 0 {
		t.Error("expected the added artifact to appear in state.Artifacts")
	}
}

// TestBootstrapProjectSetsDaemonIdentityForHealthLive confirms bootstrapProject
// calls d.SetIdentity, exactly as internal/daemon/run.go's real daemon-start
// path always does -- without it, GET /health/live's runtime_mode and
// project_id fields stay at their zero values instead of reflecting the
// project bootstrapProject just created. Constructs the same daemon/authority
// pair bootstrapDemoService does and calls bootstrapProject directly (the
// exact function under test) so this is real daemon-handler behavior, not a
// simulated response.
func TestBootstrapProjectSetsDaemonIdentityForHealthLive(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := wasmdemo.NewMemoryAuthority(signer)
	cache := wasmdemo.NewMemoryCache()
	cache.SetServerPublicKey(signer.PublicKey())
	d, err := daemon.New(cache, authority)
	if err != nil {
		t.Fatal(err)
	}
	d.SetPersonalMode(true)

	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))

	if _, err = bootstrapProject(context.Background(), d, authority, root, signer); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	d.Handler().ServeHTTP(recorder, request)

	var response struct {
		RuntimeMode string `json:"runtime_mode"`
		ProjectID   string `json:"project_id"`
	}
	if err = json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.RuntimeMode != "personal" {
		t.Errorf("/health/live runtime_mode = %q, want %q -- SetIdentity was not called", response.RuntimeMode, "personal")
	}
	if response.ProjectID == "" {
		t.Error("/health/live project_id is empty -- SetIdentity was not called")
	}
}
