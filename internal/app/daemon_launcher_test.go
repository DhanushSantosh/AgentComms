package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func TestSlowHealthyDaemonFixtureResolvesBuildIDBeforeServing(t *testing.T) {
	calls := 0
	handler := slowHealthyDaemonHandler(store.Config{ProjectID: "fixture"}, func() string {
		calls++
		return "captured-before-health"
	})
	if calls != 1 {
		t.Fatalf("fixture must resolve its build ID before serving, like daemon.Run; calls=%d", calls)
	}
	for request := 0; request < 2; request++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
		var health daemonclient.Health
		if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
			t.Fatal(err)
		}
		if health.BuildID != "captured-before-health" || calls != 1 {
			t.Fatalf("health must reuse the startup build ID, got %+v; resolver calls=%d", health, calls)
		}
	}
}

func TestReplacementDaemonHealthProbeUsesReadinessBudget(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", t.TempDir())
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", t.TempDir())
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	config, err := store.Open(root).Config()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := daemon.ListenLocal(config.DaemonEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/live" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(600 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(daemonclient.Health{Status: "live", BuildID: "replacement-fixture"})
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	health, err := replacementDaemonHealth(config.DaemonEndpoint)
	if err != nil {
		t.Fatalf("replacement fixture rejected a response within the production readiness budget: %v", err)
	}
	if health.BuildID != "replacement-fixture" {
		t.Fatalf("unexpected replacement health: %+v", health)
	}
}

func TestTestDaemonLauncherPreservesEarlyExitLogAfterParentClosesWriter(t *testing.T) {
	root := t.TempDir()
	cleanupProjectDaemon(t, root)
	configDir := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", configDir)
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	// Force a genuine daemon.Run failure before the named pipe/socket binds.
	// Only this isolated fixture's projection is deliberately invalidated.
	if err := os.WriteFile(runtimeinit.ProjectionPath(root), []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(configDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := launchDaemonProcess("unused-test-executable", root, logFile); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	// ensureDaemon closes its own writer immediately after launching. A real
	// child inherits a separate handle; the in-process launcher must too.
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}
	run, ok := testDaemonRuns.Load(root)
	if !ok {
		t.Fatal("test daemon completion was not tracked")
	}
	select {
	case <-run.(chan struct{}):
	case <-time.After(daemonReadyTimeout):
		t.Fatal("invalid-cache daemon did not exit")
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "daemon.Run failed:") ||
		!strings.Contains(string(contents), "not a database") {
		t.Fatalf("early startup failure was lost from the daemon log: %q", contents)
	}
}
