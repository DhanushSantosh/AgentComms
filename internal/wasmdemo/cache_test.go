package wasmdemo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

// authorityAndCommand wires a real MemoryAuthority producing a genuine
// signed agent.register event/receipt pair for project, so cache tests
// exercise MemoryCache.Apply against authentic, verifiable inputs rather
// than hand-built fixtures.
func authorityAndCommand(t *testing.T, projectID, actor string) (*MemoryAuthority, controlplane.Event, controlplane.Receipt) {
	t.Helper()
	signer, err := controlplane.GenerateSigner()
	if err != nil {
		t.Fatal(err)
	}
	authority := NewMemoryAuthority(signer)
	ctx := context.Background()
	if err := authority.CreateProject(ctx, projectID, actor); err != nil {
		t.Fatal(err)
	}
	command, _ := registerCommand(t, projectID, actor)
	event, receipt, err := authority.Command(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	return authority, event, receipt
}

func TestMemoryCacheApplyThenStateReturnsResultingState(t *testing.T) {
	authority, event, receipt := authorityAndCommand(t, "demo", "owner")
	cache := NewMemoryCache()
	cache.SetServerPublicKey(authority.signer.PublicKey())

	ctx := context.Background()
	if err := cache.Apply(ctx, event, receipt); err != nil {
		t.Fatal(err)
	}

	state, metadata, err := cache.State(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, found := state.Agents["owner"]; !found {
		t.Error("expected applied agent.register event to be reflected in cached state")
	}
	if metadata.CacheSequence != 1 {
		t.Errorf("expected cache sequence 1, got %d", metadata.CacheSequence)
	}
}

func TestMemoryCacheEventsReturnsRequestedPageRange(t *testing.T) {
	authority, event, receipt := authorityAndCommand(t, "demo", "owner")
	cache := NewMemoryCache()
	cache.SetServerPublicKey(authority.signer.PublicKey())
	ctx := context.Background()
	if err := cache.Apply(ctx, event, receipt); err != nil {
		t.Fatal(err)
	}

	page, err := cache.Events(ctx, "demo", controlplane.PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 event, got %d", len(page.Items))
	}
	if page.Items[0].Event.Sequence != 1 {
		t.Errorf("expected sequence 1, got %d", page.Items[0].Event.Sequence)
	}

	// Paging past the only event returns nothing further.
	after, err := cache.Events(ctx, "demo", controlplane.PageRequest{Cursor: controlplane.EncodeCursor(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Items) != 0 {
		t.Errorf("expected no events past sequence 1, got %d", len(after.Items))
	}
}

func TestMemoryCacheVerifyRangeAcceptsAppliedEvents(t *testing.T) {
	authority, event, receipt := authorityAndCommand(t, "demo", "owner")
	cache := NewMemoryCache()
	cache.SetServerPublicKey(authority.signer.PublicKey())
	ctx := context.Background()
	if err := cache.Apply(ctx, event, receipt); err != nil {
		t.Fatal(err)
	}
	if err := cache.VerifyRange(ctx, "demo", 1, 1); err != nil {
		t.Errorf("expected verification of applied range to succeed, got %v", err)
	}
}

func TestMemoryCacheSaveDraftThenDraftsReturnsIt(t *testing.T) {
	cache := NewMemoryCache()
	ctx := context.Background()
	body, err := json.Marshal(map[string]string{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	draft := controlplane.Draft{ProjectID: "demo", ID: "draft-1", Kind: "message", Body: body}
	if err := cache.SaveDraft(ctx, draft); err != nil {
		t.Fatal(err)
	}
	drafts, err := cache.Drafts(ctx, "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(drafts))
	}
	if drafts[0].ID != "draft-1" {
		t.Errorf("expected draft-1, got %q", drafts[0].ID)
	}
}

func TestMemoryCacheDeleteDraftRemovesItAndReportsMissingIDs(t *testing.T) {
	cache := NewMemoryCache()
	ctx := context.Background()
	body, err := json.Marshal(map[string]string{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"draft-1", "draft-2"} {
		if err = cache.SaveDraft(ctx, controlplane.Draft{
			ProjectID: "demo", ID: id, Kind: "message", Body: body,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err = cache.DeleteDraft(ctx, "demo", "draft-1"); err != nil {
		t.Fatal(err)
	}
	drafts, err := cache.Drafts(ctx, "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != "draft-2" {
		t.Fatalf("expected only draft-2 to remain, got %#v", drafts)
	}
	// The demo cache backs the WASM TUI's draft store, so it has to fail the
	// same way the on-disk stores do rather than silently succeeding.
	err = cache.DeleteDraft(ctx, "demo", "draft-1")
	var controlErr *controlplane.Error
	if !errors.As(err, &controlErr) || controlErr.Code != controlplane.CodeValidation {
		t.Fatalf("want a VALIDATION error deleting an already-deleted draft, got %#v", err)
	}
}
