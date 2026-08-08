package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// wellFormedANSISequence matches a complete CSI escape sequence (ESC '['
// followed by parameter bytes and a final letter) -- stripping every
// well-formed occurrence and checking for a leftover ESC byte is how
// TestSidebarTitleSurvivesCompactFallback distinguishes real corruption
// from an ordinary, valid 24-bit color code: a naive substring check like
// `strings.Contains(v, "[38;2;")` would flag literally every colored line
// lipgloss ever renders, since that is exactly what a well-formed truecolor
// sequence looks like.
var wellFormedANSISequence = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// TestProjectControlResponsiveViews checks sizes roomy enough that every
// Overview section is guaranteed visible without scrolling -- 80x24 used to
// be in this list too, back when the page-level scroll window's height
// floor (see renderBody's own history, fixed alongside
// TestOverviewScrollReachesTrueEndOnASmallTerminal) silently claimed more
// room than a terminal that size actually has. At 80x24 LIVE ACTIVITY
// genuinely doesn't fit without scrolling now -- correct, not a
// regression, and covered separately by the scroll-based test.
func TestProjectControlResponsiveViews(t *testing.T) {
	s := newTestService(t)
	for _, size := range [][2]int{{140, 40}, {100, 30}} {
		v, e := RenderForTest(s, "owner", size[0], size[1])
		if e != nil {
			t.Fatal(e)
		}
		for _, want := range []string{"AGENT COMMS", "PROJECT CONTROL", "AGENT WORKFORCE", "ATTENTION", "LIVE ACTIVITY"} {
			if !strings.Contains(v, want) {
				t.Errorf("%dx%d missing %q", size[0], size[1], want)
			}
		}
	}
}

// TestSidebarTitleSurvivesCompactFallback is the regression test for a real
// bug: renderSidebar's compact fallback (len(rows)+2 > h, the branch that
// drops spacer lines and keybinding hints on a short terminal) truncated
// the sidebar's title AFTER it had already been styled -- truncate() slices
// runes with no notion of ANSI escape codes, so it chopped straight through
// the middle of a 24-bit color escape sequence as readily as through the
// visible "● AGENT COMMS" text, leaking a raw, unterminated code like
// "[1;38;2;0;25…" onto the screen in place of the title. Confirmed live at
// 60x22 in the real project before the fix; every size here is well below
// the ~23-25 row threshold where the fallback kicks in.
func TestSidebarTitleSurvivesCompactFallback(t *testing.T) {
	s := newTestService(t)
	for _, size := range [][2]int{{60, 22}, {45, 18}, {30, 12}, {24, 8}} {
		v, e := RenderForTest(s, "owner", size[0], size[1])
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(v, "AGENT COM") {
			t.Errorf("%dx%d: sidebar title missing or corrupted, got:\n%s", size[0], size[1], v)
		}
		if stripped := wellFormedANSISequence.ReplaceAllString(v, ""); strings.ContainsRune(stripped, '\x1b') {
			t.Errorf("%dx%d: rendered output contains a malformed/unterminated ANSI escape sequence", size[0], size[1])
		}
	}
}

// TestOverviewScrollReachesTrueEndOnASmallTerminal is the regression test
// for a second, related bug: renderBody's page-level scroll window for
// every non-table view (Overview, Blockers, Audit & health, Activity,
// Archive search) floored its available height at a comfortable desktop
// constant (22) instead of the real terminal size -- unlike
// visibleRowCount's identical-shaped max(0, h-4) for row-list views, which
// got this right. On a terminal short enough that contentH-4 fell under 22,
// the page believed it had more room than it did, so even scrolling all the
// way to the reported maxScroll still rendered past the real screen edge:
// content existed but its own trailing lines (here, the "[g] agents ..."
// key-hint row Overview always ends with) were permanently unreachable, no
// matter how far down a user scrolled -- confirmed live at 80x16 before the
// fix. This drives real PgDn key presses (not just fixed-size snapshots
// like TestSmallTerminalNeverRendersMoreLinesThanItHas covers) and checks
// both halves: the render never exceeds the terminal's real height at any
// scroll position, and scrolling eventually reaches genuinely the last line
// of content rather than getting stuck short of it.
func TestOverviewScrollReachesTrueEndOnASmallTerminal(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.width, m.height = 80, 16

	reachedEnd := false
	for i := 0; i < 50; i++ {
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > m.height {
			t.Fatalf("press %d: rendered %d lines but the terminal only has %d", i, len(lines), m.height)
		}
		if strings.Contains(m.View().Content, "[g]") {
			reachedEnd = true
			break
		}
		updated, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}))
		m = updated.(Model)
	}
	if !reachedEnd {
		t.Fatal("scrolling never reached Overview's own trailing content -- some of it is permanently unreachable")
	}
}

// TestSmallTerminalNeverRendersMoreLinesThanItHas is the regression test
// for a real bug across several pieces of chrome, not just one: bodyLayout
// used to floor paneW/paneH/innerW/innerH at comfortable desktop minimums
// (30/10/30/6) rather than true survival minimums, so on any terminal
// smaller than that the layout rendered as if it had more room than it
// actually did. Shrinking those floors surfaced three further, genuinely
// separate bugs, each confirmed by rendering a real 40x14 screen and
// counting physical lines against the real height:
//   - innerH (how much room a row list gets) used a flat "paneH-8" guess
//     for the chrome above it, rather than bodyPrefixHeight's own precise,
//     already-measured height -- wrong wherever the chrome actually took
//     more than 8 lines (any narrow-enough terminal, since the command
//     rail and hub tabs both wrap there).
//   - commandRail never truncated its own output to the width it was
//     given -- only a >40-column fallback shortened it, so below that it
//     could render longer than its declared width and get wrapped onto
//     real extra lines once actually embedded in the pane.
//   - the row list's own footer ("[↑/↓] select · ...") had the same gap:
//     only the optional per-action hints after the base trio were
//     width-aware.
//   - the sidebar had no concept of "not enough room" at all; it always
//     rendered every hub with blank-line spacing plus trailing keybinding
//     hints, overflowing on its own at any height below ~21 lines.
//
// Each is fixed at its source; this test exists so any of them (or a new
// one just like them) regressing shows up as a failure here rather than as
// a user staring at a terminal where the last few rows of a table are
// permanently below the screen, unreachable by scrolling no matter what.
func TestSmallTerminalNeverRendersMoreLinesThanItHas(t *testing.T) {
	s := newTestService(t)
	for i := 0; i < 10; i++ {
		registerAgent(t, s, "agent-0"+string(rune('0'+i)), model.RoleAgent, "src")
	}
	if _, e := s.Execute("owner", "message.post", "msg-1", model.MessagePosted{Kind: "FYI", To: []string{"owner"}, Subject: "hi"}); e != nil {
		t.Fatal(e)
	}

	for _, dims := range [][2]int{{140, 38}, {60, 20}, {40, 14}} {
		for _, enterView := range []func(*testing.T, Model) Model{enterAgentsView, enterInboxView} {
			m, e := New(s, "owner")
			if e != nil {
				t.Fatal(e)
			}
			m.width, m.height = dims[0], dims[1]
			m = enterView(t, m)
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) > dims[1] {
				t.Errorf("dims=%v: rendered %d lines but the terminal only has %d -- some content is unreachable, not just unscrolled",
					dims, len(lines), dims[1])
			}
		}
	}

	// Scrolling all the way down at the smallest size must still reach the
	// last row -- not just avoid overflowing above it.
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.width, m.height = 40, 14
	m = enterAgentsView(t, m)
	for i := 0; i < 15; i++ {
		m = pressKey(t, m, keyText("j"))
	}
	if id := m.agentList.SelectedID(m.state, m.actor); id != "owner" {
		t.Fatalf("scrolling to the end at 40x14 should reach the alphabetically-last agent (owner), got %q", id)
	}
}

func TestProjectControlDirectNavigation(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := view.Update(tea.KeyPressMsg(tea.Key{Code: 'g'}))
	view = next.(Model)
	if views[view.view] != "Agents" {
		t.Fatalf("g opened %q, want Agents", views[view.view])
	}
	next, _ = view.Update(tea.KeyPressMsg(tea.Key{Code: 'i'}))
	view = next.(Model)
	if views[view.view] != "Invocations" {
		t.Fatalf("i opened %q, want Invocations", views[view.view])
	}
}

// TestSwitchingViewsWithoutEnterShowsLiveContent guards a real regression:
// openView (driven by arrow-key hub navigation, letter shortcuts like 'g',
// and the palette) used to switch which view was displayed without
// refreshing that view's RowList -- only focusCurrentView (Enter) called
// Refresh. Confirmed live: switching tabs left panels reading "No rows here
// yet." until you additionally pressed Enter. Asserts the fix: opening a
// view refreshes its content immediately, without entering row-focus mode.
func TestSwitchingViewsWithoutEnterShowsLiveContent(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Register("builder", "builder", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'g'})) // opens Agents; Enter never pressed
	m = next.(Model)
	if views[m.view] != "Agents" {
		t.Fatalf("g opened %q, want Agents", views[m.view])
	}
	if m.rowFocus {
		t.Fatal("g should open the view, not enter row-focus mode")
	}
	m.width, m.height = 120, 30
	body := m.View().Content
	if strings.Contains(body, "No rows here yet.") {
		t.Fatal("Agents view shows no rows until Enter is pressed -- content should be live as soon as the view opens")
	}
	if !strings.Contains(body, "builder") {
		t.Fatalf("Agents view should show the registered agent without needing Enter, got:\n%s", body)
	}
}

func TestCyclePickerOption(t *testing.T) {
	options := []string{"AGENT", "OBSERVER", "ORCHESTRATOR", "OWNER"}
	if got := cyclePickerOption(options, "AGENT", false); got != "OBSERVER" {
		t.Fatalf("forward from AGENT = %q, want OBSERVER", got)
	}
	if got := cyclePickerOption(options, "AGENT", true); got != "OWNER" {
		t.Fatalf("backward from AGENT (wrap) = %q, want OWNER", got)
	}
	if got := cyclePickerOption(options, "OWNER", false); got != "AGENT" {
		t.Fatalf("forward from OWNER (wrap) = %q, want AGENT", got)
	}
	if got := cyclePickerOption(options, "not-a-real-value", false); got != "OBSERVER" {
		t.Fatalf("unknown current value should fall back to options[0] then advance, got %q", got)
	}
}

// TestPickerFieldCyclesWithArrowKeysAndRejectsTypedText exercises the
// picker end to end through a real form: left/right must change the
// selected value, and typed characters must be silently ignored rather
// than corrupting it -- this is what makes an enum field impossible to
// mistype, the actual point of the picker.
func TestPickerFieldCyclesWithArrowKeysAndRejectsTypedText(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Register("builder", "builder", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("a")) // activate
	if m.form != "agent.activate" {
		t.Fatalf("expected agent.activate form, got %q", m.form)
	}
	if got := m.inputs[0].Value(); got != "AGENT" {
		t.Fatalf("role should default to Options[0]=AGENT, got %q", got)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if got := m.inputs[0].Value(); got != "OBSERVER" {
		t.Fatalf("right should cycle AGENT -> OBSERVER, got %q", got)
	}
	// A typed character must not reach the field at all.
	m = pressKey(t, m, keyText("z"))
	if got := m.inputs[0].Value(); got != "OBSERVER" {
		t.Fatalf("typed text should be ignored on a picker field, got %q", got)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if got := m.inputs[0].Value(); got != "AGENT" {
		t.Fatalf("left should cycle back OBSERVER -> AGENT, got %q", got)
	}
}

func TestGuidedTaskFormUsesGovernedService(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.openTaskForm()
	m = next.(Model)
	for i, value := range []string{"task-ui", "Created in TUI", "feature/ui", "src/ui,tests/ui"} {
		m.inputs[i].SetValue(value)
	}
	m.formFocus = len(m.inputs) - 1
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if m.form != "" {
		t.Fatalf("form stayed open: %v", m.err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["task-ui"].Title != "Created in TUI" {
		t.Fatal("guided write did not reach service")
	}
}
