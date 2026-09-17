package runtimeinit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func TestInitializePersonalCreatesAuthorityDirectly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	result, err := Initialize(context.Background(), Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	})
	if err != nil {
		t.Fatal(err)
	}
	projectStore := store.Open(root)
	config, err := projectStore.Config()
	if err != nil {
		t.Fatal(err)
	}
	if config.RuntimeMode != "personal" || result.ProjectID != config.ProjectID {
		t.Fatalf("unexpected initialization result=%+v config=%+v", result, config)
	}
	for _, obsolete := range []string{"events", "migrations", ".git"} {
		if _, statErr := os.Stat(filepath.Join(root, store.Runtime, obsolete)); !os.IsNotExist(statErr) {
			t.Fatalf("obsolete runtime path %s exists: %v", obsolete, statErr)
		}
	}
	authorityCredential, err := identity.ResolveCredential(
		projectStore.Credentials, config.ProjectID, personalAuthorityActor,
	)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := controlplane.NewSigner(authorityCredential.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := personalauthority.Open(DatabasePath(root), signer)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	state, metadata, err := engine.State(context.Background(), config.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["owner"].Status != "ACTIVE" || metadata.ServerSequence != 2 {
		t.Fatalf("owner was not initialized atomically: state=%+v metadata=%+v", state.Agents["owner"], metadata)
	}
}

func TestInitializeRefusesExistingBootstrap(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
	if err := os.WriteFile(filepath.Join(root, store.Bootstrap), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err == nil {
		t.Fatal("initialization overwrote an existing bootstrap")
	}
	if _, err := os.Stat(filepath.Join(root, store.Runtime)); !os.IsNotExist(err) {
		t.Fatalf("failed initialization published runtime data: %v", err)
	}
}

// TestInitializeIgnoresLegacyBootstrapName is the regression test for RFC
// 0031 / UX-01: ".agents" is a name several unrelated agent-tooling
// projects also use for their own directory, so init used to refuse to run
// against anything at that path at all -- a file, an empty directory, or a
// populated one. Init no longer looks at that path; only store.Bootstrap
// (".agentcomms") is checked.
func TestInitializeIgnoresLegacyBootstrapName(t *testing.T) {
	for _, name := range []string{"file", "empty dir", "populated dir"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(root, "user"))
			t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(root, "credentials"))
			legacy := filepath.Join(root, store.LegacyBootstrap)
			switch name {
			case "file":
				if err := os.WriteFile(legacy, []byte("some other tool's own file"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "empty dir":
				if err := os.Mkdir(legacy, 0o755); err != nil {
					t.Fatal(err)
				}
			case "populated dir":
				if err := os.MkdirAll(filepath.Join(legacy, "persona.json"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Initialize(context.Background(), Config{
				ProjectRoot: root, Owner: "owner", Mode: "personal",
			}); err != nil {
				t.Fatalf("expected init to ignore a pre-existing %s at the legacy bootstrap path, got: %v", name, err)
			}
			if _, err := os.Stat(legacy); err != nil {
				t.Fatalf("expected the pre-existing %s to survive untouched: %v", name, err)
			}
			if _, err := os.Stat(filepath.Join(root, store.Bootstrap)); err != nil {
				t.Fatalf("expected the current bootstrap to be created: %v", err)
			}
		})
	}
}

func TestInitializeRollsBackRuntimeAndCredentialsWhenProfilePublishFails(t *testing.T) {
	root := t.TempDir()
	credentialDirectory := filepath.Join(root, "credentials")
	configBlocker := filepath.Join(root, "config-blocker")
	if err := os.WriteFile(configBlocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", configBlocker)
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", credentialDirectory)

	if _, err := Initialize(context.Background(), Config{
		ProjectRoot: root, Owner: "owner", Mode: "personal",
	}); err == nil {
		t.Fatal("expected profile publication to fail")
	}
	for _, path := range []string{filepath.Join(root, store.Runtime), filepath.Join(root, store.Bootstrap)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed initialization retained %s: %v", path, err)
		}
	}
	entries, err := os.ReadDir(credentialDirectory)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed initialization retained credentials: %v", entries)
	}
}
