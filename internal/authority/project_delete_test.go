package authority

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/google/uuid"
)

// TestDeleteProjectCascadesAndTombstones is the end-to-end integration test
// for RFC 0020's server-side teardown: signature/authorization checks are
// verified fully server-side (never trusting a client's own claims), a
// successful deletion cascades every project-scoped table to zero rows,
// the deleted_projects tombstone survives the cascade with the right
// content, and re-deleting an already-deleted project fails cleanly
// instead of erroring ambiguously (idempotent, safe to retry).
func TestDeleteProjectCascadesAndTombstones(t *testing.T) {
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

	ctx := context.Background()
	projectID := "delete-" + uuid.NewString()
	if err = engine.CreateProject(ctx, projectID, "owner"); err != nil {
		t.Fatal(err)
	}
	owner, _ := controlplane.GenerateSigner()
	member, _ := controlplane.GenerateSigner()
	ownerElevated, _ := controlplane.GenerateSigner()
	memberElevated, _ := controlplane.GenerateSigner()
	bogus, _ := controlplane.GenerateSigner()

	mutate := func(actor string, signer *controlplane.Signer, typ, entity string, payload any) {
		t.Helper()
		raw, encodeErr := model.EncodePayload(typ, payload)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: typ, EntityID: entity,
			Payload: raw, IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if typ == "agent.register" {
			command.PublicKey = signer.PublicKey()
		}
		if signErr := command.Sign(signer.PrivateKey()); signErr != nil {
			t.Fatal(signErr)
		}
		if _, _, mutateErr := engine.Mutate(ctx, command); mutateErr != nil {
			t.Fatal(mutateErr)
		}
	}
	// Both registered as HUMAN: agent.elevate-key refuses an AGENT
	// principal (internal/protocol/transitions.go), and DeleteProject
	// needs both an owner and a non-owner human with their own elevated
	// key to exercise every authorization branch below.
	mutate("owner", owner, "agent.register", "owner", model.AgentRegistered{
		PublicKey: owner.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "owner",
	})
	mutate("owner", owner, "agent.activate", "owner",
		model.AgentActivated{Role: model.RoleOwner, Capabilities: []string{"*"}, Scopes: []string{"*"}})
	mutate("member", member, "agent.register", "member", model.AgentRegistered{
		PublicKey: member.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "member",
	})
	mutate("owner", owner, "agent.activate", "member", model.AgentActivated{Role: model.Role("Tester"), Scopes: []string{"src"}})
	mutate("owner", owner, "agent.elevate-key", "owner", model.AgentElevatedKeyRegistered{PublicKey: ownerElevated.PublicKey()})
	mutate("member", member, "agent.elevate-key", "member", model.AgentElevatedKeyRegistered{PublicKey: memberElevated.PublicKey()})
	mutate("owner", owner, "task.create", "task-1",
		model.TaskCreated{Title: "T", Repository: "r", Branch: "b", Resources: []string{"src/x"}})

	deleteCommand := func(actor string, signer *controlplane.Signer) controlplane.Command {
		command := controlplane.Command{
			ProjectID: projectID, Actor: actor, Type: "project.delete", EntityID: projectID,
			IdempotencyKey: uuid.NewString(), IssuedAt: time.Now().UTC(),
		}
		if signErr := command.Sign(signer.PrivateKey()); signErr != nil {
			t.Fatal(signErr)
		}
		return command
	}
	expectCode := func(t *testing.T, err error, code controlplane.ErrorCode) {
		t.Helper()
		if err == nil {
			t.Fatalf("expected an error with code %s, got success", code)
		}
		var controlErr *controlplane.Error
		if !errors.As(err, &controlErr) {
			t.Fatalf("expected a *controlplane.Error, got %v", err)
		}
		if controlErr.Code != code {
			t.Fatalf("expected code %s, got %s (%v)", code, controlErr.Code, err)
		}
	}

	// A signature that doesn't match the owner's registered elevated key at
	// all -- rejected as a signature integrity failure.
	expectCode(t, engine.DeleteProject(ctx, deleteCommand("owner", bogus)), controlplane.CodeIntegrity)
	// A different, non-owner actor, correctly signed with THEIR OWN
	// elevated key -- rejected on role, never reaches signature
	// verification against the wrong key.
	expectCode(t, engine.DeleteProject(ctx, deleteCommand("member", memberElevated)), controlplane.CodeAuthorization)
	// The real owner, but signed with their ordinary primary key instead
	// of the elevated one -- still rejected, elevation is mandatory.
	expectCode(t, engine.DeleteProject(ctx, deleteCommand("owner", owner)), controlplane.CodeIntegrity)

	// The real thing: owner, signed with their own registered elevated key.
	if err = engine.DeleteProject(ctx, deleteCommand("owner", ownerElevated)); err != nil {
		t.Fatalf("expected deletion to succeed, got %v", err)
	}

	for _, table := range []string{"projects", "agents", "tasks", "events", "actor_keys"} {
		var count int
		if scanErr := engine.db.QueryRowContext(ctx,
			"SELECT count(*) FROM "+table+" WHERE project_id = $1", projectID).Scan(&count); scanErr != nil {
			t.Fatal(scanErr)
		}
		if count != 0 {
			t.Fatalf("expected %s to have 0 rows for the deleted project, got %d", table, count)
		}
	}
	var tombstoneOwner, tombstoneDeletedBy, tombstoneFingerprint string
	if scanErr := engine.db.QueryRowContext(ctx,
		"SELECT owner_id, deleted_by, actor_key_fingerprint FROM deleted_projects WHERE project_id = $1", projectID,
	).Scan(&tombstoneOwner, &tombstoneDeletedBy, &tombstoneFingerprint); scanErr != nil {
		t.Fatalf("expected a surviving tombstone row: %v", scanErr)
	}
	if tombstoneOwner != "owner" || tombstoneDeletedBy != "owner" || tombstoneFingerprint == "" {
		t.Fatalf("tombstone row has unexpected content: owner=%q deleted_by=%q fingerprint=%q",
			tombstoneOwner, tombstoneDeletedBy, tombstoneFingerprint)
	}

	// Idempotent: re-deleting an already-deleted project fails cleanly
	// instead of erroring ambiguously -- safe for a client to retry after a
	// dropped response.
	expectCode(t, engine.DeleteProject(ctx, deleteCommand("owner", ownerElevated)), controlplane.CodeValidation)
}
