package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactAddThroughGuidedForm(t *testing.T) {
	instance := newTestService(t)
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello from the TUI"), 0o600); err != nil {
		t.Fatal(err)
	}

	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Artifacts")
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	if view.form != "artifact.add" {
		t.Fatalf("expected artifact.add form, got %q", view.form)
	}
	view.inputs[0].SetValue(path)
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatalf("add artifact failed: %v", view.err)
	}

	st, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d: %+v", len(st.Artifacts), st.Artifacts)
	}
	rendered := view.View().Content
	if !strings.Contains(rendered, "notes.txt") {
		t.Fatalf("artifact list missing notes.txt:\n%s", rendered)
	}
}
