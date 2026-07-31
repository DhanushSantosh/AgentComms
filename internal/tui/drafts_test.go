package tui

import (
	"strings"
	"testing"
)

func TestDraftSaveThroughGuidedForm(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Drafts")
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "draft.save" {
		t.Fatalf("expected draft.save form, got %q", view.form)
	}
	view.inputs[0].SetValue("draft-review-notes")
	view.inputs[1].SetValue("document")
	view.inputs[2].SetValue("Looks good, ship it.")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatalf("save draft failed: %v", view.err)
	}

	drafts, err := instance.Drafts(50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range drafts {
		if d.ID == "draft-review-notes" && d.Kind == "document" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected draft-review-notes among drafts: %+v", drafts)
	}
	rendered := view.draftsView(colors(false))
	if !strings.Contains(rendered, "draft-review-notes") {
		t.Fatalf("drafts view missing draft-review-notes:\n%s", rendered)
	}
}
