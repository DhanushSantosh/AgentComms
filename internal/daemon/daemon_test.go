package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/localcache"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/remote"
)

func TestSyncPopulatesVerifiedCache(t *testing.T) {
	signer, _ := controlplane.GenerateSigner()
	payload, _ := model.EncodePayload("agent.register", model.AgentRegistered{
		PublicKey: signer.PublicKey(), PrincipalType: model.PrincipalHuman, DisplayName: "Owner",
	})
	event := controlplane.Event{
		ProjectID: "project", Sequence: 1, ID: "evt-1", Time: time.Now().UTC(),
		Actor: "owner", Type: "agent.register", EntityID: "owner", Payload: payload,
		ActorIntentHash: "intent", IdempotencyKey: "request",
	}
	event.Hash, _ = controlplane.HashEvent(event)
	receipt := controlplane.Receipt{
		ProjectID: "project", Sequence: 1, EventID: event.ID, EventHash: event.Hash,
		ActorIntentHash: event.ActorIntentHash, CommittedAt: event.Time,
	}
	_ = signer.SignReceipt(&receipt)
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project/events" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(controlplane.EventPage{
			Items:    []controlplane.EventRecord{{Event: event, Receipt: receipt}},
			Metadata: controlplane.ResultMetadata{Consistency: "AUTHORITATIVE", ServerSequence: 1, Connectivity: "ONLINE"},
		})
	}))
	defer authority.Close()
	client, _ := remote.New(authority.URL, time.Second)
	cache, err := localcache.Open(filepath.Join(t.TempDir(), "cache.db"), signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	instance, _ := New(cache, client)
	if err = instance.Sync(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	state, metadata, err := cache.State(context.Background(), "project")
	if err != nil || state.Agents["owner"].DisplayName != "Owner" || metadata.CacheSequence != 1 {
		t.Fatalf("state=%#v metadata=%#v err=%v", state, metadata, err)
	}
}
