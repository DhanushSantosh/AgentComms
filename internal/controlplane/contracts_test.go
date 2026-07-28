package controlplane

import (
	"encoding/json"
	"errors"
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

func TestEventHashKeepsPreFingerprintCanonicalBytesCompatible(t *testing.T) {
	event := Event{
		ProjectID: "project-1", Sequence: 7, ID: "evt-00000000000000000007",
		Time:  time.Date(2026, 7, 28, 12, 34, 56, 123456789, time.UTC),
		Actor: "alpha", Type: "task.create", EntityID: "task-1",
		Payload: json.RawMessage(`{"title":"stable"}`), PreviousHash: "previous",
		ActorIntentHash: "intent", IdempotencyKey: "idem-1",
	}
	const preFingerprintHash = "478c10380ac0469328871a76c7304d635034a20571bc474fcdc1ea912d442776"
	hash, err := HashEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if hash != preFingerprintHash {
		t.Fatalf("empty actor key fingerprint changed the pre-RFC canonical hash: got %s want %s", hash, preFingerprintHash)
	}
}

func TestEventHashAttestsActorKeyFingerprint(t *testing.T) {
	event := Event{
		ProjectID: "project", Sequence: 1, ID: "evt-1", Time: time.Now().UTC(),
		Actor: "owner", ActorKeyFingerprint: "key-fingerprint-a",
		Type: "agent.register", Payload: json.RawMessage(`{"display_name":"owner"}`),
		ActorIntentHash: strings.Repeat("b", 64), IdempotencyKey: "request-1",
	}
	hash, err := HashEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Hash = hash
	if !VerifyEventHash(event) {
		t.Fatal("correctly hashed actor key fingerprint was rejected")
	}
	tampered := event
	tampered.ActorKeyFingerprint = "key-fingerprint-b"
	if VerifyEventHash(tampered) {
		t.Fatal("event with a tampered actor key fingerprint was accepted")
	}
	withoutFingerprint := event
	withoutFingerprint.ActorKeyFingerprint = ""
	withoutFingerprintHash, err := HashEvent(withoutFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if withoutFingerprintHash == hash {
		t.Fatal("populating actor key fingerprint did not change the event hash")
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

// TestClassifyValidationError guards the single shared classification both
// authority backends (internal/personalauthority, internal/authority) call
// -- previously each hand-maintained its own copy, which had already
// drifted (a dead "already claimed" branch existed in one but not the
// other; no real ValidateTransition message has ever contained that
// phrase).
func TestClassifyValidationError(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    ErrorCode
	}{
		{"role required", "owner or orchestrator role required", CodeAuthorization},
		{"principal required", "human principal required to grant the orchestrator role", CodeAuthorization},
		{"scope required", "a matching scope is required for this resource", CodeAuthorization},
		{"required without a matching keyword", "active message recipient x is required", CodeValidation},
		{"resource overlap", "write lease overlaps task t-1", CodeConflict},
		{"already leased", "worktree w is already leased by owner (task t-1, expires 10:00)", CodeConflict},
		{"already claimed is not special-cased", "task is no longer available to claim", CodeValidation},
		{"generic validation", "display name is required", CodeValidation},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClassifyValidationError(errors.New(c.message))
			if got.Code != c.want {
				t.Fatalf("ClassifyValidationError(%q) code = %q, want %q", c.message, got.Code, c.want)
			}
			if got.Message != c.message {
				t.Fatalf("expected the original message to be preserved, got %q", got.Message)
			}
		})
	}
}
