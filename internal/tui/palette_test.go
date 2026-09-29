package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// TestPaletteOpenedFromRowFocusDoesNotLeakKeystrokes is the regression
// test for the most serious defect this audit found: opening the command
// palette from inside a focused row list used to set m.palette=true
// without clearing m.rowFocus, so every subsequent keystroke kept routing
// straight back to updateRowList instead of the palette -- a typed
// character could silently trigger a real row action (here, "s" would
// have opened Suspend's confirm dialog) while the palette sat on screen
// showing an empty, frozen query box. Also confirms closing the palette
// returns exactly to the row-focused view it was opened from, with no
// separate state needed to restore it (see Update()'s own comment on why
// checking m.palette first, rather than resetting m.rowFocus, is what
// makes that automatic).
func TestPaletteOpenedFromRowFocusDoesNotLeakKeystrokes(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = enterAgentsView(t, m)
	m = pressKey(t, m, keyText("/"))
	if !m.palette {
		t.Fatal("expected the palette to open")
	}
	// "s" is agents.go's own suspend row-action key -- typing it into the
	// palette must never reach updateRowList.
	m = pressKey(t, m, keyText("s"))
	if m.confirm != nil {
		t.Fatalf("typing into the palette must never trigger a row action, got confirm: %+v", m.confirm)
	}
	if m.query != "s" {
		t.Fatalf("expected the keystroke to land in the query, got %q", m.query)
	}
	// "n" would normally open the create-agent form at the row-list level.
	m = pressKey(t, m, keyText("n"))
	if m.form != "" {
		t.Fatalf("typing into the palette must never open a form, got form=%q", m.form)
	}
	if m.query != "sn" {
		t.Fatalf("expected both keystrokes to accumulate in the query, got %q", m.query)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.palette {
		t.Fatal("expected esc to close the palette")
	}
	if !m.rowFocus {
		t.Fatal("expected closing the palette to return to the row-focused view it was opened from")
	}
}

// TestPaletteAcceptsSpaceInQuery is the regression test for the second
// confirmed defect: bubbletea/ultraviolet's Key.String() reports the
// spacebar as the literal word "space", never a single " " character, so
// the len(k)==1 fallback could never see it -- no multi-word command
// ("new task", "new environment key", ...) could ever actually be typed.
func TestPaletteAcceptsSpaceInQuery(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m = pressKey(t, m, keyText("n"))
	m = pressKey(t, m, keyText("e"))
	m = pressKey(t, m, keyText("w"))
	m = pressKey(t, m, keyText(" ")) // the spacebar, not the literal "space" word
	m = pressKey(t, m, keyText("t"))
	if m.query != "new t" {
		t.Fatalf("expected the query to include a real space, got %q", m.query)
	}
}

// TestPaletteEnterAppliesExactlyTheHighlightedMatch is the regression test
// for the third confirmed defect: the rendered/highlighted "top match" and
// what Enter actually executed used to come from two independently
// written matching implementations that had silently drifted apart --
// paletteMatches() (display) did a substring match over commands and view
// names combined, while the old applyPalette() (execution) required an
// *exact* match against a named command first, falling back to a
// substring match against view names *only*. Typing "new task" (now
// possible at all, per the space fix above) previously would not have hit
// either path reliably. This asserts the single-source-of-truth fix:
// whatever paletteMatches() lists first is exactly what Enter runs.
func TestPaletteEnterAppliesExactlyTheHighlightedMatch(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m.query = "new task"
	matches := m.paletteMatches()
	if len(matches) == 0 || matches[0].label != "new task" {
		t.Fatalf("expected \"new task\" to be the top match, got %+v", matches)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.palette {
		t.Fatal("expected applying a match to close the palette")
	}
	if m.form != "task.create" {
		t.Fatalf("expected the highlighted match to actually run, got form=%q", m.form)
	}
	if views[m.view] != "Tasks" {
		t.Fatalf("expected navigating to Tasks as part of applying \"new task\", got %q", views[m.view])
	}
}

// TestPaletteEnterOnEmptyQueryIsANoOp preserves the original behavior: an
// accidental Enter before typing anything must never silently apply
// whatever paletteMatches() lists first for an empty filter.
func TestPaletteEnterOnEmptyQueryIsANoOp(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if !m.palette {
		t.Fatal("expected Enter on an empty query to leave the palette open, not apply a match")
	}
	if m.form != "" {
		t.Fatalf("expected no form to open, got %q", m.form)
	}
}

// TestPaletteMatchClickAppliesIt is the regression test for the fourth
// confirmed defect: the palette had zero mouse support at all, unlike
// every other surface in the app (sidebar, hub tabs, row table, form
// fields, confirm dialogs). Computes the exact screen position of the
// first match row the same way a real click would need to (paletteLayout's
// own row offsets plus centerOffset's replication of lipgloss.Place's
// centering math) and simulates clicking it.
func TestPaletteMatchClickAppliesIt(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 30
	m.palette = true
	p := colors(m.highContrast)
	panel, matchLine := m.paletteLayout(p)
	if len(matchLine) == 0 {
		t.Fatal("expected at least one match for an empty query")
	}
	left := centerOffset(m.width, lipgloss.Width(panel))
	top := centerOffset(m.height, lipgloss.Height(panel))
	m = pressMsg(t, m, tea.MouseClickMsg{X: left + 2, Y: top + matchLine[0], Button: tea.MouseLeft})
	if m.palette {
		t.Fatal("expected clicking a match to close the palette")
	}
	if m.form != "task.create" {
		t.Fatalf("expected clicking the first match (new task) to open its form, got form=%q", m.form)
	}
}

// TestPaletteClickElsewhereClosesItAndNavigates is the regression test for
// the fifth and sixth confirmed defects together: sidebar and hub-tab
// clicks used to run unconditionally, before any mode check at all, so
// clicking away from an open palette silently navigated underneath it
// without ever closing it -- the palette stayed visibly on screen, frozen,
// on top of a view that had already changed. A click that misses every
// match row must close the palette and let the click reach its ordinary
// target instead.
func TestPaletteClickElsewhereClosesItAndNavigates(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 30
	m.palette = true
	m.query = "something nothing on screen matches"
	p := colors(m.highContrast)
	_, paneH, _, _ := m.bodyLayout(p)
	_, hubLine := m.renderSidebar(p, m.sidebarWidth(), paneH)
	if len(hubLine) == 0 {
		t.Fatal("expected at least one clickable sidebar hub")
	}
	m = pressMsg(t, m, tea.MouseClickMsg{X: 2, Y: 1 + hubLine[0], Button: tea.MouseLeft})
	if m.palette {
		t.Fatal("expected clicking a sidebar hub to close the still-open palette, not leave it stuck on screen")
	}
	if m.query != "" {
		t.Fatalf("expected the abandoned query to be cleared, got %q", m.query)
	}
}

// TestPaletteDownThenEnterSelectsTheSecondMatch is the regression test for
// UX-11's own reproduction: typing "/new" showed six matches, but Down
// then Enter still opened the first ("new task") -- the palette had no
// selection cursor at all, and Enter always applied matches[0] regardless
// of what was highlighted (there was nothing highlighted differently in
// the first place).
func TestPaletteDownThenEnterSelectsTheSecondMatch(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m.query = "new"
	matches := m.paletteMatches()
	if len(matches) < 2 {
		t.Fatalf("expected at least two matches for \"new\", got %+v", matches)
	}
	second := matches[1].label

	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.paletteSelectedIndex() != 1 {
		t.Fatalf("expected Down to move the selection to index 1, got %d", m.paletteSelectedIndex())
	}
	m = pressKey(t, m, keyEnter())
	if m.palette {
		t.Fatal("expected applying a match to close the palette")
	}
	if matches[0].label == "new task" && second != "new task" && m.form == "task.create" {
		t.Fatal("expected Down to move off \"new task\" before Enter, but the first match's form still opened")
	}
}

// TestPaletteUpDoesNotGoBelowZero confirms Up is a no-op at the top of the
// list rather than wrapping or going negative (which would make
// paletteSelectedIndex's clamp mask a real bug).
func TestPaletteUpDoesNotGoBelowZero(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m.query = "new"
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if m.paletteSelected != 0 {
		t.Fatalf("expected Up at the top to stay at 0, got %d", m.paletteSelected)
	}
}

// TestPaletteAcceptsUnicodeInput is the regression test for UX-11's
// Unicode concern: the query-append branch matched on len(k) == 1, a byte
// length, so any multi-byte UTF-8 rune (accented letters, CJK, emoji) was
// silently dropped -- never appended to the query at all.
func TestPaletteAcceptsUnicodeInput(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m = pressKey(t, m, keyText("é"))
	if m.query != "é" {
		t.Fatalf("expected a single accented character to be accepted, got query=%q", m.query)
	}
	m = pressKey(t, m, keyText("新"))
	if m.query != "é新" {
		t.Fatalf("expected a CJK character to be accepted, got query=%q", m.query)
	}
	// Backspace must remove one whole rune, not corrupt the string by
	// truncating a multi-byte character's trailing bytes.
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if m.query != "é" {
		t.Fatalf("expected backspace to remove exactly one rune, got query=%q", m.query)
	}
}

// TestPaletteWithNoInputListsEveryCommand: opening the palette used to
// show six rows and stop -- paletteMatches hard-capped at 6, so the other
// twenty-two commands and views were undiscoverable from the one surface
// built to discover them. An empty query now means "everything".
func TestPaletteWithNoInputListsEveryCommand(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	matches := m.paletteMatches()
	if want := len(paletteCommands()) + len(views); len(matches) != want {
		t.Fatalf("empty query listed %d commands, want all %d", len(matches), want)
	}
	labels := map[string]bool{}
	for _, match := range matches {
		labels[match.label] = true
	}
	for _, cmd := range paletteCommands() {
		if !labels[cmd.label] {
			t.Errorf("command %q missing from the empty-query list", cmd.label)
		}
	}
	for _, name := range views {
		if !labels[name] {
			t.Errorf("view %q missing from the empty-query list", name)
		}
	}
}

// TestPaletteFilteredListIsNotCappedEither: the same cap silently hid
// matches past the sixth for a typed query too -- "new" matches all ten
// named commands, and four of them could never be reached.
func TestPaletteFilteredListIsNotCappedEither(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	m.query = "new"
	if got := len(m.paletteMatches()); got < len(paletteCommands()) {
		t.Fatalf(`query "new" listed %d matches, want at least the %d named commands`, got, len(paletteCommands()))
	}
}

// TestPalettePanelNeverOutgrowsTheTerminal: listing everything only helps
// if the panel still fits the screen. paletteLayout windows the list;
// this proves the rendered panel never exceeds the terminal's own height
// at any size, with the full (empty-query) list open.
func TestPalettePanelNeverOutgrowsTheTerminal(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.palette = true
	// From 16 lines up: below that the panel's own frame (border,
	// padding, title, query box, footer) is already taller than the
	// terminal no matter how small the match window gets.
	for _, height := range []int{16, 18, 20, 24, 30, 45, 60} {
		m.width, m.height = 100, height
		panel, _ := m.paletteLayout(colors(m.highContrast))
		if got := lipgloss.Height(panel); got > height {
			t.Errorf("height %d: palette panel renders %d lines", height, got)
		}
	}
}

// TestPaletteWindowFollowsTheSelection: a windowed list must scroll to
// whatever is selected, or Down past the window's edge would highlight a
// row nobody can see (and click hit-testing would hand the click to the
// wrong match).
func TestPaletteWindowFollowsTheSelection(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 26
	m.palette = true
	total := len(m.paletteMatches())
	visible := paletteVisibleCount(m.height)
	if total <= visible {
		t.Fatalf("test needs a list taller than the window: %d matches, %d visible", total, visible)
	}
	m.paletteSelected = total - 1
	_, matchLine := m.paletteLayout(colors(m.highContrast))
	if matchLine[total-1] < 0 {
		t.Fatal("the selected match scrolled out of the rendered window")
	}
	if matchLine[0] >= 0 {
		t.Fatal("expected the window to have scrolled off the first match")
	}
	m.paletteSelected = 0
	_, matchLine = m.paletteLayout(colors(m.highContrast))
	if matchLine[0] < 0 {
		t.Fatal("expected the first match to be visible when it is selected")
	}
}

// TestPaletteClickIgnoresScrolledOutMatches: off-window matches carry -1
// in matchLine, and a click above the panel produces a negative relative
// Y -- without an explicit guard the two meet and a stray click applies
// a command the user cannot even see.
func TestPaletteClickIgnoresScrolledOutMatches(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 100, 26
	m.palette = true
	m.paletteSelected = len(m.paletteMatches()) - 1
	p := colors(m.highContrast)
	panel, _ := m.paletteLayout(p)
	top := centerOffset(m.height, lipgloss.Height(panel))
	if _, ok := m.paletteMatchAt(p, m.width/2, top-1); ok {
		t.Fatal("a click above the panel must not resolve to a scrolled-out match")
	}
}
