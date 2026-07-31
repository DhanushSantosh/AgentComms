package testsupport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/daemon"
	"github.com/DhanushSantosh/AgentComms/internal/daemonclient"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/runtimeinit"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

const (
	personalDaemonReadyTimeout = 5 * time.Second
	personalDaemonPollInterval = 10 * time.Millisecond
	// personalDaemonStopTimeout bounds how long test cleanup waits for the
	// daemon goroutine to exit after cancel() is called. This must
	// comfortably exceed the graceful-shutdown allowance daemon.Run itself
	// grants server.Shutdown (internal/daemon/run.go's
	// daemonShutdownTimeout, 10s) -- a daemon legitimately using its full
	// shutdown budget hasn't closed daemonStopped yet at 5s, which failed
	// this cleanup intermittently under real scheduling jitter (confirmed
	// live in CI: TestInvocationClaimIsExclusive failing with "personal
	// daemon did not stop before test cleanup" on a clean re-run of the
	// exact same commit). Separate from personalDaemonReadyTimeout (the
	// startup wait) since a fresh daemon binding a socket and a daemon
	// gracefully draining an in-flight request are different operations
	// with different natural timeouts.
	personalDaemonStopTimeout = 15 * time.Second
)

func StartPersonalProject(t testing.TB) (*service.Service, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	if _, err := runtimeinit.Initialize(context.Background(), runtimeinit.Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err != nil {
		t.Fatal(err)
	}
	projectStore := store.Open(root)
	config, err := projectStore.Config()
	if err != nil {
		t.Fatal(err)
	}
	authorityCredential, err := identity.ResolveCredential(
		projectStore.Credentials, config.ProjectID, "__personal_authority__",
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	daemonStopped := make(chan struct{})
	go func() {
		defer close(daemonStopped)
		_ = daemon.Run(ctx, daemon.RunConfig{
			ServicePublicKey: config.ServicePublicKey,
			CachePath:        runtimeinit.ProjectionPath(root), Endpoint: config.DaemonEndpoint,
			RuntimeMode: "personal", PersonalDatabase: runtimeinit.DatabasePath(root),
			ServicePrivateKey: authorityCredential.PrivateKey, ProjectID: config.ProjectID,
			ProjectRoot: root, ConnectorConfigPath: os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG"),
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-daemonStopped:
		case <-time.After(personalDaemonStopTimeout):
			t.Errorf("personal daemon did not stop before test cleanup")
		}
	})
	client, err := daemonclient.New(config.DaemonEndpoint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(personalDaemonReadyTimeout)
	for time.Now().Before(deadline) {
		if client.Healthy(context.Background()) == nil {
			return service.New(root), root
		}
		time.Sleep(personalDaemonPollInterval)
	}
	t.Fatal("personal daemon did not become ready")
	return nil, ""
}
