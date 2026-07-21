package localcache

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestApplyAndOfflineState(t *testing.T) {
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	cache, err := Open(filepath.Join(t.TempDir(), "cache.db"), signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	payload, _ := model.EncodePayload("agent.register", model.AgentRegistered{
		PublicKey: signer.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "Owner",
	})
	event := controlplane.Event{
		ProjectID: "project", Sequence: 1, ID: "evt-0001", Time: time.Now().UTC(),
		Actor: "owner", Type: "agent.register", EntityID: "owner", Payload: payload,
		ActorIntentHash: "intent", IdempotencyKey: "request-1",
	}
	event.Hash, _ = controlplane.HashEvent(event)
	receipt := controlplane.Receipt{
		ProjectID: event.ProjectID, Sequence: event.Sequence, EventID: event.ID,
		EventHash: event.Hash, ActorIntentHash: event.ActorIntentHash, CommittedAt: event.Time,
	}
	if err = signer.SignReceipt(&receipt); err != nil {
		t.Fatal(err)
	}
	if err = cache.Apply(context.Background(), event, receipt); err != nil {
		t.Fatal(err)
	}
	state, metadata, err := cache.State(context.Background(), "project")
	if err != nil {
		t.Fatal(err)
	}
	if state.Agents["owner"].DisplayName != "Owner" || metadata.CacheSequence != 1 || metadata.Consistency != "CACHED" {
		t.Fatalf("state=%#v metadata=%#v", state, metadata)
	}
	if err = cache.Apply(context.Background(), event, receipt); err != nil {
		t.Fatalf("idempotent cache apply failed: %v", err)
	}
}

func TestDraftBoundary(t *testing.T) {
	signer, _ := controlplane.GenerateSigner()
	cache, err := Open(filepath.Join(t.TempDir(), "cache.db"), signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	draft := controlplane.Draft{ProjectID: "project", ID: "draft-1", Kind: "document", Body: json.RawMessage(`{"title":"Draft"}`)}
	if err = cache.SaveDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	drafts, err := cache.Drafts(context.Background(), "project", 10)
	if err != nil || len(drafts) != 1 || drafts[0].ID != draft.ID {
		t.Fatalf("drafts=%#v err=%v", drafts, err)
	}
	draft.Kind = "lease"
	if err = cache.SaveDraft(context.Background(), draft); err == nil {
		t.Fatal("governed lease draft was accepted")
	}
}
