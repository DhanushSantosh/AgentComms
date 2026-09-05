package tui

import (
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestDocumentCreateThenUpdateThenSupersede(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Documents")
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "document.create" {
		t.Fatalf("expected document.create form, got %q", view.form)
	}
	values := []string{"guide-v1", "Operator guide", "First version", "reference"}
	for i, v := range values {
		view.inputs[i].SetValue(v)
	}
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}

	st, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := st.Documents["guide-v1"]
	if !ok || doc.Status != "ACTIVE" || doc.Version != 1 {
		t.Fatalf("document not created as expected: %+v", doc)
	}

	view.rowFocus = true
	if id := view.documentList.SelectedID(view.state, view.actor); id != "guide-v1" {
		t.Fatalf("selected id = %q, want guide-v1", id)
	}
	view = pressKey(t, view, keyText("e"))
	if view.form != "document.update" {
		t.Fatalf("expected document.update form, got %q", view.form)
	}
	view.inputs[0].SetValue("Operator guide")
	view.inputs[1].SetValue("Second version")
	view.inputs[2].SetValue("reference,current")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	st, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Documents["guide-v1"].Version != 2 || st.Documents["guide-v1"].Body != "Second version" {
		t.Fatalf("document not updated as expected: %+v", st.Documents["guide-v1"])
	}

	if _, err := instance.Execute("owner", "document.create", "guide-v2",
		model.DocumentPayload{Title: "Operator guide", Body: "Replacement", Tags: []string{"reference"}}); err != nil {
		t.Fatal(err)
	}
	view.refresh()
	view.rowFocus = true
	view = pressKey(t, view, keyText("s"))
	if view.form != "document.supersede" {
		t.Fatalf("expected document.supersede form, got %q", view.form)
	}
	view.inputs[0].SetValue("guide-v2")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	st, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Documents["guide-v1"].Status != "SUPERSEDED" {
		t.Fatalf("expected guide-v1 to be superseded: %+v", st.Documents["guide-v1"])
	}
	if st.Documents["guide-v2"].Status != "ACTIVE" || st.Documents["guide-v2"].Supersedes != "guide-v1" {
		t.Fatalf("expected guide-v2 to be the active replacement: %+v", st.Documents["guide-v2"])
	}
}

// TestDocumentUpdateFormPrefillsExistingContent is the regression test for
// UX-12's reproduced bug: opening the update form for an existing document
// used to start every field blank, so editing just the title meant either
// retyping the whole body from memory or silently publishing a blanked-out
// body if the operator didn't.
func TestDocumentUpdateFormPrefillsExistingContent(t *testing.T) {
	instance := newTestService(t)
	if _, err := instance.Execute("owner", "document.create", "guide-v1",
		model.DocumentPayload{Title: "Operator guide", Body: "Original body", Tags: []string{"reference", "current"}}); err != nil {
		t.Fatal(err)
	}
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Documents")
	view.rowFocus = true
	if id := view.documentList.SelectedID(view.state, view.actor); id != "guide-v1" {
		t.Fatalf("selected id = %q, want guide-v1", id)
	}
	view = pressKey(t, view, keyText("e"))
	if view.form != "document.update" {
		t.Fatalf("expected document.update form, got %q", view.form)
	}
	if got := view.inputs[0].Value(); got != "Operator guide" {
		t.Fatalf("Title field not prefilled: got %q", got)
	}
	if got := view.inputs[1].Value(); got != "Original body" {
		t.Fatalf("Body field not prefilled: got %q", got)
	}
	if got := view.inputs[2].Value(); got != "reference, current" {
		t.Fatalf("Tags field not prefilled: got %q", got)
	}
	// Editing only the title (leaving body/tags at their prefilled values)
	// must publish the unchanged body, not blank it out.
	view.inputs[0].SetValue("Operator guide (revised)")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	st, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Documents["guide-v1"]; got.Title != "Operator guide (revised)" || got.Body != "Original body" {
		t.Fatalf("editing only the title should preserve the prefilled body: %+v", got)
	}
}

// TestFormRequiredFieldErrorNamesAndFocusesTheMissingField is the
// regression test for UX-12's other reproduced bug: a missing required
// field on a multi-field form (fifteen fields on invocation request) said
// only "Complete every required field," naming nothing and leaving focus
// wherever it already was.
func TestFormRequiredFieldErrorNamesAndFocusesTheMissingField(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Documents")
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "document.create" {
		t.Fatalf("expected document.create form, got %q", view.form)
	}
	// Fill everything except Body (index 2), a required field, and submit
	// from the last field as the existing multi-tab-then-enter flow does.
	view.inputs[0].SetValue("guide-v1")
	view.inputs[1].SetValue("Operator guide")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.notice != "Body is required." {
		t.Fatalf("notice = %q, want it to name the missing field (Body is required.)", view.notice)
	}
	if view.formFocus != 2 {
		t.Fatalf("formFocus = %d, want focus moved to the missing Body field (index 2)", view.formFocus)
	}
}
