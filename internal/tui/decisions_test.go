package tui

import "testing"

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
	if view.form != "decision.create" {
		t.Fatalf("expected decision.create form, got %q", view.form)
	}
	values := []string{"decision-001", "Adopt trunk-based development", "All work lands on main", ""}
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
	if st.Decisions["decision-001"].Status != "ACTIVE" {
		t.Fatalf("decision not created as expected: %+v", st.Decisions["decision-001"])
	}

	view.rowFocus = true
	if id := view.decisionList.SelectedID(view.state, view.actor); id != "decision-001" {
		t.Fatalf("selected id = %q, want decision-001", id)
	}
	view = pressKey(t, view, keyText("s"))
	if view.form != "decision.supersede" {
		t.Fatalf("expected decision.supersede form, got %q", view.form)
	}
	values = []string{"decision-002", "Adopt short-lived branches", "Trunk-based development caused too many conflicts", ""}
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
	if st.Decisions["decision-001"].Status != "SUPERSEDED" {
		t.Fatalf("expected decision-001 to be superseded: %+v", st.Decisions["decision-001"])
	}
	if st.Decisions["decision-002"].Status != "ACTIVE" || st.Decisions["decision-002"].Supersedes != "decision-001" {
		t.Fatalf("expected decision-002 to reference decision-001: %+v", st.Decisions["decision-002"])
	}
}
