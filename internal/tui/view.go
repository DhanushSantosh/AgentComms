package tui

import (
	"fmt"
	"image/color"
	"sort"
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

type palette struct{ ink, panel, cyan, amber, red, violet, muted, text color.Color }

func colors(high bool) palette {
	if high {
		return palette{lipgloss.Color("#000000"), lipgloss.Color("#111111"), lipgloss.Color("#00FFFF"), lipgloss.Color("#FFFF00"), lipgloss.Color("#FF4444"), lipgloss.Color("#DD88FF"), lipgloss.Color("#BBBBBB"), lipgloss.Color("#FFFFFF")}
	}
	return palette{lipgloss.Color("#071216"), lipgloss.Color("#0D2024"), lipgloss.Color("#56D6C9"), lipgloss.Color("#E8B85C"), lipgloss.Color("#F07167"), lipgloss.Color("#B9A7E8"), lipgloss.Color("#78918F"), lipgloss.Color("#D7E5E3")}
}

// contentWidth is the width-only half of bodyLayout, split out so
// bodyPrefixHeight (which needs to measure the real command rail/tabs/
// header at the width they actually render at) can use it without calling
// bodyLayout itself -- bodyLayout's own innerH now depends on
// bodyPrefixHeight's measurement, and bodyPrefixHeight depending back on
// bodyLayout would be a cycle.
func (m Model) contentWidth() int {
	paneW := max(10, m.width-m.sidebarWidth()-3)
	return max(6, paneW-4)
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
	innerH = max(1, paneH-m.bodyPrefixHeight(p)-1)
	return
}
func (m Model) View() tea.View {
	p := colors(m.highContrast)
	sidebarW := m.sidebarWidth()
	paneW, paneH, _, _ := m.bodyLayout(p)
	side, _ := m.renderSidebar(p, sidebarW, paneH)
	body := m.renderBody(p, paneW, paneH)
	screen := lipgloss.JoinHorizontal(lipgloss.Top, side, " ", body)
	screen = lipgloss.NewStyle().MaxWidth(m.width).Render(screen)
	if m.palette {
		screen = m.renderPalette(p, screen)
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Agent Comms · Project Control"
	return v
}
func (m Model) sidebarWidth() int {
	if m.width < 60 {
		return 14
	}
	if m.width < 72 {
		return 16
	}
	return 21
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

func (m Model) renderSidebar(p palette, w, h int) (view string, hubLine []int) {
	titleStyle := lipgloss.NewStyle().Foreground(p.cyan).Bold(true)
	title := titleStyle.Render(sidebarTitleText)
	sub := lipgloss.NewStyle().Foreground(p.muted).Render(truncate(m.projectID, max(8, w-2)))
	activeHub := m.activeHubIndex()

	rows := []string{title, sub, "", lipgloss.NewStyle().Foreground(p.muted).Render("OPERATIONS"), ""}
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
			rows = append(rows, lipgloss.NewStyle().Foreground(p.text).PaddingLeft(2).
				Render("└ "+truncate(views[m.view], max(8, w-5))), "")
		}
	}
	rows = append(rows,
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("↑↓ hub  ←→ tab"),
		lipgloss.NewStyle().Foreground(p.muted).Render("Enter open  Esc back"),
		lipgloss.NewStyle().Foreground(p.muted).Render("[/] commands"),
	)
	// Padding(1) costs 2 more lines (top+bottom) than rows itself. Unlike
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
	if len(rows)+2 > h {
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
	return lipgloss.NewStyle().Width(w).Height(h).Padding(1).Background(p.ink).Foreground(p.text).Render(strings.Join(rows, "\n")), hubLine
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
	pane := lipgloss.NewStyle().Width(w).Height(h).Padding(1, 2).Background(p.panel).Foreground(p.text)
	if m.form != "" {
		content := m.renderForm(p)
		return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content)
	}
	if m.confirm != nil {
		content := m.renderConfirm(p)
		return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content)
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
		bodyContent = m.invocationList.View(p, m.state, m.actor, listW, listH) + "\n\n" +
			m.invocationDeliveryDetails(p, contentW)
	case "Runtimes":
		bodyContent = m.runtimeList.View(p, m.state, m.actor, listW, listH) + "\n\n" +
			m.runtimeDetailPane(p, contentW)
	case "Approvals":
		bodyContent = m.approvalList.View(p, m.state, m.actor, listW, listH)
	case "Documents":
		bodyContent = m.documentList.View(p, m.state, m.actor, listW, listH)
	case "Contracts & decisions":
		bodyContent = m.decisionList.View(p, m.state, m.actor, listW, listH)
		if contracts := decisionMessages(m.state); contracts != "" {
			bodyContent += "\n\n" + wrap.Render(contracts)
		}
	case "Artifacts":
		bodyContent = m.artifactList.View(p, m.state, m.actor, listW, listH)
	case "Drafts":
		bodyContent = m.draftsView(p)
	case "Environment":
		bodyContent = m.envList.View(p, m.state, m.actor, listW, listH)
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
	if m.inspecting {
		if inspector := m.renderInspector(p, contentW); inspector != "" {
			content += "\n\n" + inspector
		}
	}
	if m.err != nil {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.red).MaxWidth(contentW).Render("Error: "+m.err.Error())
	} else if m.toastMsg != "" && time.Now().Before(m.toastExpiresAt) {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.ink).Background(p.cyan).Bold(true).MaxWidth(contentW).Render(" "+m.toastMsg+" ")
	} else if m.notice != "" {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.cyan).MaxWidth(contentW).Render("Notice: "+m.notice)
	}
	isTable := m.activeRowList() != nil
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
		availH := max(0, contentH)
		if len(lines) > availH {
			// One line of availH's own budget goes to the scroll indicator
			// appended below the window -- reserved only here, since it's
			// only ever added when scrolling is actually needed.
			windowH := max(1, availH-1)
			maxScroll := len(lines) - windowH
			if m.scrollOffset > maxScroll {
				m.scrollOffset = maxScroll
			}
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			end := min(len(lines), m.scrollOffset+windowH)
			content = strings.Join(lines[m.scrollOffset:end], "\n")
			scrollInfo := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render(
				fmt.Sprintf(" ⇡⇣ Scroll %d-%d of %d (PgUp/PgDn/Wheel)", m.scrollOffset+1, end, len(lines)),
			)
			content += "\n" + scrollInfo
		}
	}
	return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content)
}

func (m Model) commandRail(p palette, width int) string {
	sequence := max(m.state.Integrity.ServerSequence, m.state.Integrity.CacheSequence)
	freshness := empty(m.state.Integrity.Connectivity, "LOCAL")
	hub := navigationHubs[m.activeHubIndex()].Name
	left := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("LIVE")
	switch {
	case m.staleReads >= staleReadThreshold:
		// Takes priority over an in-flight toast: reads have been failing,
		// so no new toast could have fired recently anyway (toastMsg only
		// ever gets set inside a *successful* refreshSilent) -- an old one
		// still fading out is less urgent than "this data might be stale."
		left = lipgloss.NewStyle().Foreground(p.ink).Background(p.red).Bold(true).
			Render(fmt.Sprintf("⚠ STALE (%d failed reads)", m.staleReads))
	case m.toastMsg != "" && time.Now().Before(m.toastExpiresAt):
		left = lipgloss.NewStyle().Foreground(p.ink).Background(p.cyan).Bold(true).Render(m.toastMsg)
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
	leftLen := lipgloss.Width(left) + lipgloss.Width(detail)
	rightLen := lipgloss.Width(right)
	if leftLen+rightLen > width && width > 40 {
		detail = fmt.Sprintf("  %s / %s", hub, views[m.view])
		leftLen = lipgloss.Width(left) + lipgloss.Width(detail)
	}
	gap := max(1, width-leftLen-rightLen)
	rail := left + lipgloss.NewStyle().Foreground(p.muted).Render(detail) +
		strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(p.amber).Render(right)
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
	return max(40, m.width-m.sidebarWidth()-10)
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

// renderPickerField renders a picker field as "Label: ‹ value ›" instead of
// a raw textinput.Model.View() (which would show a blinking text cursor
// that's misleading here -- the value never accepts typed characters).
func renderPickerField(style lipgloss.Style, prompt, value string, focused bool) string {
	if !focused {
		return style.Render(prompt + value)
	}
	return style.Render(prompt) + style.Render("‹ "+value+" ›")
}

func (m Model) overview(p palette) string {
	contentWidth := max(28, m.width-m.sidebarWidth()-7)
	open, running := 0, 0
	for _, t := range m.state.Tasks {
		if !t.Archived && t.Status != "COMPLETED" && t.Status != "CANCELLED" {
			open++
		}
	}
	for _, invocation := range m.state.Invocations {
		switch invocation.Status {
		case "RUNNING", "CLAIMED":
			running++
		}
	}
	status := fmt.Sprintf(
		"%d agents  ·  %d active tasks  ·  %d active invocations  ·  %d signed events",
		len(m.state.Agents), open, running, m.state.Integrity.EventCount,
	)
	workforceWidth := contentWidth
	attentionWidth := contentWidth
	if contentWidth >= 78 {
		attentionWidth = max(25, contentWidth/3)
		workforceWidth = contentWidth - attentionWidth - 2
	}
	workforce := m.section(p, "AGENT WORKFORCE", "signal / identity / current obligation", m.workforce(p, workforceWidth-4), workforceWidth)
	attention := m.section(p, "ATTENTION", "items requiring intervention", m.attention(p), attentionWidth)
	top := workforce + "\n\n" + attention
	if contentWidth >= 78 {
		top = lipgloss.JoinHorizontal(lipgloss.Top, workforce, "  ", attention)
	}
	activity := m.section(p, "LIVE ACTIVITY", "append-only project history", m.chain(p), contentWidth)
	keyParts := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[g]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("agents"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[i]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("invocations"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[n]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("create"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[r]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("refresh"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[/]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("commands"),
	}
	keys := strings.Join(keyParts, " · ")
	return lipgloss.NewStyle().Foreground(p.cyan).Render(status) + "\n\n" + top + "\n\n" + activity + "\n\n" + keys
}

func (m Model) section(p palette, title, subtitle, body string, width int) string {
	heading := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(title)
	description := lipgloss.NewStyle().Foreground(p.muted).Render(subtitle)
	return lipgloss.NewStyle().
		Width(max(20, width)).
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
	if width >= 54 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(
			fmt.Sprintf("%-12s %-14s %-10s %s", "SIGNAL", "AGENT", "ROLE", "CURRENT WORK"),
		))
	}
	for _, agentID := range service.SortedKeys(m.state.Agents) {
		agent := m.state.Agents[agentID]
		signal := "○ OFFLINE"
		if agent.PrincipalType == model.PrincipalHuman {
			signal = "◆ CONTROL"
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
		// Deterministic fix: among every runtime for this agent, keep the
		// one most recently seen (falling back to registration time for a
		// runtime that's never reported in), so the signal reflects
		// reality -- the most current runtime -- the same way on every
		// render.
		var current *model.AgentRuntime
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
			if current == nil || currentAt(runtime).After(currentAt(*current)) {
				runtime := runtime
				current = &runtime
			}
		}
		if current != nil {
			switch {
			case current.Health == "DEGRADED":
				signal = "▲ DEGRADED"
			case current.Status == "ONLINE":
				signal = "● ONLINE"
			case current.Status == "DRAINING":
				signal = "◐ DRAINING"
			default:
				signal = "○ " + current.Status
			}
		}
		work := "available"
		for _, invocation := range m.state.Invocations {
			if invocation.Target == agentID && (invocation.Status == "CLAIMED" || invocation.Status == "RUNNING" || invocation.Status == "WAITING") {
				work = strings.ToLower(invocation.Status) + " · " + invocation.Instruction
				break
			}
		}
		if work == "available" {
			for _, task := range m.state.Tasks {
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
		name := empty(agent.DisplayName, agentID)
		if width < 54 {
			rows = append(rows, fmt.Sprintf("%-12s %s\n             %s", signal, name, truncate(work, width-13)))
			continue
		}
		rows = append(rows, fmt.Sprintf(
			"%-12s %-14s %-10s %s",
			signal, truncate(name, 13), strings.ToLower(string(agent.Role)), truncate(work, max(10, width-42)),
		))
	}
	return strings.Join(rows, "\n")
}
func (m Model) attention(p palette) string {
	rows := []string{}
	for _, t := range m.state.Tasks {
		if t.Status == "BLOCKED" {
			rows = append(rows, "! "+t.ID+"  "+t.Title+" is blocked")
		}
		if !t.LeaseUntil.IsZero() && time.Until(t.LeaseUntil) < time.Hour {
			rows = append(rows, "◷ "+t.ID+"  lease expires "+t.LeaseUntil.Local().Format("15:04"))
		}
	}
	for _, a := range m.state.Approvals {
		if a.Status == "PENDING" {
			rows = append(rows, "◆ "+a.ID+"  approval: "+a.Action)
		}
	}
	for _, invocation := range m.state.Invocations {
		switch invocation.Status {
		case "WAITING":
			rows = append(rows, "◫ "+invocation.ID+"  "+invocation.Target+" waits: "+invocation.Reason)
		case "PENDING":
			rows = append(rows, "→ "+invocation.ID+"  pending delivery to "+invocation.Target)
		}
	}
	for _, delivery := range m.state.InvocationDeliveries {
		if delivery.Status == "FAILED" || delivery.Status == "EXHAUSTED" {
			rows = append(rows, "✕ "+delivery.InvocationID+"  delivery failed: "+delivery.Error)
		}
	}
	for _, runtime := range m.state.AgentRuntimes {
		if runtime.Status == "REVOKED" || runtime.Health == "DEGRADED" {
			rows = append(rows, "● "+runtime.ID+"  "+runtime.Status+" · "+runtime.Health)
		}
	}
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(p.cyan).Render("✓ CLEAR") + "\n" +
			lipgloss.NewStyle().Foreground(p.muted).Render("No intervention needed.")
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func (m Model) blockers(p palette) string {
	rows := []string{}
	for _, id := range service.SortedKeys(m.state.Tasks) {
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
// string (0-based, before centering) of paletteMatches()'s i-th entry.
func (m Model) paletteLayout(p palette) (panel string, matchLine []int) {
	width := min(68, max(36, m.width-8))
	rows := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("COMMANDS"),
		lipgloss.NewStyle().Foreground(p.muted).Render("Go to a workspace or start an action."),
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("Command"),
		lipgloss.NewStyle().Width(width-6).Background(p.panel).Foreground(p.text).
			Padding(0, 1).Render("> " + m.query + "█"),
		"",
	}
	matches := m.paletteMatches()
	if len(matches) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.amber).Render("No matching command"))
	} else {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render("Matches"))
		matchLine = make([]int, len(matches))
		for index, match := range matches {
			marker := "  "
			style := lipgloss.NewStyle().Foreground(p.text)
			if index == 0 {
				marker = "› "
				style = style.Foreground(p.cyan).Bold(true)
			}
			matchLine[index] = len(rows)
			rows = append(rows, style.Render(marker+match.label))
		}
	}
	paletteFooter := strings.Join([]string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[type]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("filter"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[enter/click]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("open"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[esc]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("close"),
	}, " · ")
	rows = append(rows, "", paletteFooter)
	panel = lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).
		BorderForeground(p.cyan).Background(p.ink).Foreground(p.text).Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	return panel, matchLine
}

func (m Model) renderPalette(p palette, under string) string {
	panel, _ := m.paletteLayout(p)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(p.ink)))
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
	apply func(Model) (tea.Model, tea.Cmd)
}

func (m Model) paletteMatches() []paletteMatch {
	query := strings.ToLower(strings.TrimSpace(m.query))
	var matches []paletteMatch
	for _, cmd := range paletteCommands() {
		if len(matches) >= 6 {
			break
		}
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
		matches = append(matches, paletteMatch{label: cmd.label, apply: func(value Model) (tea.Model, tea.Cmd) {
			value.openView(cmd.view)
			return cmd.open(value)
		}})
	}
	for _, name := range views {
		if len(matches) >= 6 {
			break
		}
		if query != "" && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		name := name
		matches = append(matches, paletteMatch{label: name, apply: func(value Model) (tea.Model, tea.Cmd) {
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
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("From: %s  ->  To: %s  |  Kind: %s  |  Status: %s", msg.From, strings.Join(msg.To, ", "), msg.Kind, fmtStatus(msg.Status))))
			lines = append(lines, titleStyle.Render("Body: ")+msg.Body)
		}
	case "Invocations":
		if inv, ok := m.state.Invocations[id]; ok {
			lines = append(lines, titleStyle.Render("Instruction: ")+inv.Instruction)
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Target: %s  |  RequestedBy: %s  |  Priority: %s  |  Status: %s", inv.Target, inv.RequestedBy, inv.Priority, fmtStatus(inv.Status))))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Consumer Mode: %s  |  Preferred Runtime: %s", empty(string(inv.ConsumerMode), "EITHER"), empty(inv.PreferredRuntimeID, "automatic"))))
			if inv.Reason != "" {
				lines = append(lines, titleStyle.Render("Reason: ")+inv.Reason)
			}
		}
	case "Agents":
		if ag, ok := m.state.Agents[id]; ok {
			lines = append(lines, titleStyle.Render("Display Name: ")+empty(ag.DisplayName, ag.ID))
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Status: %s  |  Role: %s  |  Type: %s", fmtStatus(ag.Status), string(ag.Role), string(ag.PrincipalType))))
			lines = append(lines, mutedStyle.Render("Scopes: ")+strings.Join(ag.Scopes, ", "))
			lines = append(lines, mutedStyle.Render("Capabilities: ")+strings.Join(ag.Capabilities, ", "))
			lines = append(lines, mutedStyle.Render("Fingerprint: ")+ag.KeyFingerprint)
		}
	case "Runtimes":
		if r, ok := m.state.AgentRuntimes[id]; ok {
			lines = append(lines, titleStyle.Render("Agent ID: ")+r.AgentID)
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Status: %s  |  Health: %s  |  Kind: %s  |  Connector: %s", fmtStatus(r.Status), r.Health, r.Kind, r.Connector)))
			lines = append(lines, mutedStyle.Render("Host ID: ")+r.HostID)
		}
	case "Approvals":
		if app, ok := m.state.Approvals[id]; ok {
			lines = append(lines, titleStyle.Render("Action: ")+app.Action)
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("Tier: %s  |  Status: %s  |  Requester: %s", app.Tier, fmtStatus(app.Status), app.Requester)))
			lines = append(lines, titleStyle.Render("Reason: ")+app.Reason)
			if app.ExpiresAt != nil {
				lines = append(lines, mutedStyle.Render("Expires: ")+app.ExpiresAt.Local().Format(time.RFC3339))
			}
			if app.Subject != "" {
				lines = append(lines, titleStyle.Render("Reviewed operation: ")+app.Subject)
			}
		}
	default:
		lines = append(lines, mutedStyle.Render("ID: ")+id)
	}

	return lipgloss.NewStyle().
		Width(max(20, width-2)).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.cyan).
		PaddingLeft(1).
		Render(strings.Join(lines, "\n"))
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
