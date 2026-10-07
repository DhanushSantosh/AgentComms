package authority

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestProviderLifecycleAuthority runs RFC 0050 against a real PostgreSQL
// authority: providers persist in their own table, agent registration uses
// the project's set, and retirement only stops new registrations.
func TestProviderLifecycleAuthority(t *testing.T) {
	databaseURL := os.Getenv("AGENT_COMMS_TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("AGENT_COMMS_TEST_POSTGRES_URL is not configured")
	}
	serviceSigner, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Open(context.Background(), Config{DatabaseURL: databaseURL}, serviceSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	engine.now = func() time.Time { return now }
	projectID := "providers-" + uuid.NewString()
	if err = engine.CreateProject(context.Background(), projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	signers := map[string]*controlplane.Signer{}
	signer := func(id string) *controlplane.Signer {
		if signers[id] == nil {
			generated, generateErr := controlplane.GenerateSigner()
			if generateErr != nil {
				t.Fatal(generateErr)
			}
			signers[id] = generated
		}
		return signers[id]
	}
	mutate := func(actor, typ, id string, payload any) error {
		raw, encodeErr := model.EncodePayload(typ, payload)
		if encodeErr != nil {
			return encodeErr
		}
		cmd := controlplane.Command{ProjectID: projectID, Actor: actor, Type: typ, EntityID: id, Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: now}
		if typ == "agent.register" {
			cmd.PublicKey = signer(actor).PublicKey()
		}
		if signErr := cmd.Sign(signer(actor).PrivateKey()); signErr != nil {
			return signErr
		}
		_, _, mutateErr := engine.Mutate(context.Background(), cmd)
		return mutateErr
	}
	must := func(actor, typ, id string, payload any) {
		t.Helper()
		if mutateErr := mutate(actor, typ, id, payload); mutateErr != nil {
			t.Fatalf("%s %s: %v", typ, id, mutateErr)
		}
	}
	register := func(id string, principalType model.PrincipalType) error {
		return mutate(id, "agent.register", id, model.AgentRegistered{PublicKey: signer(id).PublicKey(), PrincipalType: principalType, DisplayName: id})
	}
	state := func() model.State {
		t.Helper()
		loaded, _, stateErr := engine.State(context.Background(), projectID)
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		return loaded
	}

	if err = register("owner", model.PrincipalHuman); err != nil {
		t.Fatal(err)
	}
	must("owner", "agent.activate", "owner", model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})

	if err = register("gemini-main", model.PrincipalAgent); err == nil || !strings.Contains(err.Error(), "provider add gemini") {
		t.Fatalf("an unregistered provider must be refused with the command to fix it, got %v", err)
	}
	must("owner", "provider.register", "gemini", model.ProviderRegistered{DisplayName: "Google Gemini CLI"})
	if got := state().Providers["gemini"]; got.Status != model.ProviderStatusActive || got.DisplayName != "Google Gemini CLI" || got.AddedBy != "owner" || got.CreatedAt.IsZero() {
		t.Fatalf("persisted provider = %+v", got)
	}
	if err = register("gemini-main", model.PrincipalAgent); err != nil {
		t.Fatalf("gemini-main must register once gemini is a provider: %v", err)
	}

	must("owner", "provider.retire", "gemini", model.ProviderRetired{Reason: "trial ended"})
	retired := state().Providers["gemini"]
	if retired.Status != model.ProviderStatusRetired || retired.RetireReason != "trial ended" {
		t.Fatalf("persisted retirement = %+v", retired)
	}
	if err = register("gemini-second", model.PrincipalAgent); err == nil {
		t.Fatal("a retired provider must refuse new agents")
	}
	if _, ok := state().Agents["gemini-main"]; !ok {
		t.Fatal("retiring a provider must not remove its existing agents")
	}
	if err = mutate("owner", "provider.retire", "codex", model.ProviderRetired{Reason: "x"}); err == nil {
		t.Fatal("a built-in provider must not be retirable")
	}
}

func TestProvidersMigrationIsAutomaticAndCreatesTheTable(t *testing.T) {
	var migration *schemaMigration
	for i := range schemaMigrations {
		if schemaMigrations[i].Version == 8 {
			migration = &schemaMigrations[i]
		}
	}
	if migration == nil || !migration.Automatic || migration.Name != "project-registered-providers" ||
		!strings.Contains(migration.SQL, "CREATE TABLE IF NOT EXISTS providers") {
		t.Fatalf("migration 8 = %+v", migration)
	}
}
