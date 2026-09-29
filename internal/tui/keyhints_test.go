package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
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
	if got := m.View().Content; !strings.Contains(got, "↑↓ hub") {
		t.Error("navigation mode should say ↑↓ moves the hub")
	}
	m = enterAgentsView(t, m)
	rendered := m.View().Content
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

// TestCreateHintLabelsMatchOpenCreateForm holds the bar's "[n] new ..."
// labels to the views openCreateForm actually handles. Without it the two
// drift silently in both directions: a view missing from the map loses a
// working key from the bar, and a view listed there that openCreateForm
// ignores advertises a dead one -- exactly what the overview's old
// hardcoded "[n] create" did.
func TestCreateHintLabelsMatchOpenCreateForm(t *testing.T) {
	s := newTestService(t)
	for _, name := range views {
		m, err := New(s, "owner")
		if err != nil {
			t.Fatal(err)
		}
		m.openView(name)
		next, _ := m.openCreateForm()
		opened := next.(Model).form != ""
		_, labelled := createHintLabels[name]
		if opened != labelled {
			t.Errorf("%s: openCreateForm opens a form = %v, createHintLabels has it = %v", name, opened, labelled)
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
	p := colors(m.highContrast)
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

// TestBodyPaneKeepsTheTerminalBackground is the regression test for the
// dark grey slab: the body pane used to paint p.panel over the whole
// right-hand side, which fought every terminal theme that wasn't that
// shade. Checks the palette color's own 24-bit background escape appears
// nowhere in an ordinary render.
func TestBodyPaneKeepsTheTerminalBackground(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 140, 40
	// #0D2024, the low-contrast palette's panel fill.
	const panelBackground = "48;2;13;32;36"
	for _, name := range views {
		m.openView(name)
		if strings.Contains(m.View().Content, panelBackground) {
			t.Fatalf("%s: body still paints its own panel background", name)
		}
	}
}

func TestPackHintsNeverExceedsTheSidebarWidth(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{8, 12, 14, 16, 19, 21} {
		for _, entered := range []bool{false, true} {
			m.rowFocus = entered
			lines := packHints(m.globalHints(), width)
			if len(lines) == 0 {
				t.Fatalf("width %d: expected some global hints", width)
			}
			for _, line := range lines {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("width %d: hint line spans %d columns: %q", width, got, line)
				}
			}
		}
	}
}

func TestRenderHintRowStaysOnOneLine(t *testing.T) {
	p := colors(false)
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
