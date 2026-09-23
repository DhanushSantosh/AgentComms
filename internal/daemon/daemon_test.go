package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// Exercises the DELETE route end to end through the real mux, not the
// handler in isolation: the method and the {draft} path segment are the
// parts a refactor can silently break without any compiler complaint.
func TestDeleteDraftRouteRemovesTheDraft(t *testing.T) {
	signer, _ := controlplane.GenerateSigner()
	cache, err := localcache.Open(filepath.Join(t.TempDir(), "cache.db"), signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	// The draft path never reaches the authority, but New requires a client.
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unused", http.StatusNotFound)
	}))
	defer authority.Close()
	client, _ := remote.New(authority.URL, time.Second)
	instance, err := New(cache, client)
	if err != nil {
		t.Fatal(err)
	}
	handler := instance.Handler()
	ctx := context.Background()

	// A draft ID needing escaping proves the client and the route agree on
	// encoding rather than both happening to be lenient about plain IDs.
	const draftID = "draft/one"
	if err = cache.SaveDraft(ctx, controlplane.Draft{
		ProjectID: "project", ID: draftID, Kind: "document", Body: json.RawMessage(`{"title":"Draft"}`),
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/v1/projects/project/drafts/"+url.PathEscape(draftID), nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", recorder.Code, recorder.Body.String())
	}
	drafts, err := cache.Drafts(ctx, "project", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 0 {
		t.Fatalf("expected the draft to be gone, got %#v", drafts)
	}

	// Deleting it again must surface the store's error, not a blanket 200.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodDelete, "/v1/projects/project/drafts/"+url.PathEscape(draftID), nil))
	if recorder.Code == http.StatusOK {
		t.Fatalf("deleting an absent draft returned 200: %s", recorder.Body.String())
	}
}
