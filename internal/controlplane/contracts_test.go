package controlplane

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCommandAndReceiptSignatures(t *testing.T) {
	actor, err := GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	command := Command{
		ProjectID: "project", Actor: "owner", Type: "task.create", EntityID: "task-1",
		Payload: json.RawMessage(`{"title":"Build"}`), IdempotencyKey: "request-1",
		IssuedAt: time.Now().UTC(),
	}
	if err = command.Sign(actor.PrivateKey()); err != nil {
		t.Fatal(err)
	}
	if !command.Verify(actor.PublicKey()) {
		t.Fatal("valid command signature was rejected")
	}
	command.EntityID = "task-2"
	if command.Verify(actor.PublicKey()) {
		t.Fatal("tampered command signature was accepted")
	}

	service, err := GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	receipt := Receipt{ProjectID: "project", Sequence: 1, EventID: "evt-1", EventHash: "hash", ActorIntentHash: "intent", CommittedAt: time.Now().UTC()}
	if err = service.SignReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	if !VerifyReceipt(receipt, service.PublicKey()) {
		t.Fatal("valid receipt signature was rejected")
	}
	receipt.Sequence = 2
	if VerifyReceipt(receipt, service.PublicKey()) {
		t.Fatal("tampered receipt was accepted")
	}
}

func TestVerifyEventHashAcceptsOnlyStructurallyValidAttestedImports(t *testing.T) {
	event := Event{
		ProjectID: "project", Sequence: 1, ID: "evt-1", Time: time.Now().UTC(),
		Actor: "owner", Type: "agent.register", Payload: json.RawMessage(`{"display_name":"owner"}`),
		Hash: strings.Repeat("a", 64), ActorIntentHash: strings.Repeat("b", 64),
		IdempotencyKey: "legacy:evt-1",
	}
	if !VerifyEventHash(event) {
		t.Fatal("valid attested import was rejected")
	}
	event.Payload = json.RawMessage(`{`)
	if VerifyEventHash(event) {
		t.Fatal("malformed attested import payload was accepted")
	}
	event.Payload = json.RawMessage(`{"display_name":"owner"}`)
	event.Hash = "short"
	if VerifyEventHash(event) {
		t.Fatal("malformed attested import hash was accepted")
	}
}

func TestCursorAndLimits(t *testing.T) {
	cursor := EncodeCursor(42)
	sequence, err := DecodeCursor(cursor)
	if err != nil || sequence != 42 {
		t.Fatalf("cursor round trip: sequence=%d err=%v", sequence, err)
	}
	if _, err = DecodeCursor("!"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if _, err = (PageRequest{Limit: MaxPageSize + 1}).BoundedLimit(); err == nil {
		t.Fatal("oversized page accepted")
	}
}
