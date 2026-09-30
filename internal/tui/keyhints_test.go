package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/charmbracelet/x/ansi"
)

// TestEveryViewShowsItsOwnKeysAndTheGlobalOnes is the regression test for
// the gap this bar exists to close: only some views printed the keys they
// honored (row lists, Drafts, Project settings and a hardcoded strip on
// the overview), so Blockers, Audit & health, Activity and Archive search
// showed none at all, and no view showed the full global set. Renders
// every view for real and checks both halves are on screen.
func TestEveryViewShowsItsOwnKeysAndTheGlobalOnes(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	for _, name := range views {
		m, err := New(s, "owner")
		if err != nil {
			t.Fatal(err)
		}
		m.width, m.height = 140, 40
		m.openView(name)
		rendered := m.View().Content
		hints := m.contextHints()
		if len(hints) == 0 {
			t.Errorf("%s: no contextual keys at all", name)
			continue
		}
		if !strings.Contains(rendered, hints[0].label) {
			t.Errorf("%s: contextual key %q %q missing from the rendered view", name, hints[0].key, hints[0].label)
		}
		// The globals live in the sidebar, which every view renders.
		for _, want := range []string{"cmds", "refresh", "quit"} {
			if !strings.Contains(rendered, want) {
				t.Errorf("%s: global key %q missing from the rendered view", name, want)
			}
		}
	}
}

// TestGlobalHintsFollowTheFocusMode: ↑/↓ moves the hub cursor from
// navigation mode and the row cursor from inside a tab, and the sidebar
// used to insist on "↑↓ hub" in both -- there was no indicator anywhere
// for which of the two modes you were actually in.
func TestGlobalHintsFollowTheFocusMode(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 40
	// Plain text with runs of spaces collapsed: the legend is an aligned
	// grid, so the key and its label are styled separately and the key
	// column is padded to the widest key ("esc" in an entered tab).
	legend := func(m Model) string {
		return strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
	}
	if !strings.Contains(legend(m), "↑↓ hub") {
		t.Error("navigation mode should say ↑↓ moves the hub")
	}
	m = enterAgentsView(t, m)
	rendered := legend(m)
	if !strings.Contains(rendered, "↑↓ rows") {
		t.Error("an entered tab should say ↑↓ moves the rows")
	}
	if strings.Contains(rendered, "↑↓ hub") {
		t.Error("an entered tab must not still claim ↑↓ moves the hub")
	}
}

// TestEnteredRowViewShowsItsRowActions: the contextual half of the bar
// comes from the selected row's own Actions, the same list updateRowList
// dispatches on -- not a hardcoded strip that can drift from it.
func TestEnteredRowViewShowsItsRowActions(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 160, 40
	m = enterAgentsView(t, m)
	list := m.activeRowList()
	actions := list.Actions(list.SelectedID(m.state, m.actor), m.state, m.actor)
	if len(actions) == 0 {
		t.Fatal("expected the selected agent row to offer actions")
	}
	rendered := m.View().Content
	for _, act := range actions {
		if !strings.Contains(rendered, act.Label) {
			t.Errorf("row action %q (%s) missing from the key bar", act.Label, act.Key)
		}
	}
}

// TestCreateFormsOpenWhatTheyAdvertise checks the shared hint/opener table,
// including views where [n] must have no effect.
func TestCreateFormsOpenWhatTheyAdvertise(t *testing.T) {
	s := newTestService(t)
	for _, name := range views {
		m, err := New(s, "owner")
		if err != nil {
			t.Fatal(err)
		}
		m.openView(name)
		next, _ := m.openCreateForm()
		opened := next.(Model).form != ""
		spec, labelled := createForms[name]
		if opened != labelled {
			t.Errorf("%s: openCreateForm opens a form = %v, createForms has it = %v", name, opened, labelled)
		}
		if labelled && (spec.label == "" || (!spec.task && (spec.form == nil || spec.command == ""))) {
			t.Errorf("%s: incomplete create form specification: %+v", name, spec)
		}
	}
}

// TestKeyBarIsDroppedRatherThanPushingContentOffScreen: on a terminal too
// short to afford it, the bar must yield. A bar that stayed would be the
// thing rendering past the last row of the screen -- the failure
// bodyLayout's floors exist to rule out.
func TestKeyBarIsDroppedRatherThanPushingContentOffScreen(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	p := colors()
	m.width, m.height = 80, 8
	if bar := m.bodySuffix(p); bar != "" {
		t.Fatalf("expected no key bar on an 8-line terminal, got %q", bar)
	}
	if got := m.bodySuffixHeight(p); got != 0 {
		t.Fatalf("a bar that isn't rendered must reserve 0 lines, got %d", got)
	}
	m.height = 40
	if m.bodySuffix(p) == "" {
		t.Fatal("expected a key bar on a 40-line terminal")
	}
	if got := m.bodySuffixHeight(p); got != 1 {
		t.Fatalf("expected the bar to reserve exactly its own single line, got %d", got)
	}
}

// TestNothingPaintsItsOwnBackground is the regression test for every
// filled surface the TUI used to draw over the terminal: the body pane's
// dark-grey slab, the sidebar's black column, and the command palette's
// black card and backdrop. On a terminal with a background image each
// one was a rectangle punched out of it. Colored badges (ink on cyan or
// red) are the deliberate exception -- those are meaning, not chrome.
// Unconditional: there is no terminal, light or dark, where the TUI
// paints a surface of its own.
func TestNothingPaintsItsOwnBackground(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 40
	fills := map[string]string{
		"black":          "48;2;0;0;0",    // the former sidebar and palette fill
		"dark grey":      "48;2;17;17;17", // the former query-box fill
		"low-contrast":   "48;2;13;32;36", // the body pane's original slab
		"low-contrast 2": "48;2;7;18;22",  // and the sidebar's
	}
	check := func(what, rendered string) {
		for name, escape := range fills {
			if strings.Contains(rendered, escape) {
				t.Errorf("%s: still paints a %s background", what, name)
			}
		}
	}
	for _, name := range views {
		m.openView(name)
		check(name, m.View().Content)
	}
	m.openView("Agents")
	m.rowFocus = true
	check("focused Agents", m.View().Content)
	m.palette = true
	check("command palette", m.View().Content)
}

// TestHintGridStaysInsideTheSidebarAndAlignsItsColumns: the sidebar's key
// legend used to be packed greedily into ragged lines ("↑↓ hub  ←→ tab
// ⏎ open" over "/ cmds  r refresh") with no column to scan down. It is a
// grid now -- and a grid is only a grid if every row starts its columns in
// the same place, at every width, without spilling past the sidebar.
func TestHintGridStaysInsideTheSidebarAndAlignsItsColumns(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	p := colors()
	for _, width := range []int{8, 12, 14, 18, 25} {
		for _, entered := range []bool{false, true} {
			m.rowFocus = entered
			hints := m.globalHints()
			lines := hintGrid(p, hints, width)
			if len(lines) == 0 {
				t.Fatalf("width %d: expected some global hints", width)
			}
			labelColumn := -1
			for _, line := range lines {
				plain := ansi.Strip(line)
				if got := lipgloss.Width(plain); got > width {
					t.Fatalf("width %d: hint row spans %d columns: %q", width, got, plain)
				}
				if strings.Contains(plain, "…") {
					continue // truncated at a width too narrow for any grid
				}
				// The first label starts at the same column on every row.
				first := strings.Fields(plain)
				if len(first) < 2 {
					t.Fatalf("width %d: malformed hint row %q", width, plain)
				}
				column := lipgloss.Width(plain[:strings.Index(plain, first[1])])
				if labelColumn == -1 {
					labelColumn = column
				} else if column != labelColumn {
					t.Errorf("width %d entered=%v: label column moves from %d to %d on %q", width, entered, labelColumn, column, plain)
				}
			}
			// Nothing dropped: every key is still somewhere in the grid.
			joined := ansi.Strip(strings.Join(lines, "\n"))
			for _, hint := range hints {
				if width >= 18 && !strings.Contains(joined, hint.label) {
					t.Errorf("width %d entered=%v: %q missing from the legend", width, entered, hint.label)
				}
			}
		}
	}
	// Wide enough for two columns, the grid uses them.
	if two := hintGrid(p, m.globalHints(), 25); len(two) >= len(m.globalHints()) {
		t.Errorf("at 25 columns the legend should pair its hints, got %d rows for %d hints", len(two), len(m.globalHints()))
	}
}

func TestRenderHintRowStaysOnOneLine(t *testing.T) {
	p := colors()
	hints := []keyHint{{"[a]", "alpha"}, {"[b]", "bravo"}, {"[c]", "charlie"}, {"[d]", "delta"}}
	for _, width := range []int{6, 10, 20, 40, 80} {
		row := renderHintRow(p, hints, width)
		if strings.Contains(row, "\n") {
			t.Fatalf("width %d: key row wrapped onto a second line: %q", width, row)
		}
		if got := lipgloss.Width(row); got > width {
			t.Fatalf("width %d: key row spans %d columns: %q", width, got, row)
		}
	}
}
