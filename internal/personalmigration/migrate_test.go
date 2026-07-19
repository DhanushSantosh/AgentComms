package personalmigration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/personalauthority"
	"github.com/DhanushSantosh/AgentComms/internal/store"
)

func TestMigrateActivatesVerifiedPersonalAuthority(t *testing.T) {
	root := t.TempDir()
	projectStore := store.Open(root)
	credentials := identity.NewMemoryStore()
	projectStore.SetCredentialStore(credentials)
	if err := projectStore.Init("owner"); err != nil {
		t.Fatal(err)
	}
	legacyEvents, err := projectStore.Events()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Migrate(context.Background(), projectStore)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "ACTIVATED" || result.ImportedEvents != uint64(len(legacyEvents)) {
		t.Fatalf("unexpected migration result: %+v", result)
	}
	config, err := projectStore.Config()
	if err != nil {
		t.Fatal(err)
	}
	if config.RuntimeMode != "personal" || !config.LegacyReadOnly {
		t.Fatalf("personal cutover was not activated: %+v", config)
	}
	bootstrap, err := os.ReadFile(filepath.Join(root, ".agents"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bootstrap, store.PersonalBootstrap()) {
		t.Fatalf("unexpected personal bootstrap: %s", bootstrap)
	}
	credential, err := credentials.Get(config.ProjectID, AuthorityActor)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := controlplane.NewSigner(credential.PrivateKey)
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
	if state.Agents["owner"].Role != model.RoleOwner ||
		metadata.Consistency != "PERSONAL_AUTHORITATIVE" {
		t.Fatalf("imported state or metadata is invalid: state=%+v metadata=%+v", state, metadata)
	}
	if _, err = projectStore.Append("owner", "task.create", "forbidden",
		model.TaskCreated{Title: "Forbidden", Repository: "local", Branch: "main"}); err == nil {
		t.Fatal("legacy write succeeded after personal cutover")
	}
}
