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

// TestVerifyEventHashRecomputesLegacyImportedEvents guards against a real,
// live-found regression: an earlier refactor (commit f78b940) removed the
// Legacy field from the canonical hash HashEvent computes, silently
// breaking verification for events imported by the legacy migration path
// (their originally-computed hash no longer matched what recomputing
// produced), and a later "fix" (004a023) papered over that by skipping
// hash recomputation entirely for legacy-prefixed events -- trusting only
// their structural shape, not their actual content. This test proves
// legacy-prefixed events are genuinely content-verified again: a
// correctly-hashed one passes, and tampering with any field it's hashed
// over is actually caught, not silently accepted.
func TestVerifyEventHashRecomputesLegacyImportedEvents(t *testing.T) {
	event := Event{
		ProjectID: "project", Sequence: 1, ID: "evt-1", Time: time.Now().UTC(),
		Actor: "owner", Type: "agent.register", Payload: json.RawMessage(`{"display_name":"owner"}`),
		ActorIntentHash: strings.Repeat("b", 64),
		IdempotencyKey:  "legacy:evt-1",
	}
	hash, err := HashEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Hash = hash
	if !VerifyEventHash(event) {
		t.Fatal("correctly-hashed legacy-imported event was rejected")
	}

	tampered := event
	tampered.Payload = json.RawMessage(`{"display_name":"attacker"}`)
	if VerifyEventHash(tampered) {
		t.Fatal("legacy-imported event with tampered payload was accepted -- hash was not actually recomputed")
	}

	tampered = event
	tampered.Actor = "someone-else"
	if VerifyEventHash(tampered) {
		t.Fatal("legacy-imported event with tampered actor was accepted -- hash was not actually recomputed")
	}

	tampered = event
	tampered.Payload = json.RawMessage(`{`)
	if VerifyEventHash(tampered) {
		t.Fatal("malformed attested import payload was accepted")
	}

	tampered = event
	tampered.Hash = "short"
	if VerifyEventHash(tampered) {
		t.Fatal("malformed attested import hash was accepted")
	}
}

// TestVerifyEventHashRecomputesNativeEvents proves ordinary (non-legacy)
// events are also genuinely content-verified, not just legacy-imported
// ones -- both paths in VerifyEventHash converge on the same real
// recomputation.
func TestVerifyEventHashRecomputesNativeEvents(t *testing.T) {
	event := Event{
		ProjectID: "project", Sequence: 1, ID: "evt-1", Time: time.Now().UTC(),
		Actor: "owner", Type: "agent.register", Payload: json.RawMessage(`{"display_name":"owner"}`),
		ActorIntentHash: strings.Repeat("b", 64), IdempotencyKey: "request-1",
	}
	hash, err := HashEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Hash = hash
	if !VerifyEventHash(event) {
		t.Fatal("correctly-hashed native event was rejected")
	}
	tampered := event
	tampered.EntityID = "different-entity"
	if VerifyEventHash(tampered) {
		t.Fatal("tampered native event was accepted")
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
