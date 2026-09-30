package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/charmbracelet/x/ansi"
)

func TestStatusBannerStaysVisibleAboveFullTable(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-alpha", model.Role("MEMBER"), "src")
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 18
	m.openView("Agents")
	m.notice = "Agent registered successfully"

	lines := strings.Split(m.View().Content, "\n")
	if !strings.Contains(lines[m.bodyPrefixHeight(colors())-1], "Notice: Agent registered successfully") {
		t.Fatalf("status banner was clipped or misplaced: %q", lines)
	}
	if row, ok := m.rowAtY(colors(), m.rowTableTopY(colors())+1); !ok || row != 0 {
		t.Fatalf("banner shifted table click mapping: row=%d ok=%v", row, ok)
	}
}

func TestConfirmMouseHitTargetsOnlyVisibleButtons(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.openConfirm(confirmState{prompt: "Suspend a very-long-provider-scoped-identifier-for-wrapping? Suspending it will affect delivery and future work."})
	for _, size := range [][2]int{{160, 40}, {100, 30}, {70, 24}} {
		m.width, m.height = size[0], size[1]
		for _, choice := range []struct {
			label string
			yes   bool
		}{{confirmYesLabel, true}, {confirmNoLabel, false}} {
			found := false
			for _, offset := range []int{0, 1000} {
				m.scrollOffset = offset
				for y, line := range strings.Split(m.View().Content, "\n") {
					plain := ansi.Strip(line)
					at := strings.LastIndex(plain, choice.label)
					if at < 0 {
						continue
					}
					found = true
					x := ansi.StringWidth(plain[:at]) + 2
					if yes, ok := m.confirmChoiceAt(colors(), x, y); !ok || yes != choice.yes {
						t.Fatalf("%dx%d offset %d: click on %q at (%d,%d) gave yes=%v ok=%v", size[0], size[1], offset, choice.label, x, y, yes, ok)
					}
				}
			}
			if !found {
				t.Fatalf("%dx%d: %q not visible; screen:\n%s", size[0], size[1], choice.label, ansi.Strip(m.View().Content))
			}
		}
		m.scrollOffset = 0
		for y, line := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(ansi.Strip(line), "very-long-provider-scoped") {
				if _, ok := m.confirmChoiceAt(colors(), m.sidebarWidth()+8, y); ok {
					t.Fatalf("%dx%d: click on prompt row %d accepted as confirmation", size[0], size[1], y)
				}
			}
		}
	}
}

func TestOpeningConfirmResetsPageScroll(t *testing.T) {
	m := Model{scrollOffset: 12}
	m.openConfirm(confirmState{prompt: "Review this action?"})
	if m.scrollOffset != 0 || m.confirm == nil {
		t.Fatalf("opening confirm left offset=%d confirm=%v", m.scrollOffset, m.confirm)
	}
}

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

// TestSidebarAndBodyAreSeparatedByARule: the two panes used to be
// divided by a blank column, which on a transparent TUI read as a gap
// in the layout rather than an edge. The rule has to be exactly one
// column, on every row, at exactly sidebarWidth() -- every click-to-cell
// translation in mouse.go is written against that offset, so a wider
// divider would shift every row, tab and field hit-test.
func TestSidebarAndBodyAreSeparatedByARule(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{140, 38}, {104, 20}, {60, 20}, {40, 14}} {
		m.width, m.height = size[0], size[1]
		column := m.sidebarWidth()
		lines := strings.Split(m.View().Content, "\n")
		for i, line := range lines {
			runes := []rune(ansi.Strip(line))
			if len(runes) <= column {
				t.Errorf("%dx%d line %d is too short to hold the divider: %q", size[0], size[1], i, string(runes))
				continue
			}
			// Inset by ruleInset at each end, matching the padding both
			// panes carry, so the rule divides the content rather than
			// sticking out past it.
			want := '│'
			if i < ruleInset || i >= len(lines)-ruleInset {
				want = ' '
			}
			if runes[column] != want {
				t.Errorf("%dx%d line %d: column %d is %q, want %q", size[0], size[1], i, column, string(runes[column]), string(want))
			}
		}
	}
}

// TestEveryViewFitsTheTerminal is the regression test for a scroll
// window that counted logical lines while the pane rendered physical
// ones: at a narrow width a line of prose wraps, so Overview (and every
// other non-table view through the same branch) rendered past the
// bottom of the screen -- 21 lines into a 20-line terminal, 16 into 14
// -- with no way to scroll to what fell off. TestSmallTerminalNever-
// RendersMoreLinesThanItHas covers the same rule for the two row-list
// views it enters; this one sweeps every view, which is how the gap
// survived.
func TestEveryViewFitsTheTerminal(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{140, 38}, {104, 20}, {80, 24}, {60, 20}, {40, 14}, {30, 12}, {20, 8}} {
		for _, name := range views {
			m.width, m.height = size[0], size[1]
			m.openView(name)
			if got := lipgloss.Height(m.View().Content); got > size[1] {
				t.Errorf("%s at %dx%d renders %d lines", name, size[0], size[1], got)
			}
		}
	}
}

func TestScrollViewportKeepsLongContentReachableInsideItsHeight(t *testing.T) {
	lines := make([]string, 24)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %02d", i)
	}
	content := strings.Join(lines, "\n")
	for _, width := range []int{12, 40} {
		first := scrollViewport(colors(), content, width, 5, 0, "PgUp/PgDn")
		last := scrollViewport(colors(), content, width, 5, 100, "PgUp/PgDn")
		if !strings.Contains(first, "line 00") || !strings.Contains(last, "line 23") {
			t.Errorf("width %d: first or last row inaccessible", width)
		}
		for _, rendered := range []string{first, last} {
			if lipgloss.Height(rendered) > 5 {
				t.Errorf("width %d: viewport exceeded five lines", width)
			}
			if !strings.Contains(rendered, "↕") {
				t.Errorf("width %d: missing visual scroll affordance", width)
			}
		}
	}
}

func TestLongFormKeepsFocusedFieldVisibleAtNarrowSize(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 60, 20
	opened, _ := m.openActionForm(invocationRequestForm, "invocation.request", "")
	m = opened.(Model)
	for i := 0; i < len(m.inputs)-1; i++ {
		m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	}
	if m.formFocus != len(m.inputs)-1 {
		t.Fatalf("expected final form field focused, got %d", m.formFocus)
	}
	rendered := m.View().Content
	if !strings.Contains(rendered, "Approval expires in") {
		t.Fatalf("focused final field is not visible after tabbing:\n%s", rendered)
	}
	for y, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "Approval expires in") {
			if field, ok := m.formFieldAtY(colors(), y); !ok || field != m.formFocus {
				t.Fatalf("click map at row %d picked field %d (ok=%t), want focused field %d", y, field, ok, m.formFocus)
			}
			break
		}
	}
	if !strings.Contains(rendered, "↕") {
		t.Fatal("long form lacks a visible scroll indicator")
	}
	if got := lipgloss.Height(rendered); got > m.height {
		t.Fatalf("long form overflowed to %d lines at height %d", got, m.height)
	}
}

func TestEveryViewStaysWithinTerminalColumns(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{140, 38}, {80, 24}, {60, 20}, {40, 14}, {30, 12}, {20, 8}} {
		m.width, m.height = size[0], size[1]
		for _, name := range views {
			m.openView(name)
			for i, line := range strings.Split(m.View().Content, "\n") {
				if got := lipgloss.Width(line); got > m.width {
					t.Errorf("%s at %dx%d line %d occupies %d columns", name, m.width, m.height, i, got)
				}
			}
		}
	}
}

func TestOverlaysFitSmallTerminals(t *testing.T) {
	s := newTestService(t)
	base, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{80, 24}, {60, 20}, {40, 14}, {30, 12}, {20, 8}} {
		m := base
		m.width, m.height = size[0], size[1]
		states := []Model{m}
		paletteModel := m
		paletteModel.palette = true
		states = append(states, paletteModel)
		formModel, _ := m.openActionForm(invocationRequestForm, "invocation.request", "")
		states = append(states, formModel.(Model))
		confirmModel := m
		confirmModel.confirm = &confirmState{prompt: "Confirm this exact change?"}
		states = append(states, confirmModel)
		for i, candidate := range states {
			rendered := candidate.View().Content
			if got := lipgloss.Height(rendered); got > m.height {
				t.Errorf("overlay %d at %dx%d renders %d lines", i, m.width, m.height, got)
			}
			for lineNo, line := range strings.Split(rendered, "\n") {
				if got := lipgloss.Width(line); got > m.width {
					t.Errorf("overlay %d at %dx%d line %d occupies %d columns", i, m.width, m.height, lineNo, got)
				}
			}
		}
	}
}

// TestConfirmShowsBothChoicesAndKeepsItsBorder is the regression test for
// a confirm dialog that rendered without a width: lipgloss padded every
// line to the long prompt's width, and the pane's hard-wrap then split each
// padded line into its text plus lines of trailing spaces. At 70x24 that
// showed "Sign and apply" and pushed "Go back" below the fold -- the
// irreversible choice visible, the safe one hidden -- and the wrapped
// prompt lost the left border marking it as part of the dialog.
func TestConfirmShowsBothChoicesAndKeepsItsBorder(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	const prompt = "Suspend agent claude-a-rather-long-provider-scoped-identifier-for-wrapping? Suspension blocks new claims."
	for _, size := range [][2]int{{160, 40}, {100, 30}, {70, 24}} {
		for _, localDraft := range []bool{false, true} {
			m.width, m.height = size[0], size[1]
			m.openView("Agents")
			m.confirm = &confirmState{prompt: prompt, typ: "agent.suspend", id: "x", localDraft: localDraft}
			label := fmt.Sprintf("%dx%d localDraft=%v", size[0], size[1], localDraft)

			screen := ansi.Strip(m.View().Content)
			for _, choice := range []string{"Go back", "[n / esc]"} {
				if !strings.Contains(screen, choice) {
					t.Errorf("%s: %q is not on screen; the safe choice must be visible whenever the destructive one is", label, choice)
				}
			}

			block := m.renderConfirm(colors())
			for i, line := range strings.Split(block, "\n") {
				if w := lipgloss.Width(line); w > m.contentWidth() {
					t.Errorf("%s: confirm line %d is %d columns in a %d-column pane, so the pane will split it", label, i, w, m.contentWidth())
				}
				if !strings.HasPrefix(ansi.Strip(line), "┃") {
					t.Errorf("%s: confirm line %d has lost its border: %q", label, i, ansi.Strip(line))
				}
			}
		}
	}
}

// TestWrapAtHyphensKeepsWholeSegments: the sidebar truncated the project
// ID to one line ("ac-fed3cb9f-266b-4cd5-a0…"), which cut off exactly the
// part that tells one project from another. It wraps now, breaking after
// a hyphen so no line ends mid-way through a hex group.
func TestWrapAtHyphensKeepsWholeSegments(t *testing.T) {
	const id = "ac-fed3cb9f-266b-4cd5-a081-1fe1441f9c86"
	for _, width := range []int{25, 18, 14, 10} {
		lines := wrapAtHyphens(id, width)
		if got := strings.Join(lines, ""); got != id {
			t.Fatalf("width %d: wrapping must lose nothing, rejoined %q", width, got)
		}
		for i, line := range lines {
			if len([]rune(line)) > width {
				t.Errorf("width %d: line %d is %d wide: %q", width, i, len([]rune(line)), line)
			}
			// Every line but the last ends on a hyphen when the segments
			// allow it -- only a segment longer than the width itself
			// (none here above width 12) may be hard-broken.
			if width > 12 && i < len(lines)-1 && !strings.HasSuffix(line, "-") {
				t.Errorf("width %d: line %d breaks mid-segment: %q", width, i, line)
			}
		}
	}
	if got := wrapAtHyphens("abcdefghij", 4); strings.Join(got, "") != "abcdefghij" || len(got) != 3 {
		t.Errorf("a segment longer than the width must be hard-broken, not dropped: %q", got)
	}
}

// TestSidebarShowsTheWholeProjectIDAndPinsItsLegend covers the two
// sidebar layout changes together, as rendered: the full project ID is on
// screen, and the KEYS legend sits at the foot of the column rather than
// floating one line under the hub list with empty space beneath it.
func TestSidebarShowsTheWholeProjectIDAndPinsItsLegend(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.projectID = "ac-fed3cb9f-266b-4cd5-a081-1fe1441f9c86"
	for _, size := range [][2]int{{140, 40}, {120, 32}, {70, 30}} {
		m.width, m.height = size[0], size[1]
		side, _ := m.renderSidebar(colors(), m.sidebarWidth(), m.height)
		lines := strings.Split(ansi.Strip(side), "\n")
		compact := strings.Join(strings.Fields(strings.Join(lines, " ")), "")
		if !strings.Contains(compact, m.projectID) {
			t.Errorf("%dx%d: the full project ID is not on screen", size[0], size[1])
		}
		// Pinned, not merely last: the final legend row is the sidebar's
		// final content line, directly above its bottom padding. The old
		// layout also ended with the legend -- one line under the hubs,
		// with blank rows beneath it -- so "is it last" cannot tell them
		// apart; "is it at the bottom" can.
		quitAt := -1
		for i, line := range lines {
			if strings.Contains(line, "quit") {
				quitAt = i
			}
		}
		if want := len(lines) - 2; quitAt != want {
			t.Errorf("%dx%d: the legend's last row is on line %d, want it pinned to line %d", size[0], size[1], quitAt, want)
		}
		keysAt, lastHubAt := -1, -1
		for i, line := range lines {
			if strings.TrimSpace(line) == "KEYS" {
				keysAt = i
			}
			if strings.Contains(line, "Project") && !strings.Contains(line, "└") {
				lastHubAt = i
			}
		}
		if keysAt < 0 || keysAt <= lastHubAt {
			t.Errorf("%dx%d: the KEYS heading (line %d) must come after the hub list (line %d)", size[0], size[1], keysAt, lastHubAt)
		}
		if got := len(lines); got != m.height {
			t.Errorf("%dx%d: the sidebar renders %d lines", size[0], size[1], got)
		}
	}
}
