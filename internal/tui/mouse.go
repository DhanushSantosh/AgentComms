package tui

import "charm.land/lipgloss/v2"

// rowListDimensions returns the exact (w, h) the currently active row-list
// view passes to RowList.View, mirroring renderBody's per-view adjustments
// (Agents/Invocations/Runtimes/Contracts & decisions each reserve extra
// lines for their own control bar or detail pane). Shared by renderBody
// itself and by syncActiveRowListDimensions, so the persisted table's real
// viewport size -- fixed outside of View(), which can't persist state for
// the next Update() to read -- can never drift from what's actually
// rendered.
func (m Model) rowListDimensions() (w, h int) {
	_, _, innerW, innerH := m.bodyLayout()
	switch views[m.view] {
	case "Agents":
		return innerW, max(5, innerH-4)
	case "Invocations":
		return innerW, max(5, innerH-10)
	case "Runtimes":
		return innerW, max(5, innerH-9)
	case "Contracts & decisions":
		return innerW, max(5, innerH-6)
	default:
		return innerW, innerH
	}
}

// syncActiveRowListDimensions writes the real width/height onto the active
// row list, using the exact same visibleRowCount formula RowList.View
// itself renders with. Without this, the persisted RowList never learns
// its actual on-screen size -- View() only computes it locally, per call,
// from whatever h it's given -- which would make scroll math (clampTopRow)
// and the click-to-row math in rowAtY below work from a stale height.
func (m *Model) syncActiveRowListDimensions() {
	list := m.activeRowList()
	if list == nil {
		return
	}
	w, h := m.rowListDimensions()
	list.SetDimensions(w, visibleRowCount(h))
}

// bodyPrefixHeight returns how many terminal lines render above
// renderBody's own content block -- the pane's top padding, the command
// rail, the hub tabs, and the section header -- for the view active right
// now. Recomputed fresh from the same model fields renderBody itself uses
// (there's no other way: View() cannot persist layout metrics for the
// Update() call that handles a click to read back). Takes no width
// parameter deliberately: meta/tabs render at contentW (renderBody's own
// inner width, w-4 for pane's Padding(1, 2)), never the outer paneW --
// passing the wrong one here was the exact bug behind an inaccurate
// settings click, so this owns computing it correctly rather than trusting
// each caller to pass the right one.
func (m Model) bodyPrefixHeight(p palette) int {
	_, _, contentW, _ := m.bodyLayout()
	meta := m.commandRail(p, contentW)
	tabs, _ := m.renderHubTabs(p, contentW)
	title := views[m.view]
	if title == "Overview" {
		title = "PROJECT CONTROL"
	}
	header := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(title)
	return 1 /* pane top padding */ + lipgloss.Height(meta) + lipgloss.Height(tabs) + 1 /* blank */ + lipgloss.Height(header) + 1 /* blank */
}

// rowTableTopY returns the absolute screen row where the active row list's
// own header row renders -- one above the first data row -- accounting
// for whichever per-view content (agentControlBar, invocationControlBar)
// renders before it. The "+1", not "+2", after each control bar's height
// is deliberate: bodyContent joins them with "\n\n", and a double newline
// between two strings that don't already end in one contributes exactly
// one blank line, not two -- getting this wrong here was the exact bug
// behind a real click landing one row below the row it should have
// selected, confirmed by rendering a real Agents screen and comparing
// byte-for-byte against this formula's prediction.
func (m Model) rowTableTopY(p palette) int {
	_, _, contentW, _ := m.bodyLayout()
	top := m.bodyPrefixHeight(p)
	switch views[m.view] {
	case "Agents":
		top += lipgloss.Height(m.agentControlBar(p, contentW)) + 1
	case "Invocations":
		top += lipgloss.Height(m.invocationControlBar(p, contentW)) + 1
	}
	return top
}

// rowAtY translates a click's absolute screen Y into an absolute row index
// in the active row list, or ok=false if the click landed outside the
// table's data rows (on its header, above/below it, or past the last
// rendered row). Exact, not an approximation: RowList tracks topRow
// itself (rowlist.go) rather than delegating scrolling to bubbles/table's
// viewport, whose internal scroll offset is neither a pure function of
// (cursor, height, rowCount) nor exposed for an outside caller to read --
// see RowList's own doc comment for why that made precise click
// resolution impossible to combine with correct cursor-visibility.
func (m Model) rowAtY(p palette, y int) (row int, ok bool) {
	list := m.activeRowList()
	if list == nil {
		return 0, false
	}
	rowCount := len(list.source.Rows(m.state, m.actor, list.mine))
	if rowCount == 0 {
		return 0, false
	}
	firstDataRow := m.rowTableTopY(p) + 1
	relative := y - firstDataRow
	if relative < 0 {
		return 0, false
	}
	visibleEnd := min(list.topRow+max(1, list.height), rowCount)
	absolute := list.topRow + relative
	if absolute < list.topRow || absolute >= visibleEnd {
		return 0, false
	}
	return absolute, true
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// formFieldAtY translates a click's absolute screen Y into an m.inputs
// index, or ok=false if the click missed every field line. Each candidate
// row is measured with the exact same MaxWidth renderForm's real Render
// applies (formMaxWidth), so a wrapped title/hint is accounted for exactly
// rather than assumed to be one line.
func (m Model) formFieldAtY(p palette, y int) (field int, ok bool) {
	if len(m.inputs) == 0 {
		return 0, false
	}
	top := m.bodyPrefixHeight(p)
	rows, fieldLine := m.formRows(p)
	maxW := m.formMaxWidth()
	measure := lipgloss.NewStyle().MaxWidth(maxW)
	line := top
	rowTop := make([]int, len(rows))
	for i, row := range rows {
		rowTop[i] = line
		line += lipgloss.Height(measure.Render(row))
	}
	for i, at := range fieldLine {
		if y == rowTop[at] {
			return i, true
		}
	}
	return 0, false
}

// confirmChoiceAt translates a click's absolute screen (x, y) into a
// yes/no choice on the active confirm dialog, or ok=false if the click
// missed both printed button labels -- deliberately narrow (see
// updateConfirm's comment on why an ambiguous click must never default to
// either choice on an action that can be irreversible).
func (m Model) confirmChoiceAt(p palette, x, y int) (yes, ok bool) {
	if m.confirm == nil {
		return false, false
	}
	rows := []string{
		lipgloss.NewStyle().Foreground(p.amber).Bold(true).Render("REVIEW / Signed change"),
		m.confirm.prompt,
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("This action becomes part of project history."),
	}
	line := m.bodyPrefixHeight(p)
	for _, row := range rows {
		line += lipgloss.Height(row)
	}
	if y != line {
		return false, false
	}
	// The button row starts after the sidebar, the " " JoinHorizontal
	// separator, the confirm block's own BorderLeft (1 column), and its
	// PaddingLeft(2).
	textStart := m.sidebarWidth() + 1 + 1 + 2
	relativeX := x - textStart
	if relativeX < 0 {
		return false, false
	}
	if relativeX < lipgloss.Width(confirmYesLabel) {
		return true, true
	}
	noStart := lipgloss.Width(confirmYesLabel) + lipgloss.Width(confirmGap)
	if relativeX >= noStart && relativeX < noStart+lipgloss.Width(confirmNoLabel) {
		return false, true
	}
	return false, false
}

// hubTabAt translates a click's absolute screen (x, y) into a view name
// from the current hub's tab bar, or ok=false if the click missed it --
// the row directly below the command rail, within one of the tab labels'
// own column ranges (renderHubTabs's tabRange, recomputed fresh here for
// the same reason sidebarHubAt and rowAtY do).
func (m Model) hubTabAt(p palette, x, y int) (view string, ok bool) {
	_, _, contentW, _ := m.bodyLayout()
	meta := m.commandRail(p, contentW)
	top := 1 + lipgloss.Height(meta) // pane's own top padding, then the command rail
	if y != top {
		return "", false
	}
	_, tabRange := m.renderHubTabs(p, contentW)
	relativeX := x - m.sidebarWidth() - 1 // sidebar + JoinHorizontal's " " separator
	hub := navigationHubs[m.activeHubIndex()]
	for i, r := range tabRange {
		if relativeX >= r[0] && relativeX < r[1] {
			return hub.Views[i], true
		}
	}
	return "", false
}

// settingsSectionAt translates a click's absolute screen (x, y) into a
// settingsSections index, or ok=false if the click missed the domain rail
// entirely -- including when the rail isn't even rendered, below
// settings.go's own 72-column threshold where projectSettings falls back
// to showing only the current domain's name, with no list to click.
//
// Measures each row's real rendered height with lipgloss.Height() rather
// than assuming "title is 1 line, every domain is exactly 2 lines
// (name + blank)" -- that assumption only held at a wide enough
// domainWidth. At domainWidth's own floor of 18 (hit by any contentW from
// 72 up to just under 96 -- an entirely ordinary window size, not some
// extreme edge case), the interior text width left after
// settingsDomainRail's Border(1)+Padding(1) is only 14 columns, too narrow
// for "Agents & access" or "Authority & data" (18 columns with their
// marker) or even "CONTROL DOMAINS" itself (15 columns) -- all three wrap
// onto a second line there, and the fixed-offset formula silently pointed
// at the wrong domain, or none, for every row after the first wrap.
// Confirmed live: at contentW=72 every domain past "Project policy"
// resolved to the wrong index or nothing at all.
func (m Model) settingsSectionAt(p palette, x, y int) (index int, ok bool) {
	_, _, contentW, _ := m.bodyLayout()
	if contentW < 72 {
		return 0, false
	}
	domainWidth := max(22, min(26, contentW/4))
	left := m.sidebarWidth() + 1
	if x < left || x >= left+domainWidth {
		return 0, false
	}
	innerW := max(1, domainWidth-4) // Border(1) + Padding(1), each side
	measure := lipgloss.NewStyle().Width(innerW)
	line := m.bodyPrefixHeight(p) + 1 /* border top */ + 1 /* padding top */
	line += lipgloss.Height(measure.Render("CONTROL DOMAINS")) + 1 /* blank */
	for i, section := range settingsSections {
		marker := "  "
		if i == m.settingsCursor {
			marker = "▌ "
		}
		rowHeight := lipgloss.Height(measure.Render(marker + section.name))
		if y >= line && y < line+rowHeight {
			return i, true
		}
		line += rowHeight + 1 // the blank line settingsDomainRail leaves after each row
	}
	return 0, false
}

// sidebarHubAt translates a click's absolute screen (x, y) into a
// navigationHubs index, or ok=false when the click missed the sidebar
// entirely or landed on a non-clickable line (blank space, the project
// title, the current-view sub-label, the help text).
func (m Model) sidebarHubAt(p palette, x, y int) (hub int, ok bool) {
	sidebarW := m.sidebarWidth()
	if x >= sidebarW {
		return 0, false
	}
	_, hubLine := m.renderSidebar(p, sidebarW, max(10, m.height))
	line := y - 1 // Padding(1)'s top line
	for i, l := range hubLine {
		if l == line {
			return i, true
		}
	}
	return 0, false
}
