package tui

import "testing"

// RFC 0029: decisions are `decision`-tagged documents. The
// "Contracts & decisions" view keeps its create/supersede flow but
// dispatches document.* and stores into State.Documents.
func TestDecisionCreateThenSupersede(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Contracts & decisions")
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "document.create" {
		t.Fatalf("expected document.create form, got %q", view.form)
	}
	values := []string{"decision-001", "Adopt trunk-based development", "All work lands on main"}
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
	d := st.Documents["decision-001"]
	if d.Status != "ACTIVE" || !isDecisionDoc(d) {
		t.Fatalf("decision-tagged document not created as expected: %+v", d)
	}

	view.rowFocus = true
	if id := view.decisionList.SelectedID(view.state, view.actor); id != "decision-001" {
		t.Fatalf("selected id = %q, want decision-001", id)
	}
	view = pressKey(t, view, keyText("s"))
	if view.form != "document.supersede" {
		t.Fatalf("expected document.supersede form, got %q", view.form)
	}
	values = []string{"decision-002", "Adopt short-lived branches", "Trunk-based development caused too many conflicts"}
	for i, v := range values {
		view.inputs[i].SetValue(v)
	}
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	st, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Documents["decision-001"].Status != "SUPERSEDED" {
		t.Fatalf("expected decision-001 to be superseded: %+v", st.Documents["decision-001"])
	}
	if got := st.Documents["decision-002"]; got.Status != "ACTIVE" || !isDecisionDoc(got) {
		t.Fatalf("expected decision-002 to be an active decision-tagged document: %+v", got)
	}
}
