package tui

import "testing"

func TestEnvSetUpdateThenDelete(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Environment")
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "env.set" {
		t.Fatalf("expected env.set form, got %q", view.form)
	}
	view.inputs[0].SetValue("LOG_LEVEL")
	view.inputs[1].SetValue("info")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatalf("set failed: %v", view.err)
	}
	st, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Env["LOG_LEVEL"].Value != "info" {
		t.Fatalf("expected LOG_LEVEL=info, got %+v", st.Env["LOG_LEVEL"])
	}

	view.rowFocus = true
	if id := view.envList.SelectedID(view.state, view.actor); id != "LOG_LEVEL" {
		t.Fatalf("selected id = %q, want LOG_LEVEL", id)
	}
	view = pressKey(t, view, keyText("e"))
	if view.form != "env.set" {
		t.Fatalf("expected env.set update form, got %q", view.form)
	}
	view.inputs[0].SetValue("debug")
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatalf("update failed: %v", view.err)
	}
	st, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if st.Env["LOG_LEVEL"].Value != "debug" {
		t.Fatalf("expected LOG_LEVEL=debug after update, got %+v", st.Env["LOG_LEVEL"])
	}

	view.rowFocus = true
	view = pressKey(t, view, keyText("x"))
	if view.err != nil {
		t.Fatalf("delete failed: %v", view.err)
	}
	st, err = instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := st.Env["LOG_LEVEL"]; exists {
		t.Fatalf("expected LOG_LEVEL to be deleted, still present: %+v", st.Env["LOG_LEVEL"])
	}
}
