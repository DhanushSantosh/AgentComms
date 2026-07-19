package hybridmigration

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/authority"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func TestLegacyMigrationAndAtomicCutover(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	root := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s", output)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	legacy := store.Open(root)
	legacy.SetCredentialStore(identity.NewMemoryStore())
	if err := legacy.Init("owner"); err != nil {
		t.Fatal(err)
	}
	signer, _ := controlplane.GenerateSigner()
	engine, err := authority.Open(context.Background(), authority.Config{DatabaseURL: databaseURL}, signer)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	const token = "migration-test-token"
	server := httptest.NewServer(authority.NewHTTPServer(engine, authority.HTTPConfig{MigrationToken: token}).Handler())
	defer server.Close()

	result, err := Migrate(context.Background(), Config{
		ProjectRoot: root, AuthorityURL: server.URL, ServicePublicKey: signer.PublicKey(),
		DaemonEndpoint: filepath.Join(root, "daemon.sock"), MigrationToken: token, BatchSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "ACTIVATED" || result.ImportedEvents != 2 || result.Receipt == nil {
		t.Fatalf("result=%#v", result)
	}
	runtimeConfig, err := legacy.Config()
	if err != nil {
		t.Fatal(err)
	}
	if runtimeConfig.RuntimeMode != "service" || !runtimeConfig.LegacyReadOnly {
		t.Fatalf("runtime config=%#v", runtimeConfig)
	}
	bootstrap, err := os.ReadFile(filepath.Join(root, ".agents"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bootstrap, store.ServiceBootstrap()) {
		t.Fatal("service bootstrap was not activated")
	}
	if _, err = legacy.Append("owner", "env.set", "key", map[string]string{"key": "key", "value": "value"}); err == nil {
		t.Fatal("legacy write succeeded after service cutover")
	}
}
