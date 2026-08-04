package tui

import (
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestSettingsWorkspaceIsResponsiveAndActionable(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range views {
		if name == "Project settings" {
			view.view, view.cursor = index, index
		}
	}
	for _, width := range []int{140, 92, 62} {
		view.width, view.height = width, 38
		rendered := view.View().Content
		for _, expected := range []string{"Project policy", "SIGNED GOVERNANCE", "Default task lease"} {
			if !strings.Contains(rendered, expected) {
				t.Errorf("width %d missing %q", width, expected)
			}
		}
	}
	view.width = 140
	if rendered := view.View().Content; !strings.Contains(rendered, "hidden by default") {
		t.Fatal("wide settings workspace exposed no hidden-storage guidance")
	}
}

// TestSettingsSectionClickAndDoubleClick proves the Project settings view
// -- the one place mouse support was missing entirely -- now responds to
// a click (selecting a domain) and a double-click (entering it, matching
// "e"/"enter").
func TestSettingsSectionClickAndDoubleClick(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.width, m.height = 140, 38
	for index, name := range views {
		if name == "Project settings" {
			m.view, m.cursor = index, index
		}
	}
	m.focusCurrentView()
	if !m.settingsFocus {
		t.Fatal("expected entering Project settings to set settingsFocus")
	}

	p := colors(m.highContrast)
	const wantDomain = 1 // "Agents & access"
	var targetX, targetY int
	found := false
	for y := 0; y < m.height && !found; y++ {
		for x := 0; x < m.width; x++ {
			if idx, ok := m.settingsSectionAt(p, x, y); ok && idx == wantDomain {
				targetX, targetY, found = x, y, true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find the Agents & access domain's clickable position")
	}

	m = pressMsg(t, m, click(targetX, targetY))
	if m.settingsCursor != wantDomain {
		t.Fatalf("expected a click to select domain %d, got %d", wantDomain, m.settingsCursor)
	}
	if !m.settingsFocus {
		t.Fatal("a single click should not leave the settings view")
	}

	m = pressMsg(t, m, click(targetX, targetY))
	if views[m.view] != "Agents" {
		t.Fatalf("expected double-click on Agents & access to open Agents, got %q", views[m.view])
	}
	if !m.rowFocus || m.settingsFocus {
		t.Fatalf("expected double-click to leave settings for row focus, got rowFocus=%v settingsFocus=%v", m.rowFocus, m.settingsFocus)
	}
}

func TestSettingsWheelScrollsDomainSelection(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	for index, name := range views {
		if name == "Project settings" {
			m.view, m.cursor = index, index
		}
	}
	m.focusCurrentView()
	if m.settingsCursor != 0 {
		t.Fatalf("expected settings to start on domain 0, got %d", m.settingsCursor)
	}
	m = pressMsg(t, m, wheelDown())
	if m.settingsCursor != 1 {
		t.Fatalf("expected wheel-down to move to domain 1, got %d", m.settingsCursor)
	}
	m = pressMsg(t, m, wheelUp())
	if m.settingsCursor != 0 {
		t.Fatalf("expected wheel-up to return to domain 0, got %d", m.settingsCursor)
	}
}

func TestSettingsFormPublishesSignedPolicy(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := view.openProjectSettingsForm()
	view = next.(Model)
	values := []string{"2h", "20m", "336h", "2400", "7", "true"}
	for index, value := range values {
		view.inputs[index].SetValue(value)
	}
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.confirm == nil {
		t.Fatal("shared setting change did not require confirmation")
	}
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	want := model.ProjectSettings{
		DefaultLease: "2h", StaleGrace: "20m", ActiveRetention: "336h",
		SummaryLimit: 2400, ArtifactLimitBytes: 7 * 1024 * 1024, RequireReview: true,
	}
	got := state.ProjectSettings
	got.UpdatedAt = want.UpdatedAt
	got.UpdatedBy = want.UpdatedBy
	if got != want {
		t.Fatalf("settings=%+v, want %+v", got, want)
	}
}
