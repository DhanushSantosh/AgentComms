package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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

// TestSettingsControlNeverExceedsRequestedWidth is the regression test for
// a real, reported bug: "this UI is breaking" on Project settings.
// settingsControl's own bordered box relied on lipgloss's Width()-triggered
// implicit wrap for its multi-line content, which is confirmably wrong for
// specific (width, text) pairs -- e.g. the Environment domain's own
// description rendered several columns *wider* than requested at widths
// 47-49 specifically (correct at every other width from 40-56 tested
// around it), because of how lipgloss handles a hyphenated word landing
// near a wrap boundary. The widened box then got clipped by the outer
// screen's own MaxWidth(m.width), splitting its own border mid-line --
// exactly the visual corruption reported. Fixed by having wrapText
// pre-wrap every long plain-text line before any styling or box width is
// applied (see wrapText's own doc comment), rather than trusting the box's
// own implicit wrap.
//
// Sweeps every domain (each has different, differently-shaped description
// text) across a wide range of widths -- not just the one exact size
// reported live -- since the underlying lipgloss quirk is sensitive to the
// specific (width, text) pairing, not a single fixed threshold.
func TestSettingsControlNeverExceedsRequestedWidth(t *testing.T) {
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
	for width := 40; width <= 160; width += 3 {
		for _, height := range []int{18, 22, 26, 30, 38} {
			for domain := range settingsSections {
				m.width, m.height, m.settingsCursor = width, height, domain
				rendered := m.View().Content
				for _, line := range strings.Split(rendered, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Fatalf("width=%d height=%d domain=%d: a rendered line is %d columns wide, exceeding the terminal", width, height, domain, w)
					}
				}
			}
		}
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

// TestSettingsSectionClickAtNarrowWidths is the regression test for a real
// bug: settingsSectionAt assumed a fixed layout for the domain rail --
// "CONTROL DOMAINS" on exactly 1 line, then exactly 2 lines per domain
// (its name, then a blank) -- but domainWidth (projectSettings) has its
// own floor of 18, hit by any contentW from 72 up to just under 96 (an
// entirely ordinary window size, not some extreme edge case). At that
// floor, the interior text width left after settingsDomainRail's own
// Border(1)+Padding(1) is only 14 columns -- too narrow for "CONTROL
// DOMAINS" itself, or "Agents & access"/"Authority & data" with their
// marker -- so those wrap onto a second line, and the fixed-offset
// formula pointed at the wrong domain (or none at all) for every row
// after the first wrap. Confirmed live: at contentW=72, every domain past
// "Project policy" resolved to either the wrong index or nothing.
// Iterates every domain at that exact width and asserts a click on its
// real on-screen text selects it correctly.
func TestSettingsSectionClickAtNarrowWidths(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	// contentW = m.width - sidebarWidth(21) - 3 - 4 = 72 here, exactly
	// domainWidth's floor boundary -- the width this bug lived at.
	m.width, m.height = 100, 32
	for index, name := range views {
		if name == "Project settings" {
			m.view, m.cursor = index, index
		}
	}
	m.focusCurrentView()
	p := colors(m.highContrast)

	for wantDomain := range settingsSections {
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
			t.Fatalf("domain %d: could not find its clickable position at width 100", wantDomain)
		}
		clicked := pressMsg(t, m, click(targetX, targetY))
		if clicked.settingsCursor != wantDomain {
			t.Fatalf("domain %d: click at (%d,%d) selected domain %d instead", wantDomain, targetX, targetY, clicked.settingsCursor)
		}
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
