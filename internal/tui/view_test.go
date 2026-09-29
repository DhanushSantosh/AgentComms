package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestTruncateMiddleKeepsWhatDistinguishesProviderScopedIDs(t *testing.T) {
	// The point of eliding the middle: two agents of the same provider share
	// a 7-character prefix, so end-truncation renders them identically.
	a := truncateMiddle("claude-reviewer", 13)
	b := truncateMiddle("claude-developer", 13)
	if a == b {
		t.Fatalf("same-provider IDs must stay distinguishable, both rendered %q", a)
	}
	for _, got := range []string{a, b} {
		if len([]rune(got)) != 13 {
			t.Errorf("truncateMiddle should fill the column exactly, got %q (%d runes)", got, len([]rune(got)))
		}
	}
	// End-truncation is what this replaces; prove it actually collides.
	if truncate("claude-reviewer", 13) != truncate("claude-developer", 13) {
		t.Log("note: end-truncation no longer collides for these inputs")
	}
	// A value that fits is returned untouched, and short widths degrade to
	// ordinary truncation rather than producing nonsense.
	if got := truncateMiddle("claude", 13); got != "claude" {
		t.Errorf("a fitting value must be unchanged, got %q", got)
	}
	if got := truncateMiddle("claude-reviewer", 4); got != truncate("claude-reviewer", 4) {
		t.Errorf("below the middle-elision floor it should match truncate, got %q", got)
	}
	if got := truncateMiddle("claude-reviewer", 0); got != "" {
		t.Errorf("zero width should render nothing, got %q", got)
	}
}

// TestToastRendersOnceBesideTheActor is the regression test for a real
// double-render, caught live on a background-commit notification: the
// toast occupied the command rail's LIVE/STALE slot at the top left
// *and* was appended again as a block under the content, so one
// notification appeared twice, at opposite corners of the screen. It now
// renders in exactly one place -- beside the actor, top right.
func TestToastRendersOnceBesideTheActor(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 160, 40
	const toast = "Event #293 committed in background"
	m.toastMsg = toast
	m.toastExpiresAt = time.Now().Add(time.Minute)

	rendered := m.View().Content
	if got := strings.Count(rendered, toast); got != 1 {
		t.Fatalf("the toast renders %d times on screen, want exactly 1", got)
	}
	// Same physical line as the actor, which is what "beside the actor"
	// has to mean for it to be where the eye already goes.
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, toast) && !strings.Contains(line, m.actor) {
			t.Fatal("the toast renders on its own line, not beside the actor")
		}
	}
	if !strings.Contains(m.commandRail(colors(), 160), toast) {
		t.Fatal("expected the toast in the command rail")
	}
}

// TestToastNeverCostsTheActorItsPlace: the rail is truncated from the
// right to hold it to one line, so a toast simply prepended to the actor
// would push the actor off the screen instead of shortening itself.
func TestToastNeverCostsTheActorItsPlace(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	p := colors()
	const toast = "Event #293 committed in background"
	for _, width := range []int{200, 120, 92, 70, 56, 44, 30} {
		quiet := m.commandRail(p, width)
		m.toastMsg, m.toastExpiresAt = toast, time.Now().Add(time.Minute)
		loud := m.commandRail(p, width)
		m.toastMsg = ""

		if strings.Contains(loud, "\n") {
			t.Errorf("width %d: the rail wrapped onto a second line", width)
		}
		if got := lipgloss.Width(loud); got > width {
			t.Errorf("width %d: the rail spans %d columns", width, got)
		}
		if strings.Contains(quiet, m.actor) && !strings.Contains(loud, m.actor) {
			t.Errorf("width %d: the toast pushed the actor off the rail", width)
		}
	}
}

// TestStaleIndicatorAndToastShareTheRail: they used to contend for the
// same slot, so a stale-reads warning suppressed the toast. On opposite
// ends of the rail now, both can say their piece.
func TestStaleIndicatorAndToastShareTheRail(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.staleReads = staleReadThreshold
	m.toastMsg = "Event #293 committed in background"
	m.toastExpiresAt = time.Now().Add(time.Minute)
	rail := m.commandRail(colors(), 200)
	if !strings.Contains(rail, "STALE") {
		t.Error("expected the stale indicator")
	}
	if !strings.Contains(rail, m.toastMsg) {
		t.Error("expected the toast alongside it")
	}
}

// TestExpiredToastLeavesTheRailAlone guards the obvious other half: once
// its window passes the badge must go, taking its columns with it.
func TestExpiredToastLeavesTheRailAlone(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	p := colors()
	quiet := m.commandRail(p, 120)
	m.toastMsg = "Event #293 committed in background"
	m.toastExpiresAt = time.Now().Add(-time.Second)
	if got := m.commandRail(p, 120); got != quiet {
		t.Fatalf("an expired toast still changed the rail:\n%q\n%q", got, quiet)
	}
}

// TestLightTerminalGetsItsOwnDarkSurface: the TUI has one palette and it
// is built for a dark background -- white text, light-grey muted,
// pure-yellow amber. The body pane paints no background of its own so a
// dark terminal's theme shows through, which on a light terminal would
// mean unreadable. There, and only there, it paints its own.
func TestLightTerminalGetsItsOwnDarkSurface(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 120, 30
	p := colors()
	// #000000, the ink the dark surface is painted in.
	const inkBackground = "48;2;0;0;0"

	if body := m.renderBody(p, 90, 30); strings.Contains(body, inkBackground) {
		t.Error("a dark terminal must keep its own background in the body pane")
	}
	if m.View().BackgroundColor != nil {
		t.Error("a dark terminal's own background must be left alone")
	}

	m = pressMsg(t, m, tea.BackgroundColorMsg{Color: lipgloss.Color("#FFFFFF")})
	if !m.lightTerminal {
		t.Fatal("expected a white terminal background to be detected as light")
	}
	if body := m.renderBody(p, 90, 30); !strings.Contains(body, inkBackground) {
		t.Error("a light terminal must get a dark body pane")
	}
	// The sidebar and the palette card take the same surface, because
	// the screen-level color below goes through an OSC escape the
	// terminal is free to ignore.
	if side, _ := m.renderSidebar(p, m.sidebarWidth(), 30); !strings.Contains(side, inkBackground) {
		t.Error("a light terminal must get a dark sidebar")
	}
	if card, _ := m.paletteLayout(p); !strings.Contains(card, inkBackground) {
		t.Error("a light terminal must get a dark palette card")
	}
	view := m.View()
	if view.BackgroundColor != p.ink {
		t.Errorf("expected the screen background set to ink, got %v", view.BackgroundColor)
	}
	if view.ForegroundColor != p.text {
		t.Errorf("expected the screen foreground set to text, got %v", view.ForegroundColor)
	}

	// And back: a terminal that switches to a dark theme re-sends the
	// answer, and the surface goes away again.
	m = pressMsg(t, m, tea.BackgroundColorMsg{Color: lipgloss.Color("#000000")})
	if m.lightTerminal {
		t.Fatal("expected a black terminal background to be detected as dark")
	}
	if body := m.renderBody(p, 90, 30); strings.Contains(body, inkBackground) {
		t.Error("switching back to a dark terminal must drop the painted surface")
	}
}

// TestBackgroundAnswerIsNotSwallowedByAFocusedMode: terminals answer the
// query whenever they get to it, which can be long after startup and in
// any mode. Every mode-specific update function returns before the main
// switch, so this has to be handled ahead of the dispatch.
func TestBackgroundAnswerIsNotSwallowedByAFocusedMode(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = enterAgentsView(t, m)
	m = pressMsg(t, m, tea.BackgroundColorMsg{Color: lipgloss.Color("#FFFFFF")})
	if !m.lightTerminal {
		t.Fatal("a focused row list swallowed the terminal's background answer")
	}
	if !m.rowFocus {
		t.Fatal("handling the answer must not disturb the focused view")
	}
}
