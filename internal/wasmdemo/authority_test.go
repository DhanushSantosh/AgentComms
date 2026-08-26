package wasmdemo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// registerCommand builds a self-signed agent.register command for actor,
// using a freshly generated identity.Credential -- mirroring how a real
// client signs a command before sending it to an authority.
func registerCommand(t *testing.T, projectID, actor string) (controlplane.Command, identity.Credential) {
	t.Helper()
	credential, err := identity.Generate(projectID, actor)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(model.AgentRegistered{
		PublicKey: credential.PublicKey, PrincipalType: model.PrincipalHuman, DisplayName: actor,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := controlplane.Command{
		ProjectID: projectID, Actor: actor, Type: "agent.register", EntityID: actor,
		Payload: payload, IdempotencyKey: "idem-" + actor + "-register",
		IssuedAt: time.Now().UTC(), PublicKey: credential.PublicKey,
	}
	sig, err := identity.Sign(credential, mustIntentHash(t, command))
	if err != nil {
		t.Fatal(err)
	}
	command.Signature = sig
	return command, credential
}

func mustIntentHash(t *testing.T, command controlplane.Command) string {
	t.Helper()
	hash, err := command.IntentHash()
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestMemoryAuthorityAppliesACommandAndReturnsASignedReceipt(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	if err := authority.CreateProject(ctx, "demo", "owner"); err != nil {
		t.Fatal(err)
	}

	command, _ := registerCommand(t, "demo", "owner")
	event, receipt, err := authority.Command(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 1 {
		t.Errorf("expected first event to be sequence 1, got %d", event.Sequence)
	}
	if receipt.Signature == "" {
		t.Error("expected a non-empty receipt signature")
	}
	if !controlplane.VerifyReceipt(receipt, signer.PublicKey()) {
		t.Error("expected receipt to verify against the authority's signer public key")
	}
	if !controlplane.VerifyEventHash(event) {
		t.Error("expected event hash to verify")
	}
}

func TestMemoryAuthorityRejectsAnInvalidTransitionViaRealProtocolValidation(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	if err := authority.CreateProject(ctx, "demo", "owner"); err != nil {
		t.Fatal(err)
	}

	// A task.claim from an actor that was never registered must be rejected
	// by internal/protocol's real validation -- this is the one thing this
	// in-memory authority must not stub out.
	credential, err := identity.Generate("demo", "ghost")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	command := controlplane.Command{
		ProjectID: "demo", Actor: "ghost", Type: "task.claim", EntityID: "task-1",
		Payload: payload, IdempotencyKey: "idem-ghost-claim",
		IssuedAt: time.Now().UTC(),
	}
	hash, err := command.IntentHash()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := identity.Sign(credential, hash)
	if err != nil {
		t.Fatal(err)
	}
	command.Signature = sig

	_, _, err = authority.Command(ctx, command)
	if err == nil {
		t.Fatal("expected an error rejecting the unregistered actor's command")
	}
}

func TestMemoryAuthorityChainsASecondEventToTheFirst(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	if err := authority.CreateProject(ctx, "demo", "owner"); err != nil {
		t.Fatal(err)
	}

	registerCmd, credential := registerCommand(t, "demo", "owner")
	firstEvent, _, err := authority.Command(ctx, registerCmd)
	if err != nil {
		t.Fatal(err)
	}

	activatePayload, err := json.Marshal(model.AgentActivated{Role: model.RoleOwner})
	if err != nil {
		t.Fatal(err)
	}
	activateCmd := controlplane.Command{
		ProjectID: "demo", Actor: "owner", Type: "agent.activate", EntityID: "owner",
		Payload: activatePayload, IdempotencyKey: "idem-owner-activate",
		IssuedAt: time.Now().UTC(),
	}
	hash, err := activateCmd.IntentHash()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := identity.Sign(credential, hash)
	if err != nil {
		t.Fatal(err)
	}
	activateCmd.Signature = sig

	secondEvent, _, err := authority.Command(ctx, activateCmd)
	if err != nil {
		t.Fatal(err)
	}
	if secondEvent.Sequence != 2 {
		t.Errorf("expected second event to be sequence 2, got %d", secondEvent.Sequence)
	}
	if secondEvent.PreviousHash != firstEvent.Hash {
		t.Errorf("expected second event's previous hash to reference the first event's hash: got %q, want %q",
			secondEvent.PreviousHash, firstEvent.Hash)
	}
}

func TestMemoryAuthorityEventsReturnsAppendedEvents(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	if err := authority.CreateProject(ctx, "demo", "owner"); err != nil {
		t.Fatal(err)
	}

	command, _ := registerCommand(t, "demo", "owner")
	if _, _, err := authority.Command(ctx, command); err != nil {
		t.Fatal(err)
	}

	page, err := authority.Events(ctx, "demo", controlplane.PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 event, got %d", len(page.Items))
	}
	if page.Items[0].Event.Type != "agent.register" {
		t.Errorf("expected agent.register event, got %q", page.Items[0].Event.Type)
	}
}
