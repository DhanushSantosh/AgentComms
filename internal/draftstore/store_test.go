package draftstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

func TestSaveAndListDraftsRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	draft := controlplane.Draft{
		ProjectID: "proj-1", ID: "draft-1", Kind: "message",
		Body: json.RawMessage(`{"body":"hello"}`),
	}
	if err = store.SaveDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	drafts, err := store.Drafts(context.Background(), "proj-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != "draft-1" || string(drafts[0].Body) != string(draft.Body) {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}
	if drafts[0].CreatedAt.IsZero() || drafts[0].UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be stamped on save: %+v", drafts[0])
	}

	updated := draft
	updated.Body = json.RawMessage(`{"body":"updated"}`)
	if err = store.SaveDraft(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	drafts, err = store.Drafts(context.Background(), "proj-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || string(drafts[0].Body) != string(updated.Body) {
		t.Fatalf("expected in-place update of existing draft, got: %+v", drafts)
	}
}

func TestImportDraftPreservesOriginalTimestamps(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2021, 6, 7, 8, 9, 10, 0, time.UTC)
	draft := controlplane.Draft{
		ProjectID: "proj-1", ID: "draft-1", Kind: "document",
		Body: json.RawMessage(`{"body":"legacy"}`), CreatedAt: created, UpdatedAt: updated,
	}
	if err = store.ImportDraft(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	drafts, err := store.Drafts(context.Background(), "proj-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || !drafts[0].CreatedAt.Equal(created) || !drafts[0].UpdatedAt.Equal(updated) {
		t.Fatalf("ImportDraft did not preserve original timestamps: %+v", drafts)
	}
}

func TestSaveDraftRejectsInvalidInput(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cases := []struct {
		name  string
		draft controlplane.Draft
	}{
		{"missing project", controlplane.Draft{ID: "d", Kind: "message", Body: json.RawMessage(`{}`)}},
		{"missing id", controlplane.Draft{ProjectID: "p", Kind: "message", Body: json.RawMessage(`{}`)}},
		{"bad kind", controlplane.Draft{ProjectID: "p", ID: "d", Kind: "note", Body: json.RawMessage(`{}`)}},
		{"empty body", controlplane.Draft{ProjectID: "p", ID: "d", Kind: "message", Body: json.RawMessage(``)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err = store.SaveDraft(context.Background(), testCase.draft)
			if err == nil {
				t.Fatal("expected validation error")
			}
			cpErr, ok := err.(*controlplane.Error)
			if !ok || cpErr.Code != controlplane.CodeValidation {
				t.Fatalf("expected CodeValidation, got %v", err)
			}
		})
	}
}

func TestDraftCountQuotaIsEnforcedPerProject(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for index := 0; index < controlplane.MaxDraftsPerProject; index++ {
		draft := controlplane.Draft{
			ProjectID: "proj-1", ID: uniqueDraftID(index), Kind: "message", Body: json.RawMessage(`{"n":1}`),
		}
		if err = store.SaveDraft(ctx, draft); err != nil {
			t.Fatalf("draft %d: unexpected error at or below quota: %v", index, err)
		}
	}
	overflow := controlplane.Draft{
		ProjectID: "proj-1", ID: "one-too-many", Kind: "message", Body: json.RawMessage(`{"n":1}`),
	}
	err = store.SaveDraft(ctx, overflow)
	if err == nil {
		t.Fatal("expected the draft count quota to reject one more draft")
	}
	cpErr, ok := err.(*controlplane.Error)
	if !ok || cpErr.Code != controlplane.CodeRateLimited {
		t.Fatalf("expected CodeRateLimited, got %v", err)
	}
	// Updating an existing draft must remain allowed at the quota ceiling
	// -- the limit governs distinct draft count, not total writes.
	existing := controlplane.Draft{
		ProjectID: "proj-1", ID: uniqueDraftID(0), Kind: "message", Body: json.RawMessage(`{"n":2}`),
	}
	if err = store.SaveDraft(ctx, existing); err != nil {
		t.Fatalf("expected updating an existing draft to remain allowed at quota: %v", err)
	}
}

func TestDraftStorageByteQuotaIsEnforcedPerProject(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	large := make([]byte, controlplane.MaxDraftBytes)
	for index := range large {
		large[index] = 'a'
	}
	fits := controlplane.MaxDraftStorageBytes / controlplane.MaxDraftBytes
	for index := 0; index < fits; index++ {
		draft := controlplane.Draft{ProjectID: "proj-1", ID: uniqueDraftID(index), Kind: "artifact", Body: large}
		if err = store.SaveDraft(ctx, draft); err != nil {
			t.Fatalf("draft %d: unexpected error under the byte quota: %v", index, err)
		}
	}
	overflow := controlplane.Draft{ProjectID: "proj-1", ID: "overflow", Kind: "artifact", Body: large}
	err = store.SaveDraft(ctx, overflow)
	if err == nil {
		t.Fatal("expected the storage byte quota to reject a draft that would push total usage over the limit")
	}
	cpErr, ok := err.(*controlplane.Error)
	if !ok || cpErr.Code != controlplane.CodeRateLimited {
		t.Fatalf("expected CodeRateLimited, got %v", err)
	}
}

func TestOpenRejectsSymlinkedPath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.db")
	if err := os.WriteFile(real, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("expected Open to refuse a symlinked database path")
	}
}

func TestOpenHardensDatabaseFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file permissions are not meaningful on windows")
	}
	path := filepath.Join(t.TempDir(), "drafts.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("draft store file mode=%v, want 0600", info.Mode().Perm())
	}
}

func uniqueDraftID(index int) string {
	return "draft-" + strconv.Itoa(index)
}

func TestDeleteDraftRemovesOnlyTheNamedDraft(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	for _, id := range []string{"keep-me", "delete-me"} {
		if err = store.SaveDraft(ctx, controlplane.Draft{
			ProjectID: "proj-1", ID: id, Kind: "message", Body: json.RawMessage(`{"body":"x"}`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Same draft ID under a different project must survive: the primary key
	// is (project_id, draft_id), so a delete keyed on draft_id alone would
	// silently reach across projects.
	if err = store.SaveDraft(ctx, controlplane.Draft{
		ProjectID: "proj-2", ID: "delete-me", Kind: "message", Body: json.RawMessage(`{"body":"other"}`),
	}); err != nil {
		t.Fatal(err)
	}

	if err = store.DeleteDraft(ctx, "proj-1", "delete-me"); err != nil {
		t.Fatal(err)
	}
	drafts, err := store.Drafts(ctx, "proj-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != "keep-me" {
		t.Fatalf("expected only keep-me to remain in proj-1, got: %+v", drafts)
	}
	other, err := store.Drafts(ctx, "proj-2", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].ID != "delete-me" {
		t.Fatalf("delete crossed a project boundary, proj-2 holds: %+v", other)
	}
}

func TestDeleteDraftReportsAMissingDraft(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.DeleteDraft(context.Background(), "proj-1", "never-existed")
	if err == nil {
		t.Fatal("expected deleting an absent draft to report it, not succeed silently")
	}
	var controlErr *controlplane.Error
	if !errors.As(err, &controlErr) || controlErr.Code != controlplane.CodeValidation {
		t.Fatalf("want a controlplane VALIDATION error, got %#v", err)
	}
}

func TestDeleteDraftReleasesQuota(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "drafts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	// Fill the count quota exactly, confirm the next save is refused, then
	// delete one draft and confirm the same save now succeeds. This is the
	// recovery path the task exists for: before delete, a project that hit
	// the cap had no way back under it.
	for i := range controlplane.MaxDraftsPerProject {
		if err = store.SaveDraft(ctx, controlplane.Draft{
			ProjectID: "proj-1", ID: uniqueDraftID(i), Kind: "message", Body: json.RawMessage(`{"body":"x"}`),
		}); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	overflow := controlplane.Draft{
		ProjectID: "proj-1", ID: "one-too-many", Kind: "message", Body: json.RawMessage(`{"body":"x"}`),
	}
	if err = store.SaveDraft(ctx, overflow); err == nil {
		t.Fatal("expected the count quota to refuse a save at the cap")
	}
	if err = store.DeleteDraft(ctx, "proj-1", uniqueDraftID(0)); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveDraft(ctx, overflow); err != nil {
		t.Fatalf("expected delete to free quota for a new draft, got: %v", err)
	}
}
