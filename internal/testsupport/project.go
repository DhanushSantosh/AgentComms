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
	t.Cleanup(cancel)
	go func() {
		_ = daemon.Run(ctx, daemon.RunConfig{
			ServicePublicKey: config.ServicePublicKey,
			CachePath:        runtimeinit.ProjectionPath(root), Endpoint: config.DaemonEndpoint,
			RuntimeMode: "personal", PersonalDatabase: runtimeinit.DatabasePath(root),
			ServicePrivateKey: authorityCredential.PrivateKey, ProjectID: config.ProjectID,
			ProjectRoot: root, ConnectorConfigPath: os.Getenv("AGENT_COMMS_CONNECTOR_CONFIG"),
		})
	}()
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
