package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The single place that answers "what can I press right now?" -- a two-row
// bar at the foot of the body pane: the keys this specific tab actually
// honors in its current mode, then the keys that work everywhere.
//
// It replaces four independently maintained per-view key strips (the
// overview's hardcoded one, RowList.View's footer, draftsView's and
// projectSettings'), which between them covered some tabs and not others
// and had already drifted from what Update() really handles -- the old
// overview strip advertised "[n] create" on a view where openCreateForm
// has no case at all, so the key did nothing. Deriving every contextual
// hint from the same source the key handler reads (a row's own Actions,
// createForms) is what keeps that from coming back.

type keyHint struct{ key, label string }

// contextHints are the keys that mean something in the *current* tab and
// mode only. Deliberately mode-aware: a row-list tab that has not been
// entered yet doesn't honor its row actions, so advertising them there
// (the way the old always-on footers did) told the user to press keys
// that would move the hub cursor instead.
func (m Model) contextHints() []keyHint {
	entered := m.rowFocus || m.settingsFocus
	view := views[m.view]
	switch view {
	case "Overview":
		return []keyHint{{"[g]", "agents"}, {"[i]", "invocations"}, {"[pgup/pgdn]", "scroll"}}
	case "Blockers", "Audit & health", "Activity", "Archive search":
		return []keyHint{{"[pgup/pgdn]", "scroll"}, {"[r]", "refresh"}}
	case "Project settings":
		if !entered {
			return []keyHint{{"[enter]", "manage settings"}}
		}
		return []keyHint{
			{"[↑/↓]", "domain"}, {"[e/enter]", "manage"},
			{"[g]", "agents"}, {"[r]", "runtimes"},
		}
	case "Drafts":
		create := keyHint{"[n]", createForms[view].label}
		if !entered {
			return []keyHint{{"[enter]", "select a draft"}, create}
		}
		return []keyHint{
			{"[↑/↓]", "select"}, {"[pgup/pgdn]", "page"},
			{"[d]", "delete selected"}, create,
		}
	}
	mutable := m
	list := mutable.activeRowList()
	if list == nil {
		return nil
	}
	// A tab that hasn't been entered yet leads with [enter], because that
	// is the key that makes the rest of this row live -- the row actions
	// after it are real, but they reach the row list only once it has
	// focus. They are still listed either way: seeing what a tab can do
	// before committing to it is the whole point of showing them, and
	// hiding them until entry is what left half the app's views with no
	// visible keys at all.
	var hints []keyHint
	if entered {
		hints = append(hints, keyHint{"[↑/↓]", "select"})
	} else {
		hints = append(hints, keyHint{"[enter]", "open " + strings.ToLower(view)})
	}
	// The selected row's own actions, from RowSource.Actions -- exactly
	// what updateRowList's default case will dispatch on, so the bar can
	// never advertise an action this row doesn't actually offer.
	for _, act := range list.Actions(list.SelectedID(m.state, m.actor), m.state, m.actor) {
		hints = append(hints, keyHint{"[" + act.Key + "]", act.Label})
	}
	hints = append(hints, keyHint{"[i]", "inspect"})
	if form, ok := createForms[view]; ok {
		hints = append(hints, keyHint{"[n]", form.label})
	}
	return hints
}

// globalHints are the keys every view honors. They render in the sidebar
// (renderSidebar), which is on screen for every view and has the vertical
// room the body pane does not -- spending two body lines on a bar that
// repeats itself on every tab is what pushed the overview's own last
// section below the fold.
//
// Mode-aware, because the *same* keys genuinely do different things in
// each: ↑/↓ walks the hub list from navigation mode and the rows from
// inside a tab, and there was no indicator on screen for which of the two
// you were in. Labels are short on purpose -- the sidebar is 21 columns
// at its widest.
func (m Model) globalHints() []keyHint {
	if m.rowFocus || m.settingsFocus {
		return []keyHint{
			{"↑↓", "rows"}, {"esc", "back"}, {"/", "cmds"},
			{"r", "refresh"}, {"?", "help"}, {"q", "quit"},
		}
	}
	return []keyHint{
		{"↑↓", "hub"}, {"←→", "tab"}, {"⏎", "open"}, {"/", "cmds"},
		{"r", "refresh"}, {"a", "actor"}, {"?", "help"}, {"q", "quit"},
	}
}

// packHints lays hints out as plain (unstyled) lines no wider than width,
// two spaces apart, for the sidebar's narrow column. Plain, not styled:
// the packing measures each token's real width, and measuring a string
// that already carries ANSI color would count the escape bytes.
func packHints(hints []keyHint, width int) []string {
	if width <= 0 {
		return nil
	}
	var lines []string
	current := ""
	for _, hint := range hints {
		token := hint.key + " " + hint.label
		switch {
		case current == "":
			current = token
		case lipgloss.Width(current+"  "+token) <= width:
			current += "  " + token
		default:
			lines = append(lines, current)
			current = token
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	return lines
}

// renderHintRow lays hints out on exactly one physical line, dropping
// whatever doesn't fit into a "+N more" counter and truncating whatever
// still overruns. Exactly one line is a contract, not a nicety:
// bodySuffixHeight measures this bar to decide how much vertical room
// the content above it gets, and a row that silently wrapped would make
// the body taller than the terminal -- the same defect the row list's
// own footer once had.
func renderHintRow(p palette, hints []keyHint, width int) string {
	if len(hints) == 0 || width <= 0 {
		return ""
	}
	keyStyle := lipgloss.NewStyle().Foreground(p.cyan).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(p.muted)
	parts := make([]string, 0, len(hints))
	hidden := 0
	for _, hint := range hints {
		rendered := keyStyle.Render(hint.key) + " " + labelStyle.Render(hint.label)
		if lipgloss.Width(strings.Join(append(parts, rendered), " · ")) <= width {
			parts = append(parts, rendered)
			continue
		}
		hidden++
	}
	if hidden > 0 {
		more := labelStyle.Render(fmt.Sprintf("+%d more", hidden))
		if lipgloss.Width(strings.Join(append(parts, more), " · ")) <= width {
			parts = append(parts, more)
		}
	}
	return ansi.Truncate(strings.Join(parts, " · "), width, "…")
}

// keyBar is the body pane's own bar: exactly one line, carrying only the
// keys specific to the current tab. The global keys live in the sidebar
// instead, so this costs the content one line rather than three.
func (m Model) keyBar(p palette, width int) string {
	return renderHintRow(p, m.contextHints(), width)
}

// minContentLinesWithKeyBar is how many lines of real content must survive
// for the key bar to be worth its own space. Below that the bar is what
// would be pushing the content off the screen, which is a worse trade
// than not showing it -- the same "never render more lines than the
// terminal has" rule bodyLayout's floors exist for.
const minContentLinesWithKeyBar = 3

// bodySuffix returns the key bar renderBody will actually append, or ""
// when the terminal is too short to spend the lines. Shared with
// bodySuffixHeight (and so with bodyLayout's innerH) so the height
// reserved for the bar and the bar actually rendered can never disagree.
func (m Model) bodySuffix(p palette) string {
	if m.form != "" || m.confirm != nil {
		// Forms and confirm dialogs carry their own footers, and none of
		// the global keys apply while typing into one -- keystrokes go
		// into the field, not to Update's navigation switch.
		return ""
	}
	bar := m.keyBar(p, m.contentWidth())
	if bar == "" {
		return ""
	}
	paneH := max(4, m.height)
	if paneH-m.bodyPrefixHeight(p)-1-lipgloss.Height(bar) < minContentLinesWithKeyBar {
		return ""
	}
	return bar
}

// bodySuffixHeight is how many lines bodyLayout must hold back for the key
// bar. No separator line is counted: renderBody pins the bar to the foot
// of the pane, so the padding above it is whatever content didn't use.
func (m Model) bodySuffixHeight(p palette) int {
	return lipglossHeightOrZero(m.bodySuffix(p))
}

// lipgloss.Height reports 1 for the empty string; a bar that isn't
// rendered must cost 0 lines, and bodyLayout subtracting a phantom line
// would shrink every view by one row for nothing.
func lipglossHeightOrZero(s string) int {
	if s == "" {
		return 0
	}
	return lipgloss.Height(s)
}
