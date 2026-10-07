package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/charmbracelet/x/ansi"
)

// All TUI rendering: the color palette, View, the sidebar/body/rail/tabs
// layout, the foundation views (overview, workforce, attention, chain,
// ...), the command palette overlay, the inspector, and text helpers.
// Split out of model.go.

type palette struct{ ink, cyan, amber, red, violet, muted, text color.Color }

// One palette, the high-contrast one. Nothing here paints a background
// except an inverted badge (ink text on cyan or red); ink is a text
// color on a colored bar, never a fill. Everything the TUI draws sits
// on the terminal's own background, which is a deliberate trade: it
// means the TUI never fights a terminal's theme or blanks out its
// wallpaper, and it means these colors have to carry on their own. They
// are picked for a dark background. A light one will wash out the muted
// grey and the amber -- run the terminal dark, which is what the
// transparency is for.
// There used to be two -- a muted
// default and this, toggled with "h" and remembered in user config --
// which meant every surface had to be checked twice, the dim default was
// what most people ever saw, and the accessible option was a preference
// you had to know to go looking for. The remaining colors are chosen to
// carry meaning (cyan = live/selected, amber = caution and identity, red
// = failure, violet = secondary) against whatever background the
// terminal itself provides; the body pane no longer paints one.
func colors() palette {
	return palette{
		ink:    lipgloss.Color("#000000"),
		cyan:   lipgloss.Color("#00FFFF"),
		amber:  lipgloss.Color("#FFFF00"),
		red:    lipgloss.Color("#FF4444"),
		violet: lipgloss.Color("#DD88FF"),
		muted:  lipgloss.Color("#BBBBBB"),
		text:   lipgloss.Color("#FFFFFF"),
	}
}

// contentWidth is the width-only half of bodyLayout, split out so
// bodyPrefixHeight (which needs to measure the real command rail/tabs/
// header at the width they actually render at) can use it without calling
// bodyLayout itself -- bodyLayout's own innerH now depends on
// bodyPrefixHeight's measurement, and bodyPrefixHeight depending back on
// bodyLayout would be a cycle.
// wrappedHeight reports how many physical terminal lines s occupies once
// the body pane wraps it at width -- lipgloss.Height alone counts "\n"s,
// which undercounts every line long enough to wrap and is what let the
// page-level scroll window render past the bottom of a narrow terminal.
func wrappedHeight(s string, width int) int {
	if width <= 0 {
		return lipgloss.Height(s)
	}
	return lipgloss.Height(lipgloss.NewStyle().Width(width).Render(s))
}

// clampHeight cuts s to at most maxLines physical lines at this width,
// on a logical-line boundary wherever it can. A first line that is
// already taller than the whole budget is truncated to one line instead,
// since returning it whole would defeat the point.
func clampHeight(s string, width, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	used, kept := 0, 0
	for _, line := range lines {
		height := wrappedHeight(line, width)
		if used+height > maxLines {
			break
		}
		used += height
		kept++
	}
	switch kept {
	case len(lines):
		return s
	case 0:
		return ansi.Truncate(lines[0], max(1, width), "…")
	}
	return strings.Join(lines[:kept], "\n")
}

func (m Model) contentWidth() int {
	paneW := max(10, m.width-m.sidebarWidth()-3)
	return max(6, paneW-4)
}

// statusBanner pins action feedback above the scrollable content. A notice
// appended after a full table was clipped before anyone could read it.
// Forms render their own feedback inside their scrollable pane instead.
func (m Model) statusBanner(p palette, width int) string {
	if m.form != "" || m.confirm != nil {
		return ""
	}
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(p.red).Render(ansi.Truncate("Error: "+m.err.Error(), width, "…"))
	}
	if m.notice != "" {
		return lipgloss.NewStyle().Foreground(p.cyan).Render(ansi.Truncate("Notice: "+m.notice, width, "…"))
	}
	return ""
}

// bodyLayout returns the same pane/content dimensions View, renderBody, and
// the mouse hit-testing in mouse.go all need -- computed once here so they
// can never drift apart. paneW/paneH are the body pane's own outer size
// (what renderBody receives); innerW/innerH are its padded interior, where
// bodyContent (row tables, forms, prose) actually renders.
//
// innerH is measured, not guessed: it used to be a flat `paneH-8`, sized
// for whatever a desktop-width command rail and hub tabs happened to take
// (about 8 lines) -- fine as long as paneH itself was floored well above
// any real small terminal, which silently covered for it. Once that outer
// floor came down to track the real terminal, the flat "-8" stopped
// matching reality: at a narrow contentWidth the command rail and hub
// tabs genuinely wrap onto more physical lines than 8 accounts for (this
// view's own hub can have five or six tab labels to fit), so a row list
// kept being told it had more room than it actually did and rendered
// rows the terminal had no space left to show -- unreachable by
// scrolling, the exact thing a "smaller minimum" is supposed to avoid.
// bodyPrefixHeight already measures this precisely for click math
// (mouse.go); innerH now reuses that same measurement instead of keeping
// a second, cruder guess that can silently drift from it.
func (m Model) bodyLayout(p palette) (paneW, paneH, innerW, innerH int) {
	// The floors below exist only to keep downstream width/height
	// arithmetic from going negative (or zero) before the first real
	// WindowSizeMsg arrives -- they must never exceed what the real
	// terminal can show. Small floors mean the layout always matches the
	// truth of what the terminal can actually display, so RowList's own
	// scrolling is genuinely the only thing standing between the cursor
	// and the last row, at any size -- nothing renders further down the
	// page than the terminal has room for.
	sidebarW := m.sidebarWidth()
	paneW = max(10, m.width-sidebarW-3)
	paneH = max(4, m.height)
	innerW = max(6, paneW-4)
	innerH = max(1, paneH-m.bodyPrefixHeight(p)-m.bodySuffixHeight(p)-1)
	return
}
func (m Model) View() tea.View {
	p := colors()
	sidebarW := m.sidebarWidth()
	paneW, paneH, _, _ := m.bodyLayout(p)
	side, _ := m.renderSidebar(p, sidebarW, paneH)
	body := m.renderBody(p, paneW, paneH)
	screen := lipgloss.JoinHorizontal(lipgloss.Top, side, verticalRule(p, paneH), body)
	screen = lipgloss.NewStyle().MaxWidth(m.width).Render(screen)
	if m.palette {
		screen = m.renderPalette(p, screen)
	}
	// Pane floors keep layout calculations safe, but cannot enlarge the
	// actual terminal. Apply the physical bounds after every overlay too.
	if m.width <= 0 || m.height <= 0 {
		screen = ""
	} else {
		screen = lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(screen)
	}
	v := tea.NewView(screen)
	// BackgroundColor/ForegroundColor deliberately left nil: the TUI
	// never sets the terminal's own colors, so what it draws sits on
	// whatever is already there.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Agent Comms · Project Control"
	return v
}

// verticalRule is the divider between the sidebar and the body: exactly
// one column wide, because that is what the blank separator it replaced
// occupied and every click-to-cell translation in mouse.go is written
// against that one column (sidebarWidth() + 1). Widening it for
// breathing room would silently shift every row, tab and field
// hit-test by two columns; the sidebar's own right padding and the
// body pane's left padding already provide the gap on either side.
// It is inset by ruleInset lines at each end rather than run edge to
// edge: both panes carry a line of padding at the top and bottom, so a
// full-height rule stuck out past the content it divides at both ends.
// The block stays exactly height lines tall -- the inset rows are blank,
// not missing -- because JoinHorizontal aligns the three columns by
// their own heights.
func verticalRule(p palette, height int) string {
	height = max(1, height)
	rows := make([]string, height)
	for i := range rows {
		rows[i] = " "
		if i >= ruleInset && i < height-ruleInset {
			rows[i] = "│"
		}
	}
	return lipgloss.NewStyle().Foreground(p.muted).Render(strings.Join(rows, "\n"))
}

// ruleInset is how many lines the divider gives up at each end, matching
// the one line of padding the sidebar and the body pane each carry.
// bodyLayout floors the pane at 4 lines, so there are always at least
// two left to draw.
const ruleInset = 1

// The widths are cramped rather than generous on purpose at the small
// end -- every column here is one the body does not get -- but the wide
// case was too tight for its own content: at 21 the project ID was
// always elided and the active view's name ("Project settings",
// "Contracts & decisions") never fit under its hub, even after the
// truncation budget was corrected. 27 is what the longest of them
// ("Contracts & decisions", 21 columns) needs once the sidebar's own
// padding, the line's indent and its "└ " prefix are paid for.
func (m Model) sidebarWidth() int {
	if m.width < 60 {
		return 16
	}
	if m.width < 72 {
		return 20
	}
	return 27
}

// renderSidebar also returns, per navigationHubs entry, which line of rows
// (before Padding is applied) carries that hub's clickable name -- read
// back by mouse.go's sidebarHubAt, recomputed fresh from the same (m, p, w)
// a click arrived under, since View() has no way to hand this to the
// Update() call that handles the click.
// sidebarTitleText is the sidebar's brand line, kept as plain text
// (unstyled) so it can be truncated correctly wherever it needs to be --
// truncate() slices runes with no notion of ANSI escape codes, so running
// it on an already-.Render()'d string chops through the middle of a color
// code as readily as through the visible text, leaking a raw, unterminated
// escape sequence onto the screen. Confirmed live: below the height where
// the sidebar's compact fallback layout kicks in (see the len(rows)+2 > h
// branch below), the title rendered as literal garbage like
// "[1;38;2;0;25…" instead of "● AGENT COMMS" -- exactly this mistake, once,
// at the one call site that truncated the pre-styled string instead of
// this plain one.
const sidebarTitleText = "● AGENT COMMS"

// sidebarRenderedHeight is how many physical lines these rows occupy in
// a sidebar w columns wide -- its own Padding(1) leaves w-2 for content,
// and anything longer wraps. Counting len(rows) instead is what let a
// single wrapped row push the sidebar past the bottom of the screen.
func sidebarRenderedHeight(rows []string, w int) int {
	total := 0
	for _, row := range rows {
		total += wrappedHeight(row, max(1, w-2))
	}
	return total
}

// wrapAtHyphens breaks an identifier across lines no wider than width,
// preferring to break just after a hyphen so each line ends on a whole
// segment: "ac-fed3cb9f-266b-4cd5-" / "a081-1fe1441f9c86", not a cut
// through the middle of a hex group. A segment longer than width on its
// own is hard-broken, since there is no better place to split it.
//
// The sidebar used to truncate the project ID to one line, leaving
// "ac-fed3cb9f-266b-4cd5-a0…" -- the part that distinguishes one project
// from another was always the part cut off.
func wrapAtHyphens(text string, width int) []string {
	if width <= 0 || text == "" {
		return nil
	}
	var segments []string
	for rest := text; rest != ""; {
		i := strings.Index(rest, "-")
		if i < 0 {
			segments = append(segments, rest)
			break
		}
		segments = append(segments, rest[:i+1])
		rest = rest[i+1:]
	}
	var lines []string
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, segment := range segments {
		for len([]rune(segment)) > width {
			flush()
			runes := []rune(segment)
			lines = append(lines, string(runes[:width]))
			segment = string(runes[width:])
		}
		if len([]rune(current))+len([]rune(segment)) > width {
			flush()
		}
		current += segment
	}
	flush()
	return lines
}

func (m Model) renderSidebar(p palette, w, h int) (view string, hubLine []int) {
	titleStyle := lipgloss.NewStyle().Foreground(p.cyan).Bold(true)
	title := titleStyle.Render(sidebarTitleText)
	activeHub := m.activeHubIndex()
	headingStyle := lipgloss.NewStyle().Foreground(p.muted)

	rows := []string{title}
	for _, line := range wrapAtHyphens(m.projectID, max(8, w-2)) {
		rows = append(rows, headingStyle.Render(line))
	}
	rows = append(rows, "", headingStyle.Render("OPERATIONS"), "")
	hubLine = make([]int, len(navigationHubs))
	for i, hub := range navigationHubs {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.muted)
		if i == activeHub {
			marker = "▌ "
			style = style.Foreground(p.cyan).Bold(true)
		}
		hubLine[i] = len(rows)
		rows = append(rows, style.Render(marker+hub.Name), "")
		if i == activeHub {
			// w-6, not w-5: the sidebar's outer Padding(1) takes a
			// column on each side (2), this line's own PaddingLeft
			// takes 2 more, and "└ " another 2. At w=21 the old budget
			// let "Project settings" render 20 columns wide inside 19
			// usable ones, so it wrapped -- and since the fit check
			// below counted rows rather than rendered lines, the whole
			// sidebar then came out one line taller than the terminal.
			rows = append(rows, lipgloss.NewStyle().Foreground(p.text).PaddingLeft(2).
				Render("└ "+truncate(views[m.view], max(8, w-6))), "")
		}
	}
	// The global keys, from the same mode-aware list keyhints.go builds
	// (globalHints) rather than three hardcoded lines that only ever
	// described navigation mode -- inside a focused tab ↑↓ moves rows,
	// not hubs, and this used to keep insisting otherwise.
	//
	// Pinned to the foot of the sidebar under their own heading, the way
	// the body's key bar is pinned to the foot of the pane. They used to
	// sit one blank line under the last hub, which left them floating
	// mid-column with the sidebar's whole lower half empty beneath them,
	// reading as part of the hub list rather than as a legend.
	keys := append([]string{headingStyle.Render("KEYS")}, hintGrid(p, m.globalHints(), max(4, w-2))...)
	// filler may be 0: the hub list already ends on a blank line, so the
	// legend can sit flush under it. Below 0 it does not fit at all, and
	// the rows it overflows force the compact layout that follows, which
	// drops the legend rather than render past the bottom of the screen.
	filler := (h - 2) - sidebarRenderedHeight(rows, w) - sidebarRenderedHeight(keys, w)
	rows = append(rows, make([]string, max(0, filler))...)
	rows = append(rows, keys...)
	// Padding(1) costs 2 more lines (top+bottom) than the rows themselves,
	// and a row wider than the column costs more than one line of its
	// own -- which is why the check measures rendered height rather than
	// counting the slice. Unlike
	// the body's row lists, the sidebar has no scrolling concept of its
	// own -- it's meant to show the whole nav hierarchy at a glance -- so
	// when the comfortable layout above doesn't fit a small terminal, drop
	// every blank spacer line, the active hub's view name (folded into its
	// own line instead), and the trailing keybinding hints (redundant with
	// the row list's own footer and the command palette) rather than let
	// the sidebar render taller than the terminal: that used to push its
	// own bottom rows past the real screen with nothing able to scroll to
	// them, the same "content exists but nothing reaches it" failure this
	// pass through bodyLayout's floors exists to eliminate.
	if sidebarRenderedHeight(rows, w)+2 > h {
		rows = []string{titleStyle.Render(truncate(sidebarTitleText, max(4, w-2)))}
		hubLine = make([]int, len(navigationHubs))
		for i, hub := range navigationHubs {
			marker := "  "
			style := lipgloss.NewStyle().Foreground(p.muted)
			if i == activeHub {
				marker = "▌ "
				style = style.Foreground(p.cyan).Bold(true)
			}
			hubLine[i] = len(rows)
			// w-4, not w-2: the outer style below adds Padding(1) (1
			// column each side) on top of marker's own 2 columns -- the
			// same "forgot the container's own padding" mistake that once
			// made a row-list cell wrap. No "· <view>" suffix here either
			// (unlike the comfortable layout's separate expansion line):
			// the current view is already visible in the body's own
			// command rail ("<hub> / <view>"), and appending it here is
			// exactly what made this line long enough to need truncating
			// in the first place.
			rows = append(rows, style.Render(marker+truncate(hub.Name, max(1, w-4))))
		}
	}
	// No background fill: like the body pane, the sidebar shows whatever
	// the terminal itself is. It used to paint p.ink over its whole
	// column, which on a terminal with a background image was a black
	// bar down the side of it.
	return lipgloss.NewStyle().Width(w).Height(h).Padding(1).Foreground(p.text).
		Render(strings.Join(rows, "\n")), hubLine
}
func (m Model) renderBody(p palette, w, h int) string {
	// contentW, not w: meta/tabs render inside pane's own Padding(1, 2)
	// below, whose actual usable width is w-4 (2 columns of padding each
	// side), not the outer w. Sizing them to w made the tabs bar's
	// border-bottom line exactly 4 columns too wide for that inner box,
	// silently wrapping it onto an extra line and shifting the header and
	// everything below it down by one row -- confirmed live via a click
	// landing one row above the domain it should have selected in Project
	// settings. bodyPrefixHeight (mouse.go) mirrors this exact width so
	// its line-counting and the real render can never disagree again.
	_, _, contentW, contentH := m.bodyLayout(p)
	title := views[m.view]
	if title == "Overview" {
		title = "PROJECT CONTROL"
	}
	header := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(title)
	meta := m.commandRail(p, contentW)
	tabs, _ := m.renderHubTabs(p, contentW)
	// No Background of its own, ever: the body pane keeps the terminal's
	// real colors, wallpaper and all. It used to paint itself a dark grey
	// (the palette's since-deleted "panel" color) over the whole
	// right-hand side, which fought every terminal theme that wasn't that
	// exact shade. Every element inside still sets its own semantic color
	// (cyan/amber/red/muted), so nothing loses meaning; only the slab
	// goes. See colors() for the deliberate trade this makes.
	pane := lipgloss.NewStyle().Width(w).Height(h).Padding(1, 2)
	prefix := meta + "\n" + tabs + "\n\n" + header + "\n\n"
	if banner := m.statusBanner(p, contentW); banner != "" {
		prefix += banner + "\n"
	}
	if m.form != "" {
		content := m.renderForm(p)
		body := prefix + scrollViewport(p, content, contentW, contentH, m.scrollOffset, "PgUp/PgDn")
		return pane.Render(clampHeight(body, contentW, max(1, h-2)))
	}
	if m.confirm != nil {
		content := m.renderConfirm(p)
		body := prefix + scrollViewport(p, content, contentW, contentH, m.scrollOffset, "PgUp/PgDn")
		return pane.Render(clampHeight(body, contentW, max(1, h-2)))
	}
	wrap := lipgloss.NewStyle().MaxWidth(contentW)
	content := ""
	bodyContent := ""
	listW, listH := m.rowListDimensions(p)
	switch views[m.view] {
	case "Overview":
		bodyContent = wrap.Render(m.overview(p))
	case "My work", "Tasks":
		bodyContent = m.taskList.View(p, m.state, m.actor, listW, listH)
	case "Inbox":
		bodyContent = m.messageList.View(p, m.state, m.actor, listW, listH)
	case "Agents":
		bodyContent = m.agentList.View(p, m.state, m.actor, listW, listH)
	case "Invocations":
		bodyContent = m.invocationList.View(p, m.state, m.actor, listW, listH)
	case "Runtimes":
		bodyContent = m.runtimeList.View(p, m.state, m.actor, listW, listH)
	case "Approvals":
		bodyContent = m.approvalList.View(p, m.state, m.actor, listW, listH)
	case "Documents":
		bodyContent = m.documentList.View(p, m.state, m.actor, listW, listH)
	case "Contracts & decisions":
		bodyContent = m.decisionList.View(p, m.state, m.actor, listW, listH)
	case "Artifacts":
		bodyContent = m.artifactList.View(p, m.state, m.actor, listW, listH)
	case "Drafts":
		bodyContent = m.draftsView(p)
	case "Environment":
		bodyContent = m.envList.View(p, m.state, m.actor, listW, listH)
	case "Providers":
		bodyContent = m.providerList.View(p, m.state, m.actor, listW, listH)
	case "Project settings":
		bodyContent = m.projectSettings(p, contentW, contentH)
	case "Blockers":
		bodyContent = wrap.Render(m.blockers(p))
	case "Audit & health":
		bodyContent = wrap.Render(m.integrity(p))
	case "Activity":
		bodyContent = wrap.Render(m.chain(p))
	case "Archive search":
		bodyContent = wrap.Render(m.archive(p))
	}
	content = bodyContent
	isTable := m.activeRowList() != nil
	if isTable {
		if detail := m.tableDetail(p, contentW); detail != "" && contentH > 2 {
			detailH := m.tableDetailHeight(p, contentW, contentH)
			content += "\n\n" + scrollViewport(p, detail, contentW, detailH, m.detailScrollOffset, "Shift+PgUp/PgDn")
		}
	}
	if !isTable {
		lines := strings.Split(content, "\n")
		// contentH directly, not contentH-4: contentH (bodyLayout's innerH)
		// already IS the precise remaining room for content, computed from
		// bodyPrefixHeight's own exact measurement of everything above it
		// (top padding, command rail, hub tabs, both blank lines, the
		// header) plus bottom padding -- subtracting another 4 here was
		// double-counting overhead already accounted for once, the same
		// "flat guess instead of trusting the precise measurement" mistake
		// TestSmallTerminalNeverRendersMoreLinesThanItHas's own history
		// already fixed once for innerH itself (see that test's comment).
		// Also floored at 0, not a comfortable desktop constant like the 22
		// this used to floor at: that let this page-level scroll window
		// claim more vertical room than a small terminal actually had, so
		// even scrolling all the way to maxScroll still rendered past the
		// real screen edge -- content existed but nothing could reach the
		// last few lines of it, the exact failure bodyLayout's own floors
		// (see its doc comment) were already built to rule out everywhere
		// else. Confirmed live: Overview (and every other non-table view
		// routed through this same branch -- Blockers, Audit & health,
		// Activity, Archive search) hit this on any short-enough terminal,
		// matching visibleRowCount's identical max(0, h-4) survival floor
		// for row-list views instead (that "-4" is a different, legitimate
		// one: RowList.View's own header + footer rows, neither of which
		// bodyPrefixHeight measures).
		//
		// Every count below is in PHYSICAL lines, measured through
		// wrappedHeight, not in the "\n"-separated logical lines this
		// used to count. At a narrow enough contentW a single logical
		// line of prose wraps onto two or three physical ones, so a
		// window of availH logical lines rendered past the bottom of the
		// terminal by however many wrapped -- confirmed by rendering
		// Overview at 60x20 and 40x14 and counting: 21 and 16 physical
		// lines for 20 and 14 rows of terminal. The last line or two sat
		// below the screen edge with no way to scroll to them, which is
		// the exact failure this whole branch exists to prevent.
		heights := make([]int, len(lines))
		total := 0
		for i, line := range lines {
			heights[i] = wrappedHeight(line, contentW)
			total += heights[i]
		}
		availH := max(0, contentH)
		if total > availH {
			// One line of availH's own budget goes to the scroll indicator
			// appended below the window -- reserved only here, since it's
			// only ever added when scrolling is actually needed.
			windowH := max(1, availH-1)
			// maxScroll is the first line of the last window that still
			// fits, found by filling backwards from the end: with
			// variable-height lines it is no longer len(lines)-windowH.
			maxScroll, filled := len(lines), 0
			for i := len(lines) - 1; i >= 0; i-- {
				if filled+heights[i] > windowH {
					break
				}
				filled += heights[i]
				maxScroll = i
			}
			if m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			end, used := m.scrollOffset, 0
			for end < len(lines) && used+heights[end] <= windowH {
				used += heights[end]
				end++
			}
			if end == m.scrollOffset {
				// One line taller than the whole window on its own: show
				// it anyway anyway rather than an empty pane, and let the
				// pane clip it.
				end = m.scrollOffset + 1
			}
			content = strings.Join(lines[m.scrollOffset:end], "\n")
			// Truncated, not just styled: at a narrow contentW this
			// indicator is itself long enough to wrap, and the single
			// line reserved for it above would not have covered that.
			scrollInfo := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render(ansi.Truncate(
				fmt.Sprintf(" ⇡⇣ Scroll %d-%d of %d (PgUp/PgDn/Wheel)", m.scrollOffset+1, end, len(lines)),
				contentW, "…"),
			)
			content += "\n" + scrollInfo
		}
	}
	// Last word on height, for every view including the table ones the
	// scroll branch above skips: whatever the body ends up holding --
	// a row list plus its detail pane, a settings box, an inspector, an
	// error line -- it is cut to the room bodyLayout measured rather
	// than allowed to run off the bottom of the terminal. Project
	// settings and Contracts & decisions each did exactly that at 80x24,
	// one line past the edge, where nothing could scroll to it.
	content = clampHeight(content, contentW, contentH)
	body := prefix + content
	// The key bar is pinned to the foot of the pane rather than left
	// floating directly under whatever the content happened to be: padding
	// content out to contentH first keeps it in the same place on every
	// view and every refresh. contentH is exactly the room bodyLayout
	// reserved for content (bodySuffixHeight already held back this bar's
	// own lines), so the pad can never push the body past the terminal.
	if bar := m.bodySuffix(p); bar != "" {
		if pad := contentH - wrappedHeight(content, contentW); pad > 0 {
			body += strings.Repeat("\n", pad)
		}
		body += "\n" + bar
	}
	// The pane renders exactly what it was given room for, even when the
	// chrome alone does not fit. innerH floors at 1, so on a narrow
	// enough terminal the command rail and hub tabs can wrap onto more
	// lines than the whole pane has -- confirmed at 40x14 once a wider
	// sidebar pushed the tabs onto an extra row. Clamped here, at the
	// last moment, rather than trying to make every contributor above
	// shrink: h-2 is the pane's interior after its own Padding(1, 2).
	return pane.Render(clampHeight(body, contentW, max(1, h-2)))
}

// railToastMinWidth is the narrowest a toast badge may be squeezed to
// before the rail drops it entirely: below this it is a couple of
// letters and an ellipsis, which says less than nothing while still
// costing the actor its columns. The notification is not lost -- the
// event it reports is in the chain, and the rail's own seq counter
// moves with it.
const railToastMinWidth = 12

func (m Model) commandRail(p palette, width int) string {
	sequence := max(m.state.Integrity.ServerSequence, m.state.Integrity.CacheSequence)
	freshness := empty(m.state.Integrity.Connectivity, "LOCAL")
	hub := navigationHubs[m.activeHubIndex()].Name
	left := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("LIVE")
	if m.staleReads >= staleReadThreshold {
		left = lipgloss.NewStyle().Foreground(p.ink).Background(p.red).Bold(true).
			Render(fmt.Sprintf("⚠ STALE (%d failed reads)", m.staleReads))
	}
	detail := fmt.Sprintf("  %s / %s  ·  %s  ·  seq %d", hub, views[m.view], freshness, sequence)
	authority := strings.ToLower(string(m.state.Agents[m.actor].Role))
	// Actor ID alongside role, not role alone -- confirmed live as a real
	// gap: this rail used to show only "authority <role>", with no way to
	// see *which* locally-switched identity you're currently acting as
	// short of opening the actor-switch form again or running a separate
	// CLI command. That's exactly what let a wrong actor-switch go
	// unnoticed until an elevation check rejected it downstream.
	right := m.actor + " · " + empty(authority, "unknown")
	rightLen := lipgloss.Width(right)
	toast := ""
	if m.toastMsg != "" && time.Now().Before(m.toastExpiresAt) {
		toast = m.toastMsg
	}
	// Room left for a toast badge beside the actor, for a given detail
	// string: the rail's own gap keeps one column, and the badge two more
	// separating it from the actor.
	toastRoom := func(detail string) int {
		return width - lipgloss.Width(left) - lipgloss.Width(detail) - rightLen - 3
	}
	if width > 40 {
		// The short detail is the fallback for two different squeezes:
		// the rail not fitting at all, and a live toast having nowhere to
		// go. Without the second, a *wider* terminal could show fewer
		// notifications than a narrower one -- at 70 columns the long
		// detail left the badge 9 columns and it was dropped, while at 56
		// the first fallback had already fired and it fit.
		short := fmt.Sprintf("  %s / %s", hub, views[m.view])
		if lipgloss.Width(left)+lipgloss.Width(detail)+rightLen > width {
			detail = short
		} else if toast != "" && toastRoom(detail) < railToastMinWidth {
			detail = short
		}
	}
	leftLen := lipgloss.Width(left) + lipgloss.Width(detail)
	// The toast sits immediately left of the actor, at the top right --
	// one notification, in one place, where the eye already goes for
	// "who am I and what just happened". It used to occupy the rail's
	// LIVE/STALE slot on the far left *and* render again as a block under
	// the content, so a single background-commit notification appeared
	// twice on screen at opposite corners.
	//
	// Fitted into whatever room is left rather than simply prepended:
	// this rail is truncated from the right end to hold it to one line,
	// so an over-long toast would have pushed the actor off the screen
	// instead of shortening itself.
	rendered := lipgloss.NewStyle().Foreground(p.amber).Render(right)
	if room := toastRoom(detail); toast != "" && room >= railToastMinWidth {
		badge := lipgloss.NewStyle().Foreground(p.ink).Background(p.cyan).Bold(true).
			Render(" " + ansi.Truncate(toast, room-2, "…") + " ")
		rendered = badge + "  " + rendered
		rightLen = lipgloss.Width(rendered)
	}
	gap := max(1, width-leftLen-rightLen)
	rail := left + lipgloss.NewStyle().Foreground(p.muted).Render(detail) +
		strings.Repeat(" ", gap) + rendered
	// gap floors at 1: below the width>40 threshold the shortened-detail
	// fallback above never kicks in, so at a narrow enough width
	// leftLen+rightLen alone can already exceed width and the assembled
	// rail overflows it regardless of gap. bodyPrefixHeight assumes this
	// is exactly one line (lipgloss.Height on the raw string, which has
	// no embedded "\n" of its own); without truncating here, the pane's
	// own Width() then wrapped the untruncated overflow onto real extra
	// physical lines once actually rendered, silently invalidating that
	// assumption -- the exact bug class the row list's footer had.
	return ansi.Truncate(rail, width, "…")
}

// renderHubTabs also returns each tab's [start, end) column range within
// the rendered line (before the sidebar's own width is added) -- read back
// by mouse.go's hubTabAt, recomputed fresh from the same (m, p, width) a
// click arrived under, since View() has no way to hand this to the
// Update() call that handles the click.
func (m Model) renderHubTabs(p palette, width int) (view string, tabRange [][2]int) {
	hub := navigationHubs[m.activeHubIndex()]
	current := views[m.view]
	// entered distinguishes "this tab is open and its row list/settings
	// pane actually has keyboard focus" from "this tab is merely the one
	// last opened, while ↑/↓/←/→ are still just moving the hub cursor
	// around" -- confirmed live as a real, reported gap: nothing on
	// screen told those two states apart, so a key like "r" (refresh at
	// the hub level, but a real row action inside some views) did
	// something different depending on a mode with no visible indicator
	// at all for which one you were in.
	entered := m.rowFocus || m.settingsFocus
	tabs := make([]string, 0, len(hub.Views))
	tabRange = make([][2]int, len(hub.Views))
	col := 0
	for i, name := range hub.Views {
		label := name
		style := lipgloss.NewStyle().Foreground(p.muted).Padding(0, 1)
		if name == current {
			if entered {
				style = style.Foreground(p.ink).Background(p.cyan).Bold(true)
			} else {
				// Selected but not entered: cyan text on the ordinary
				// background, no fill -- deliberately lighter than the
				// solid "entered" look above, not just a different color
				// for its own sake.
				style = style.Foreground(p.cyan).Bold(true)
			}
		}
		rendered := style.Render(label)
		w := lipgloss.Width(rendered)
		tabRange[i] = [2]int{col, col + w}
		col += w
		if i < len(hub.Views)-1 {
			col++ // the " " separator strings.Join adds between tabs
		}
		tabs = append(tabs, rendered)
	}
	return lipgloss.NewStyle().Width(width).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.muted).Render(strings.Join(tabs, " ")), tabRange
}

// formMaxWidth is the width constraint renderForm's final Render applies --
// shared with formFieldLines so a standalone lipgloss.Height measurement of
// any one row (title, hint, a field line) wraps exactly the same way it
// will inside the real combined render. Lipgloss wraps each pre-existing
// line independently rather than reflowing across "\n" boundaries, so
// measuring one row against this same width in isolation is exact, not an
// approximation.
func (m Model) formMaxWidth() int {
	return max(4, m.contentWidth())
}

// formRows builds renderForm's row content plus, per m.inputs index, which
// row holds that field's own line -- read back by formFieldAtY (mouse.go)
// to translate a click into a field to focus, recomputed fresh from the
// same model state renderForm itself renders from (View() can't persist
// this for the Update() call that handles the click).
func (m Model) formRows(p palette) (rows []string, fieldLine []int) {
	title, hint := "Form", ""
	if m.formSpec != nil {
		title, hint = m.formSpec.Title, m.formSpec.Hint
	}
	rows = []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("EDIT / " + title),
		lipgloss.NewStyle().Foreground(p.muted).Render(hint),
		"",
	}
	fieldLine = make([]int, len(m.inputs))
	focusedIsPicker := false
	for i, input := range m.inputs {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.text)
		focused := i == m.formFocus
		if focused {
			marker = "▌ "
			style = style.Foreground(p.cyan).Bold(true)
		}
		var options []string
		if m.formSpec != nil && i < len(m.formSpec.Fields) {
			options = m.formSpec.Fields[i].Options
		}
		fieldLine[i] = len(rows)
		if len(options) > 0 {
			if focused {
				focusedIsPicker = true
			}
			rows = append(rows, style.Render(marker)+renderPickerField(style, input.Prompt, input.Value(), focused), "")
			continue
		}
		rows = append(rows, style.Render(marker)+input.View(), "")
	}
	formFooterParts := []string{}
	if focusedIsPicker {
		formFooterParts = append(formFooterParts, lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[←/→]")+" "+lipgloss.NewStyle().Foreground(p.muted).Render("cycle value"))
	}
	formFooterParts = append(formFooterParts,
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[tab/shift+tab]")+" "+lipgloss.NewStyle().Foreground(p.muted).Render("navigate"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[enter]")+" "+lipgloss.NewStyle().Foreground(p.muted).Render("submit"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[esc]")+" "+lipgloss.NewStyle().Foreground(p.muted).Render("cancel"),
	)
	rows = append(rows, strings.Join(formFooterParts, " · "))
	if m.notice != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.amber).Render(m.notice))
	}
	if m.err != nil {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.red).Render(m.err.Error()))
	}
	return rows, fieldLine
}
func (m Model) renderForm(p palette) string {
	rows, _ := m.formRows(p)
	return lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.cyan).PaddingLeft(2).MaxWidth(m.formMaxWidth()).
		Render(strings.Join(rows, "\n"))
}

func (m *Model) keepFormFocusVisible() {
	if m.form == "" || m.formFocus < 0 {
		return
	}
	p := colors()
	rows, positions := m.formRows(p)
	if m.formFocus >= len(positions) {
		return
	}
	fieldTop := 0
	for _, row := range rows[:positions[m.formFocus]] {
		fieldTop += wrappedHeight(row, max(1, m.contentWidth()-3))
	}
	_, _, _, available := m.bodyLayout(p)
	if fieldTop < m.scrollOffset {
		m.scrollOffset = fieldTop
	} else if fieldTop >= m.scrollOffset+max(1, available-1) {
		m.scrollOffset = max(0, fieldTop-available+2)
	}
}

// renderPickerField renders a picker field as "Label: ‹ value ›" instead of
// a raw textinput.Model.View() (which would show a blinking text cursor
// that's misleading here -- the value never accepts typed characters).
func renderPickerField(style lipgloss.Style, prompt, value string, focused bool) string {
	if !focused {
		return style.Render(prompt + value)
	}
	return style.Render(prompt) + style.Render("‹ "+value+" ›")
}

// scrollViewport wraps styled text into physical terminal lines before
// slicing it. The fixed height and one-line position indicator prevent a
// long inspector, form, or detail pane from growing over adjacent UI.
func scrollViewport(p palette, content string, width, height, offset int, keys string) string {
	if height <= 0 || width <= 0 || content == "" {
		return ""
	}
	wrapped := ansi.Hardwrap(content, width, true)
	lines := strings.Split(wrapped, "\n")
	if len(lines) <= height {
		return wrapped
	}
	if height == 1 {
		return ansi.Truncate(lines[viewportStart(content, width, height, offset)], width, "…")
	}
	window := height - 1
	start := viewportStart(content, width, height, offset)
	end := min(len(lines), start+window)
	indicator := fmt.Sprintf("↕ %d–%d/%d · %s", start+1, end, len(lines), keys)
	return strings.Join(lines[start:end], "\n") + "\n" +
		lipgloss.NewStyle().Foreground(p.cyan).Render(ansi.Truncate(indicator, width, "…"))
}

func viewportStart(content string, width, height, offset int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	lines := strings.Split(ansi.Hardwrap(content, width, true), "\n")
	if len(lines) <= height {
		return 0
	}
	return min(max(0, offset), len(lines)-max(1, height-1))
}

// tableDetail is the lower pane shared by row-list views. Inspecting takes
// precedence so the selected row's body is never pushed behind a secondary
// delivery/contract panel.
func (m Model) tableDetail(p palette, width int) string {
	if m.inspecting {
		return m.renderInspector(p, width)
	}
	switch views[m.view] {
	case "Invocations":
		return m.invocationDeliveryDetails(p, width)
	case "Runtimes":
		return m.runtimeDetailPane(p, width)
	case "Contracts & decisions":
		return decisionMessages(m.state)
	default:
		return ""
	}
}

func (m Model) tableDetailHeight(p palette, width, contentHeight int) int {
	if contentHeight <= 2 {
		return 0
	}
	detail := m.tableDetail(p, width)
	if detail == "" {
		return 0
	}
	// Preserve a table header and at least one selected row while giving
	// meaningful room to the detail. The detail itself scrolls separately.
	return min(wrappedHeight(detail, width), min(max(3, contentHeight/2), contentHeight-3))
}

// countOf renders a count with the noun form that agrees with it, so the
// overview reads "1 open task", not "1 open tasks".
func countOf(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func (m Model) overview(p palette) string {
	contentWidth := m.contentWidth()
	open, running, ready, online, inboxActions := 0, 0, 0, 0, 0
	for _, t := range m.state.Tasks {
		if !t.Archived && t.Status != "COMPLETED" && t.Status != "CANCELLED" {
			open++
		}
	}
	for _, agent := range m.state.Agents {
		if agent.Status == "ACTIVE" {
			ready++
		}
	}
	for _, runtime := range m.state.AgentRuntimes {
		if runtime.Status == "ONLINE" && runtime.Health != "DEGRADED" {
			online++
		}
	}
	for _, invocation := range m.state.Invocations {
		switch invocation.Status {
		case "RUNNING", "CLAIMED":
			running++
		}
	}
	for _, message := range m.state.Messages {
		if len(messageActionsFor(message, m.actor)) > 0 {
			inboxActions++
		}
	}
	agents := len(m.state.Agents)
	status := fmt.Sprintf(
		"%s  ·  %d can message  ·  %s  ·  %s  ·  %s  ·  %s",
		countOf(agents, "agent", "agents"), ready,
		countOf(online, "online runtime", "online runtimes"),
		countOf(open, "open task", "open tasks"),
		countOf(inboxActions, "inbox action", "inbox actions"),
		countOf(running, "running invocation", "running invocations"),
	)
	if contentWidth < 78 {
		status = fmt.Sprintf("%s · %d message-ready · %s online\n%s · %s · %s",
			countOf(agents, "agent", "agents"), ready,
			countOf(online, "runtime", "runtimes"),
			countOf(open, "open task", "open tasks"),
			countOf(inboxActions, "inbox action", "inbox actions"),
			countOf(running, "running invocation", "running invocations"))
	}
	workforceWidth := contentWidth
	attentionWidth := contentWidth
	if contentWidth >= 78 {
		attentionWidth = max(25, contentWidth/3)
		workforceWidth = contentWidth - attentionWidth - 2
	}
	workforce := m.section(p, "TEAM", "Messaging is independent of runtime presence", m.workforce(p, workforceWidth-4), workforceWidth)
	attention := m.section(p, "NEEDS ATTENTION", "Open work and delivery issues", m.attentionPreview(p, attentionWidth-4), attentionWidth)
	top := attention + "\n\n" + workforce
	if contentWidth >= 78 {
		top = lipgloss.JoinHorizontal(lipgloss.Top, workforce, "  ", attention)
	}
	activity := m.section(p, "RECENT EVENTS", fmt.Sprintf("%d signed events · durable project history", m.state.Integrity.EventCount), m.chain(p), contentWidth)
	// No key strip of its own any more: keyhints.go's bar renders the
	// overview's real keys (and the global ones) at the foot of the pane,
	// on every view rather than only this one.
	return lipgloss.NewStyle().Foreground(p.cyan).Render(status) + "\n\n" + top + "\n\n" + activity
}

func (m Model) section(p palette, title, subtitle, body string, width int) string {
	heading := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(title)
	description := lipgloss.NewStyle().Foreground(p.muted).Render(subtitle)
	return lipgloss.NewStyle().
		Width(max(4, width)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(p.muted).
		Padding(0, 1).
		Render(heading + "\n" + description + "\n\n" + body)
}

func (m Model) workforce(p palette, width int) string {
	if len(m.state.Agents) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No agents registered.")
	}
	rows := []string{}
	if width >= 68 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(
			fmt.Sprintf("%-9s %-13s %-14s %-10s %s", "MESSAGES", "RUNTIME", "AGENT", "ROLE", "CURRENT WORK"),
		))
	}
	for _, agentID := range service.SortedKeys(m.state.Agents) {
		agent := m.state.Agents[agentID]
		messageStatus := "READY"
		if agent.Status != "ACTIVE" {
			messageStatus = agent.Status
		}
		signal := "NO RUNTIME"
		if agent.PrincipalType == model.PrincipalHuman {
			signal = "HUMAN"
		}
		// An agent can have more than one AgentRuntime record (e.g. a
		// stale, revoked one left behind alongside the current live one --
		// registering a runtime never deletes an older entry under a
		// different runtime ID for the same agent). AgentRuntimes is a Go
		// map, so ranging over it visits entries in random order every
		// time; picking whichever match the loop happened to see last used
		// to make the displayed signal flip unpredictably between renders
		// -- e.g. HENRY showing "● ONLINE" one moment and "○ REVOKED" the
		// next for the exact same state, with no user action in between.
		// Prefer an eligible online runtime, then a degraded/draining one,
		// before showing the latest inactive record. A newly registered
		// but offline runtime must not hide another still-online route.
		var current *model.AgentRuntime
		rank := func(r model.AgentRuntime) int {
			switch {
			case r.Status == "ONLINE" && r.Health != "DEGRADED":
				return 3
			case r.Status == "ONLINE":
				return 2
			case r.Status == "DRAINING":
				return 1
			default:
				return 0
			}
		}
		currentAt := func(r model.AgentRuntime) time.Time {
			if !r.LastSeenAt.IsZero() {
				return r.LastSeenAt
			}
			return r.RegisteredAt
		}
		for id := range m.state.AgentRuntimes {
			runtime := m.state.AgentRuntimes[id]
			if runtime.AgentID != agentID {
				continue
			}
			if current == nil || rank(runtime) > rank(*current) ||
				(rank(runtime) == rank(*current) && (currentAt(runtime).After(currentAt(*current)) ||
					(currentAt(runtime).Equal(currentAt(*current)) && runtime.ID < current.ID))) {
				runtime := runtime
				current = &runtime
			}
		}
		if current != nil && agent.PrincipalType != model.PrincipalHuman {
			switch {
			case current.Health == "DEGRADED":
				signal = "DEGRADED"
			case current.Status == "ONLINE":
				signal = "ONLINE"
			case current.Status == "DRAINING":
				signal = "DRAINING"
			default:
				signal = current.Status
			}
		}
		// The most recently active work, not whichever ID sorts first.
		work := "none"
		for _, id := range model.SortedIDsBySequence(m.state.Invocations, func(inv model.Invocation) uint64 { return inv.UpdatedSequence }) {
			invocation := m.state.Invocations[id]
			if invocation.Target == agentID && (invocation.Status == "CLAIMED" || invocation.Status == "RUNNING" || invocation.Status == "WAITING") {
				work = strings.ToLower(invocation.Status) + " · " + invocation.Instruction
				break
			}
		}
		if work == "none" {
			for _, id := range model.SortedIDsBySequence(m.state.Tasks, func(t model.Task) uint64 { return t.UpdatedSequence }) {
				task := m.state.Tasks[id]
				if task.Owner == agentID && !task.Archived && task.Status != "COMPLETED" && task.Status != "CANCELLED" {
					work = strings.ToLower(task.Status) + " · " + task.Title
					break
				}
			}
		}
		// An agent's DisplayName is optional at registration (agent.go's
		// register form never required it) and empty for any agent that
		// registered without one -- rendering it verbatim left that row's
		// AGENT column blank, which read as missing data rather than what
		// it actually was: a name nobody set. Falling back to the agent's
		// ID (always present, always unique) means every row always shows
		// a real identity.
		// Prefer the display name; fall back to the ID. Under RFC 0039 an
		// agent ID starts with its provider, so the fallback shares a
		// 7-character prefix ("claude-") with every other agent of that
		// provider -- end-truncating it spends the column on the one part
		// that distinguishes nothing ("claude-review…"). Truncating from
		// the middle keeps both the provider and the tail that actually
		// identifies the agent.
		name := agent.DisplayName
		truncateName := truncate
		if name == "" {
			name, truncateName = agentID, truncateMiddle
		}
		if width < 68 {
			rows = append(rows, truncate(fmt.Sprintf("%s · %s · %s", name, messageStatus, signal), width))
			if work != "none" {
				rows = append(rows, truncate("  "+work, width))
			}
			continue
		}
		rows = append(rows, fmt.Sprintf(
			"%-9s %-13s %-14s %-10s %s",
			truncate(messageStatus, 9), truncate(signal, 13), truncateName(name, 13), truncate(strings.ToLower(string(agent.Role)), 10), truncate(work, max(1, width-51)),
		))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.muted).Render("No runtime = no automatic delivery; messages and requests still work."))
	return strings.Join(rows, "\n")
}
func (m Model) attentionRows() []string {
	// RFC 0041: newest activity first within each kind of attention item,
	// by signed event sequence. The preview shows only the first few rows,
	// so order decides what is visible; IDs are caller-chosen, not a clock.
	rows := []string{}
	for _, id := range model.SortedIDsBySequence(m.state.Tasks, func(t model.Task) uint64 { return t.UpdatedSequence }) {
		t := m.state.Tasks[id]
		if t.Archived || t.Status == "COMPLETED" || t.Status == "CANCELLED" {
			continue
		}
		if t.Status == "BLOCKED" {
			rows = append(rows, "! Blocked: "+t.Title)
		}
		if !t.LeaseUntil.IsZero() && time.Until(t.LeaseUntil) < time.Hour {
			rows = append(rows, "◷ Lease: "+t.Title+" · "+t.LeaseUntil.Local().Format("15:04"))
		}
	}
	for _, id := range model.SortedIDsBySequence(m.state.Approvals, func(a model.Approval) uint64 { return a.UpdatedSequence }) {
		a := m.state.Approvals[id]
		if a.Status == "PENDING" {
			rows = append(rows, "◆ "+a.ID+"  approval: "+a.Action)
		}
	}
	for _, id := range model.SortedIDsBySequence(m.state.Invocations, func(inv model.Invocation) uint64 { return inv.UpdatedSequence }) {
		invocation := m.state.Invocations[id]
		switch invocation.Status {
		case "WAITING":
			rows = append(rows, "◫ "+invocation.ID+"  "+invocation.Target+" waits: "+invocation.Reason)
		case "PENDING":
			rows = append(rows, "→ "+invocation.ID+"  pending delivery to "+invocation.Target)
		}
	}
	for _, id := range service.SortedKeys(m.state.InvocationDeliveries) {
		delivery := m.state.InvocationDeliveries[id]
		if delivery.Status == "FAILED" || delivery.Status == "EXHAUSTED" {
			rows = append(rows, "✕ "+delivery.InvocationID+"  delivery failed: "+delivery.Error)
		}
	}
	for _, id := range model.SortedIDsBySequence(m.state.AgentRuntimes, func(rt model.AgentRuntime) uint64 { return rt.UpdatedSequence }) {
		runtime := m.state.AgentRuntimes[id]
		if runtime.Status != "REVOKED" && runtime.Health == "DEGRADED" {
			rows = append(rows, "● "+runtime.ID+"  "+runtime.Status+" · "+runtime.Health)
		}
	}
	for _, id := range model.SortedIDsBySequence(m.state.Messages, func(msg model.Message) uint64 { return msg.CreatedSequence }) {
		message := m.state.Messages[id]
		if len(messageActionsFor(message, m.actor)) > 0 {
			rows = append(rows, "✉ "+message.Subject+" · from "+message.From)
		}
	}
	return rows
}

func (m Model) attention(p palette) string {
	rows := m.attentionRows()
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(p.cyan).Render("✓ CLEAR") + "\n" +
			lipgloss.NewStyle().Foreground(p.muted).Render("No intervention needed.")
	}
	return strings.Join(rows, "\n")
}

func (m Model) attentionPreview(p palette, width int) string {
	rows := m.attentionRows()
	if len(rows) == 0 {
		return m.attention(p)
	}
	const previewRows = 3
	visible := min(previewRows, len(rows))
	preview := make([]string, 0, visible+1)
	for _, row := range rows[:visible] {
		preview = append(preview, ansi.Truncate(row, max(1, width), "…"))
	}
	if remaining := len(rows) - visible; remaining > 0 {
		preview = append(preview, lipgloss.NewStyle().Foreground(p.cyan).Render(
			ansi.Truncate(fmt.Sprintf("+%d more · open related tabs", remaining), max(1, width), "…")))
	}
	return strings.Join(preview, "\n")
}

func (m Model) blockers(p palette) string {
	rows := []string{}
	for _, id := range model.SortedIDsBySequence(m.state.Tasks, func(t model.Task) uint64 { return t.UpdatedSequence }) {
		t := m.state.Tasks[id]
		if t.Status == "BLOCKED" {
			rows = append(rows, "! "+id+"  "+t.Title)
		}
	}
	if len(rows) == 0 {
		return "No active blockers."
	}
	return strings.Join(rows, "\n")
}
func (m Model) integrity(p palette) string {
	mark := "✓"
	if !m.state.Integrity.Verified {
		mark = "✕"
	}
	compatibility := lifecycleCompatibility(m.lifecycle)
	summary := fmt.Sprintf("%s Chain verified: %t\n  Signed events: %d\n  Head: %s\n  Consistency: %s\n  Connectivity: %s\n  Server sequence: %d\n  Cache sequence: %d\n\nProject lifecycle\n  Compatibility: %s\n  Installed build: %s\n  Project build: %s\n  Interrupted upgrade: %t",
		mark, m.state.Integrity.Verified, m.state.Integrity.EventCount, m.state.Integrity.Head,
		empty(m.state.Integrity.Consistency, "UNKNOWN"), empty(m.state.Integrity.Connectivity, "UNKNOWN"),
		m.state.Integrity.ServerSequence, m.state.Integrity.CacheSequence, compatibility,
		buildinfo.ResolvedBuildID(), empty(m.lifecycle.CurrentBuildID, "unrecorded"), m.lifecycle.Interrupted)
	return summary + "\n\n" + m.findingsSummary(p) + "\n\nRun `agent-comms verify` before incident recovery."
}

// findingsSummary renders the same doctor findings `agent-comms doctor`
// reports (internal/doctor.Findings) so a human never has to leave the TUI
// to see what's wrong with the project -- this is the one place that data
// previously had zero TUI presence at all.
func (m Model) findingsSummary(p palette) string {
	heading := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render("Doctor findings")
	if len(m.findings) == 0 {
		return heading + "\n  " + lipgloss.NewStyle().Foreground(p.cyan).Render("✓ No findings.")
	}
	rows := []string{heading}
	for _, f := range m.findings {
		color := p.amber
		if f.Severity == "ERROR" {
			color = p.red
		}
		rows = append(rows, "  "+lipgloss.NewStyle().Foreground(color).Bold(true).Render(f.Severity+" "+f.Code)+"  "+f.Message)
		if f.Guidance != "" {
			rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render("    "+f.Guidance))
		}
	}
	return strings.Join(rows, "\n")
}
func (m Model) chain(p palette) string {
	after := max(0, m.state.Integrity.EventCount-7)
	cursor := ""
	if after > 0 {
		cursor = controlplane.EncodeCursor(uint64(after))
	}
	page, e := m.svc.History(controlplane.PageRequest{Cursor: cursor, Limit: 7})
	if e != nil {
		return e.Error()
	}
	rows := []string{}
	for i, record := range page.Items {
		v := record.Event
		joint := "├─"
		if i == len(page.Items)-1 {
			joint = "└─"
		}
		rows = append(rows, fmt.Sprintf("%s %04d  %-22s %s · %s", joint, v.Sequence, v.Type, v.Actor, v.EntityID))
	}
	if len(rows) == 0 {
		return "○ No durable events yet."
	}
	return strings.Join(rows, "\n")
}
func (m Model) archive(p palette) string {
	n := 0
	for _, t := range m.state.Tasks {
		if t.Archived {
			n++
		}
	}
	return fmt.Sprintf("%d archived tasks remain in immutable history.\n\nUse `agent-comms history --grep <query> --all` for full-text event search or `agent-comms export markdown` for a review packet.", n)
}

// paletteLayout builds the command palette's unplaced panel content --
// shared by renderPalette (which centers it on screen) and paletteMatchAt
// (mouse.go, which needs the exact same row layout to hit-test a click,
// recomputed fresh the same way hubTabAt/sidebarHubAt/rowAtY already do,
// since View() has no way to hand this to the Update() call that handles
// a click). matchLine[i] is the row offset within the returned panel
// string (0-based physical row, including border/padding but before
// centering) of paletteMatches()'s i-th entry.
// paletteVisibleCount is how many match rows the full-size palette can
// show after its border, padding, title, query, list heading, scroll line,
// footer, and spacing. Smaller terminals use compactPaletteLayout instead.
func paletteVisibleCount(height int) int {
	return max(1, height-paletteChromeHeight)
}

// paletteChromeHeight counts the scroll line too, which only renders when
// the list is actually windowed -- deliberately conservative by that one
// line in the case where everything fits.
const paletteChromeHeight = 15

// paletteWindow is the slice of matches to render for a list that doesn't
// fit: the selection kept roughly centered, clamped so the window never
// runs off either end.
func paletteWindow(total, selected, visible int) (start, end int) {
	if total <= visible {
		return 0, total
	}
	start = selected - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m Model) paletteLayout(p palette) (panel string, matchLine []int) {
	if m.width < 44 || m.height < 16 {
		return m.compactPaletteLayout(p)
	}
	width := min(68, m.width-8)
	// An empty box said nothing about what to do with it; the list below
	// is long enough now that "type" is not the only useful answer.
	placeholder := ""
	if m.query == "" {
		placeholder = lipgloss.NewStyle().Foreground(p.muted).Render(" type to filter")
	}
	rows := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("COMMANDS"),
		lipgloss.NewStyle().Foreground(p.muted).Render("Go to a workspace or start an action."),
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("Command"),
		lipgloss.NewStyle().Width(width-6).Foreground(p.text).
			Padding(0, 1).Render("> " + m.query + "█" + placeholder),
		"",
	}
	matches := m.paletteMatches()
	if len(matches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.amber).Render("No matching command"))
	} else {
		heading := fmt.Sprintf("Matches (%d)", len(matches))
		if strings.TrimSpace(m.query) == "" {
			heading = fmt.Sprintf("All commands (%d)", len(matches))
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(heading))
		// UX-11: highlight (and, in updatePalette, actually apply) the
		// keyboard-selected row, not always index 0 -- previously Up/Down
		// had no effect on either the highlight or what Enter did.
		selected := m.paletteSelectedIndex()
		start, end := paletteWindow(len(matches), selected, paletteVisibleCount(m.height))
		// -1 for every match outside the window: matchLine is indexed by
		// match, not by row, so paletteMatchAt can still map a click on
		// the third *visible* row to the right match while the list is
		// scrolled. paletteMatchAt skips the negatives (a click above the
		// panel produces a negative relative Y, which would otherwise
		// match one of them).
		matchLine = make([]int, len(matches))
		for i := range matchLine {
			matchLine[i] = -1
		}
		// rowWidth is the panel's interior: Width(width) less its border
		// (1 column each side) and Padding(1, 2) (2 more each side). The
		// query box above already renders at exactly this width, which is
		// what makes the selected row's fill line up with it.
		rowWidth := width - 6
		for index := start; index < end; index++ {
			marker := "  "
			if index == selected {
				marker = "› "
			}
			// The selected row is a filled bar across the whole panel,
			// not cyan text with a small marker. It reads as "this is
			// what Enter will open" at a glance, and matches how the row
			// tables elsewhere in the app mark their cursor -- the
			// palette was the one list where the selection was easy to
			// lose track of while scrolling.
			text, tag := marker+matches[index].label, matches[index].kind
			pad := rowWidth - lipgloss.Width(text) - lipgloss.Width(tag)
			if pad < 1 {
				text = ansi.Truncate(text, max(1, rowWidth-lipgloss.Width(tag)-1), "…")
				pad = max(1, rowWidth-lipgloss.Width(text)-lipgloss.Width(tag))
			}
			matchLine[index] = len(rows)
			if index == selected {
				rows = append(rows, lipgloss.NewStyle().Foreground(p.ink).Background(p.cyan).Bold(true).
					Render(text+strings.Repeat(" ", pad)+tag))
				continue
			}
			rows = append(rows, lipgloss.NewStyle().Foreground(p.text).Render(text)+
				strings.Repeat(" ", pad)+lipgloss.NewStyle().Foreground(p.muted).Render(tag))
		}
		if start > 0 || end < len(matches) {
			rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(
				fmt.Sprintf("  showing %d-%d of %d", start+1, end, len(matches))))
		}
	}
	// ↑/↓ was missing entirely: the panel offered a selection cursor and
	// never said what moved it.
	paletteFooter := renderHintRow(p, []keyHint{
		{"[↑/↓]", "choose"}, {"[enter]", "open"}, {"[type]", "filter"}, {"[esc]", "close"},
	}, width-6)
	rows = append(rows, "", paletteFooter)
	// Bordered, not filled: the cyan border is what separates the palette
	// from the screen. Filling it with p.ink made it a black card, which
	// on a terminal with a background image is the one thing on screen
	// that blanks it out.
	panel = lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).
		BorderForeground(p.cyan).Foreground(p.text).Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	for i := range matchLine {
		if matchLine[i] >= 0 {
			matchLine[i] += 2 // top border + top padding
		}
	}
	return panel, matchLine
}

// compactPaletteLayout retains the same searchable command list and cursor
// on small terminals, but drops decorative chrome rather than letting a
// 16-line, 36-column dialog extend beyond the physical screen.
func (m Model) compactPaletteLayout(p palette) (string, []int) {
	width := max(8, min(68, m.width-2))
	inner := max(1, width-4)
	matches := m.paletteMatches()
	selected := m.paletteSelectedIndex()
	visible := max(1, m.height-7)
	start, end := paletteWindow(len(matches), selected, visible)
	rows := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("COMMANDS"),
		lipgloss.NewStyle().Foreground(p.text).Render(ansi.Truncate("> "+m.query+"█", inner, "…")),
		lipgloss.NewStyle().Foreground(p.muted).Render(fmt.Sprintf("%d commands · %d-%d", len(matches), start+1, end)),
	}
	matchLine := make([]int, len(matches))
	for i := range matchLine {
		matchLine[i] = -1
	}
	for i := start; i < end; i++ {
		label := ansi.Truncate("› "+matches[i].label, inner, "…")
		matchLine[i] = len(rows)
		style := lipgloss.NewStyle().Foreground(p.text)
		if i == selected {
			style = style.Foreground(p.ink).Background(p.cyan).Bold(true)
		}
		rows = append(rows, style.Render(label))
	}
	rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(ansi.Truncate("↑↓ choose · Enter open · Esc close", inner, "…")))
	panel := lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).
		BorderForeground(p.cyan).Padding(0, 1).Render(strings.Join(rows, "\n"))
	for i := range matchLine {
		if matchLine[i] >= 0 {
			matchLine[i]++ // top border; compact panel has no top padding
		}
	}
	return panel, matchLine
}

func (m Model) renderPalette(p palette, under string) string {
	panel, _ := m.paletteLayout(p)
	// Plain whitespace around the panel, not an ink-filled backdrop: the
	// area around the palette is the terminal's own, the same as every
	// other surface. (It is still blank rather than showing the view
	// underneath -- Place builds a fresh screen -- which is what keeps a
	// half-legible view from competing with the command list.)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
}

// paletteMatch is one candidate the command palette offers for the current
// query -- either a named command (paletteCommands) or a bare view to
// navigate to. This is the single source both the rendered/highlighted
// match list (paletteLayout) and what actually runs (updatePalette's Enter
// and click handling) read from -- confirmed live as a real, confusing
// defect without this: the highlighted "top match" and what Enter actually
// executed used to come from two independently-written matching
// implementations that had silently drifted apart, so the UI could
// confidently highlight "new task" while Enter did something else, or
// nothing at all, since typing it could never even produce that exact
// string in the first place (see updatePalette's "space" case).
type paletteMatch struct {
	label string
	// kind is the right-aligned tag on the row: "action" for a named
	// command that does something, "go to" for a bare view that only
	// navigates. The two were indistinguishable in a single flat list --
	// "new decision" and "Contracts & decisions" sat one above the other
	// with nothing saying that one opens a form and the other just
	// changes tab.
	kind  string
	apply func(Model) (tea.Model, tea.Cmd)
}

// paletteSelectedIndex clamps m.paletteSelected into the current match
// list's bounds, so a query change that shrinks the list (or an index left
// over from a previous open) never selects a row that no longer exists.
func (m Model) paletteSelectedIndex() int {
	n := len(m.paletteMatches())
	if n == 0 {
		return 0
	}
	if m.paletteSelected < 0 {
		return 0
	}
	if m.paletteSelected >= n {
		return n - 1
	}
	return m.paletteSelected
}

func (m Model) paletteMatches() []paletteMatch {
	query := strings.ToLower(strings.TrimSpace(m.query))
	var matches []paletteMatch
	// Every match, not the first six: with an empty query this is the
	// app's full command list, which is what the palette is for -- the
	// old cap meant opening it showed six "new ..." commands and no way
	// to discover the other twenty-two, and even a typed query silently
	// hid matches past the sixth. paletteLayout scrolls whatever doesn't
	// fit the screen instead of dropping it.
	for _, cmd := range paletteCommands() {
		names := append([]string{cmd.label}, cmd.aliases...)
		hit := query == ""
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), query) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		cmd := cmd
		matches = append(matches, paletteMatch{label: cmd.label, kind: "action", apply: func(value Model) (tea.Model, tea.Cmd) {
			value.openView(cmd.view)
			return cmd.open(value)
		}})
	}
	for _, name := range views {
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		name := name
		matches = append(matches, paletteMatch{label: name, kind: "go to", apply: func(value Model) (tea.Model, tea.Cmd) {
			value.openView(name)
			value.focusCurrentView()
			value.palette, value.query = false, ""
			return value, nil
		}})
	}
	return matches
}
func empty(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (m Model) renderInspector(p palette, width int) string {
	list := m.activeRowList()
	if list == nil {
		return ""
	}
	id := list.SelectedID(m.state, m.actor)
	if id == "" {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No row selected for inspector.")
	}
	v := views[m.view]
	var lines []string
	headerStyle := lipgloss.NewStyle().Foreground(p.cyan).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(p.text).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(p.muted)

	lines = append(lines, headerStyle.Render("🔍 INSPECTOR / "+v+" / "+id))

	switch v {
	case "Tasks", "My work":
		if t, ok := m.state.Tasks[id]; ok {
			lines = append(lines, titleStyle.Render("Title: ")+t.Title)
			lines = append(lines, entityTimesLine(mutedStyle, t.CreatedAt, t.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Status: %s  |  Owner: %s  |  Branch: %s", fmtStatus(t.Status), empty(t.Owner, "unassigned"), t.Branch)))
			lines = append(lines, mutedStyle.Render("Resources: ")+strings.Join(t.Resources, ", "))
			if !t.LeaseUntil.IsZero() {
				lines = append(lines, mutedStyle.Render("Lease Expires: ")+t.LeaseUntil.Local().Format("15:04:05 (2006-01-02)"))
			}
			if t.Summary != "" {
				lines = append(lines, titleStyle.Render("Summary: ")+t.Summary)
			}
		}
	case "Inbox":
		if msg, ok := m.state.Messages[id]; ok {
			lines = append(lines, titleStyle.Render("Subject: ")+msg.Subject)
			lines = append(lines, entityTimesLine(mutedStyle, msg.CreatedAt, msg.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("From: %s  ->  To: %s  |  Kind: %s  |  Status: %s", msg.From, strings.Join(msg.To, ", "), msg.Kind, fmtStatus(msg.Status))))
			lines = append(lines, titleStyle.Render("Body: ")+msg.Body)
		}
	case "Invocations":
		if inv, ok := m.state.Invocations[id]; ok {
			lines = append(lines, titleStyle.Render("Instruction: ")+inv.Instruction)
			lines = append(lines, entityTimesLine(mutedStyle, inv.CreatedAt, inv.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Target: %s  |  RequestedBy: %s  |  Priority: %s  |  Status: %s", inv.Target, inv.RequestedBy, inv.Priority, fmtStatus(inv.Status))))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Consumer Mode: %s  |  Preferred Runtime: %s", empty(string(inv.ConsumerMode), "EITHER"), empty(inv.PreferredRuntimeID, "automatic"))))
			if inv.Reason != "" {
				lines = append(lines, titleStyle.Render("Reason: ")+inv.Reason)
			}
		}
	case "Agents":
		if ag, ok := m.state.Agents[id]; ok {
			lines = append(lines, titleStyle.Render("Display Name: ")+empty(ag.DisplayName, ag.ID))
			lines = append(lines, entityTimesLine(mutedStyle, ag.CreatedAt, ag.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Status: %s  |  Role: %s  |  Type: %s", fmtStatus(ag.Status), string(ag.Role), string(ag.PrincipalType))))
			lines = append(lines, mutedStyle.Render("Provider: ")+agentProviderLabel(m.state, ag))
			lines = append(lines, mutedStyle.Render("Scopes: ")+strings.Join(ag.Scopes, ", "))
			lines = append(lines, mutedStyle.Render("Capabilities: ")+strings.Join(ag.Capabilities, ", "))
			lines = append(lines, mutedStyle.Render("Fingerprint: ")+ag.KeyFingerprint)
		}
	case "Runtimes":
		if r, ok := m.state.AgentRuntimes[id]; ok {
			lines = append(lines, titleStyle.Render("Agent ID: ")+r.AgentID)
			lines = append(lines, entityTimesLine(mutedStyle, r.CreatedAt, r.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Status: %s  |  Health: %s  |  Kind: %s  |  Connector: %s", fmtStatus(r.Status), r.Health, r.Kind, r.Connector)))
			lines = append(lines, mutedStyle.Render("Host ID: ")+r.HostID)
		}
	case "Approvals":
		if app, ok := m.state.Approvals[id]; ok {
			status := approvalDisplayStatus(app, time.Now())
			lines = append(lines, titleStyle.Render("Action: ")+app.Action)
			lines = append(lines, entityTimesLine(mutedStyle, app.CreatedAt, app.UpdatedAt))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Tier: %s  |  Status: %s  |  Requester: %s", app.Tier, fmtStatus(status), app.Requester)))
			lines = append(lines, titleStyle.Render("Reason: ")+app.Reason)
			if app.ExpiresAt != nil {
				lines = append(lines, mutedStyle.Render("Expires: ")+app.ExpiresAt.Local().Format(time.RFC3339))
			}
			if status == "EXPIRED" {
				lines = append(lines, mutedStyle.Render("Recorded as "+app.Status+", but this approval can no longer authorize the action. Request a new approval."))
			}
			if app.Subject != "" {
				lines = append(lines, titleStyle.Render("Reviewed operation: ")+app.Subject)
			}
		}
	default:
		lines = append(lines, mutedStyle.Render("ID: ")+id)
	}

	return lipgloss.NewStyle().
		Width(max(4, width-2)).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.cyan).
		PaddingLeft(1).
		Render(strings.Join(lines, "\n"))
}

func entityTimesLine(style lipgloss.Style, created, updated time.Time) string {
	format := func(at time.Time) string {
		if at.IsZero() {
			return "unknown"
		}
		return at.Local().Format(time.RFC3339)
	}
	return style.Render("Created: " + format(created) + "  |  Updated: " + format(updated))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// truncateMiddle shortens value to width by eliding its middle rather than
// its tail, so a value whose leading characters are shared with its
// neighbours ("claude-reviewer", "claude-developer") stays distinguishable.
// Falls back to plain truncation below the width where a middle elision
// could still show something from both ends.
func truncateMiddle(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return truncate(value, width)
	}
	if width < 5 {
		return truncate(value, width)
	}
	keep := width - 1
	head := (keep + 1) / 2
	tail := keep - head
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

// wrapText greedily word-wraps plain (unstyled) text to width, joining
// wrapped lines with "\n". Written to replace, not lean on, lipgloss's own
// Width()-triggered implicit wrap for a multi-line block: confirmed live
// that lipgloss's wrapper can render several columns *wider* than the
// requested width for specific (width, text) combinations involving a
// hyphenated word near a line-break boundary -- e.g. this exact text,
// "Plain-text, project-scoped configuration values -- never store secrets
// here.", rendered 50 columns wide when asked for 47, 48, or 49 (correct at
// every other width from 40-56 tested around it). That widened box then got
// clipped by the outer screen's own MaxWidth(m.width), splitting its own
// border mid-line -- the visible "UI breaking" on Project settings at
// certain terminal sizes. This function never overshoots width for any
// input, confirmed by sweeping the exact failing case above.
//
// Must run on plain text before any styling is applied (never *.Render()'d
// input): word-splitting a string with embedded ANSI escape codes would
// treat control bytes as ordinary characters, the same class of corruption
// truncate() caused when it was once run on an already-styled string (see
// TestSidebarTitleSurvivesCompactFallback's history). Callers needing
// colored output should style the wrapped, multi-line result afterward,
// not the other way around.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	lines := make([]string, 0, 4)
	current := words[0]
	for _, word := range words[1:] {
		if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
			current += " " + word
			continue
		}
		lines = append(lines, current)
		current = word
	}
	lines = append(lines, current)
	return strings.Join(lines, "\n")
}

// Run starts the TUI program against in/out. opts is appended after the
// input/output options are set, so a caller can pass e.g.
// tea.WithColorProfile(...) or tea.WithEnvironment(...) to override
// bubbletea's default terminal auto-detection -- needed by callers (like the
// WASM entrypoint) whose out is not a real tty and can't be auto-detected
// from at all. Real CLI callers pass no opts and get the previous behavior
// unchanged.
