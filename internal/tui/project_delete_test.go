package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestDangerZoneFormOpensFromAuthorityAndDataDomain confirms the TUI entry
// point RFC 0020 specifies: "Authority & data" (settingsSections index 3)
// -- previously a no-op on enter/double-click, per enterSettingsDomain's
// switch having no case for it -- now opens the Danger Zone form.
func TestDangerZoneFormOpensFromAuthorityAndDataDomain(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.enterSettingsDomain(3)
	opened, ok := next.(Model)
	if !ok {
		t.Fatal("enterSettingsDomain did not return a tui.Model")
	}
	if opened.form != "project.delete" || opened.formSpec != dangerZoneForm {
		t.Fatalf("expected the Danger Zone form to open, got form=%q formSpec=%v", opened.form, opened.formSpec)
	}
	if len(opened.inputs) != 2 {
		t.Fatalf("expected exactly the project-ID and passphrase fields, got %d inputs", len(opened.inputs))
	}
}

// TestDangerZoneDispatchWrongConfirmationRefuses confirms a mismatched
// typed project ID surfaces as m.err, exactly like every other form
// failure, and never quits the program.
func TestDangerZoneDispatchWrongConfirmationRefuses(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := dangerZoneForm.Dispatch(m, []string{"not-the-real-project-id"}, "whatever")
	result, ok := next.(Model)
	if !ok {
		t.Fatal("Dispatch did not return a tui.Model")
	}
	if result.err == nil {
		t.Fatal("expected a mismatched confirmation to set m.err")
	}
	if result.exitNotice != "" {
		t.Fatalf("expected no exit notice on a refused delete, got %q", result.exitNotice)
	}
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("a refused delete must never quit the program")
		}
	}
}

// TestDangerZoneDispatchSucceedsAndQuits is the full happy path: correct
// project-ID confirmation and elevated-key passphrase actually deletes the
// project (exercised through the exact same Service.DeleteProject already
// covered end to end in internal/service) and, since the Store this Model
// was built against no longer exists afterward, quits the program with a
// final exit notice -- there is no view left to return to.
func TestDangerZoneDispatchSucceedsAndQuits(t *testing.T) {
	s := newTestService(t)
	if _, err := s.ElevateKey("owner", "correct passphrase"); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, cmd := dangerZoneForm.Dispatch(m, []string{m.projectID}, "correct passphrase")
	result, ok := next.(Model)
	if !ok {
		t.Fatal("Dispatch did not return a tui.Model")
	}
	if result.err != nil {
		t.Fatalf("expected a clean success, got err: %v", result.err)
	}
	if !strings.Contains(result.exitNotice, "deleted") {
		t.Fatalf("expected an exit notice describing the deletion, got %q", result.exitNotice)
	}
	if cmd == nil {
		t.Fatal("expected a quit command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("expected a successful delete to quit the program")
	}
}
