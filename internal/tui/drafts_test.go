package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

func TestDraftDeleteConfirmsExactSelectedID(t *testing.T) {
	instance := newTestService(t)
	for _, id := range []string{"keep", "delete-me"} {
		if err := instance.SaveDraft(id, "document", json.RawMessage(`{"content":"notes"}`)); err != nil {
			t.Fatal(err)
		}
	}
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Drafts")
	view = pressKey(t, view, keyEnter())
	if !view.rowFocus {
		t.Fatal("enter should focus the draft list")
	}
	view = pressKey(t, view, keyText("j"))
	selectedID := view.drafts[view.draftCursor].ID
	view = pressKey(t, view, keyText("d"))
	if view.confirm == nil || !view.confirm.localDraft || view.confirm.id != selectedID {
		t.Fatalf("confirmation must capture the selected ID exactly: %+v", view.confirm)
	}
	if rendered := view.renderConfirm(colors(false)); strings.Contains(rendered, "Signed change") || !strings.Contains(rendered, "Local draft deletion") {
		t.Fatalf("local confirmation describes a signed change: %s", rendered)
	}
	view = pressKey(t, view, keyText("n"))
	if view.confirm != nil {
		t.Fatal("cancelling should close confirmation")
	}
	if drafts, err := instance.Drafts(50); err != nil || len(drafts) != 2 {
		t.Fatalf("cancellation changed drafts: %+v, %v", drafts, err)
	}
	view = pressKey(t, view, keyText("d"))
	view = pressKey(t, view, keyText("y"))
	if view.err != nil || view.notice != "Deleted draft "+selectedID {
		t.Fatalf("delete failed: err=%v notice=%q", view.err, view.notice)
	}
	drafts, err := instance.Drafts(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID == selectedID || len(view.drafts) != 1 {
		t.Fatalf("delete should remove only %q: service=%+v view=%+v", selectedID, drafts, view.drafts)
	}
}

func TestDraftDeleteMissingIDShowsError(t *testing.T) {
	instance := newTestService(t)
	if err := instance.SaveDraft("gone", "document", json.RawMessage(`{"content":"notes"}`)); err != nil {
		t.Fatal(err)
	}
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Drafts")
	view = pressKey(t, view, keyEnter())
	view = pressKey(t, view, keyText("d"))
	if err := instance.DeleteDraft("gone"); err != nil {
		t.Fatal(err)
	}
	view = pressKey(t, view, keyText("y"))
	if view.err == nil || view.confirm != nil {
		t.Fatalf("missing draft must surface an error and close confirmation: err=%v confirm=%+v", view.err, view.confirm)
	}
}

func TestDraftMouseSelectionTargetsClickedRow(t *testing.T) {
	instance := newTestService(t)
	for _, id := range []string{"first", "second", "third"} {
		if err := instance.SaveDraft(id, "document", json.RawMessage(`{"content":"notes"}`)); err != nil {
			t.Fatal(err)
		}
	}
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Drafts")
	view = pressKey(t, view, keyEnter())
	clicked := 2
	view = pressMsg(t, view, tea.MouseClickMsg{
		X:      view.sidebarWidth() + 5,
		Y:      view.bodyPrefixHeight(colors(false)) + 1 + clicked,
		Button: tea.MouseLeft,
	})
	if view.draftCursor != clicked {
		t.Fatalf("clicked row %d selected row %d", clicked, view.draftCursor)
	}
	view = pressKey(t, view, keyText("d"))
	if view.confirm == nil || view.confirm.id != view.drafts[clicked].ID {
		t.Fatalf("delete confirmation targets wrong clicked draft: %+v", view.confirm)
	}
}

func TestDraftRowsStaySingleLineAtNarrowWidths(t *testing.T) {
	instance := newTestService(t)
	for _, id := range []string{"draft-with-a-very-long-name-that-used-to-wrap", "other-draft"} {
		if err := instance.SaveDraft(id, "document", json.RawMessage(`{"content":"notes"}`)); err != nil {
			t.Fatal(err)
		}
	}
	for _, terminalWidth := range []int{48, 56, 68} {
		view, err := New(instance, "owner")
		if err != nil {
			t.Fatal(err)
		}
		view.width = terminalWidth
		view.openView("Drafts")
		view = pressKey(t, view, keyEnter())
		lines := strings.Split(view.draftsView(colors(view.highContrast)), "\n")
		if len(lines) != len(view.drafts)+3 {
			t.Fatalf("width %d: expected one physical line per draft, got %d lines: %q", terminalWidth, len(lines), lines)
		}
		for i, line := range lines {
			if got := lipgloss.Width(line); got > view.contentWidth() {
				t.Fatalf("width %d: line %d spans %d columns (content width %d): %q", terminalWidth, i, got, view.contentWidth(), line)
			}
		}
		view = pressMsg(t, view, tea.MouseClickMsg{
			X:      view.sidebarWidth() + 5,
			Y:      view.bodyPrefixHeight(colors(view.highContrast)) + 2,
			Button: tea.MouseLeft,
		})
		if view.draftCursor != 1 {
			t.Fatalf("width %d: click on second physical row selected %d", terminalWidth, view.draftCursor)
		}
	}
}
