package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/charmbracelet/x/ansi"
)

type FormField struct {
	Label       string
	Placeholder string
	Required    bool
	// Options, when set, turns this field into a left/right-cycling
	// single-select instead of free text -- for enum-shaped values (role,
	// kind, connector, tier, ...) where a typo used to silently produce the
	// wrong value with no feedback until the signed Execute call rejected
	// it. The field's value defaults to Options[0] and can only ever be one
	// of these strings; typed characters are ignored while it's focused.
	Options []string
	// Mask renders this field as a masked password input (bubbles/textinput's
	// EchoPassword mode) instead of plain text -- for values like an
	// elevated-key passphrase that must never echo to the screen.
	Mask bool
}
type ActionForm struct {
	Title, Hint string
	Fields      []FormField
	Build       func(values []string) (any, error)
	ResolveID   func(fixedID string, values []string) string
	ConfirmIf   func(payload any) (ok bool, prompt string)
	// Dispatch's passphrase parameter carries whatever CollectsPassphrase
	// stripped from values (empty string when CollectsPassphrase is false).
	// Present on every implementation for a single, consistent signature;
	// most ignore it.
	Dispatch func(m Model, values []string, passphrase string) (tea.Model, tea.Cmd)
	// CollectsPassphrase marks this form's last field as the actor's
	// elevated-key passphrase rather than payload data: it is stripped from
	// values before Build/ResolveID/Dispatch ever see it, and threaded
	// through Service.ExecuteWithPassphrase instead of Execute so a
	// transition that actually needs the elevated key
	// (protocol.RequiresElevatedKey) can be completed without leaving the
	// TUI for the CLI. The stripped-out value is read from the raw,
	// untrimmed input on submit, since a passphrase's whitespace is
	// significant unlike every other field's.
	CollectsPassphrase bool
}
type RowAction struct {
	Key       string
	Label     string
	EventType string
	Confirm   bool
	Form      *ActionForm
	Payload   func() any
	Prompt    func(id string) string
	OnError   func(err error, id string) error
	Dispatch  func(m Model, id string) (tea.Model, tea.Cmd)
}

func (act RowAction) prompt(id string) string {
	if act.Prompt != nil {
		return act.Prompt(id)
	}
	return "Apply " + act.Label + " to " + id + "?"
}

type RowSource interface {
	Columns(width int) []table.Column
	Rows(st model.State, actor string, mine bool) []table.Row
	RowID(idx int, st model.State, actor string, mine bool) string
	Actions(id string, st model.State, actor string) []RowAction
}
type confirmState struct {
	prompt     string
	typ        string
	id         string
	payload    any
	onError    func(err error, id string) error
	passphrase string
	// chainOrchestratorApproval marks a confirm dialog that, on "y", must
	// first file and approve a HUMAN-tier approval.request for id's
	// Orchestrator grant (protocol.OrchestratorGrantApprovalAction) before
	// attempting the confirm's own typ/id/payload -- three separate signed
	// events, exactly as protocol.ValidateTransition requires, just chained
	// into one confirmation instead of a separate trip through Approvals.
	chainOrchestratorApproval bool
}

// RowList owns its cursor and scroll position directly (cursor, topRow,
// height) rather than delegating to bubbles/table.Model's internal
// viewport. That component's MoveUp/MoveDown adjust an internal scroll
// offset (viewport.YOffset) it never exposes a getter for, and that offset
// is not a pure function of (cursor, height, rowCount) -- it depends on
// the history of prior moves. That makes it fundamentally impossible for
// an outside caller (mouse.go's rowAtY) to reconstruct which absolute row
// a screen click landed on with any reliability; a first attempt at this
// (see git history) shipped a version that either broke click accuracy or
// broke the table's own guarantee that the cursor stays visible while
// scrolling, depending on how the tradeoff was made. Owning cursor/topRow
// directly removes the tradeoff entirely: topRow is a plain field, always
// exactly known, and clampTopRow's "scroll the minimum to keep cursor
// visible" logic is the one place that guarantee is enforced, so both
// properties hold unconditionally.
type RowList struct {
	source RowSource
	mine   bool
	cursor int
	topRow int
	height int // visible data-row count, kept in sync by syncActiveRowListDimensions
	width  int
}

func newRowList(source RowSource) RowList {
	return RowList{source: source, height: 20}
}

// visibleRowCount is the one formula translating a RowList.View caller's
// requested outer height into how many data rows actually show -- shared
// between View itself and syncActiveRowListDimensions (mouse.go) so the
// persisted height used for scroll math can never drift from what's
// actually rendered.
func visibleRowCount(h int) int { return max(0, h-4) }

func (r *RowList) Refresh(st model.State, actor string) {
	r.clampToRowCount(len(r.source.Rows(st, actor, r.mine)))
}
func (r *RowList) SetMineFilter(mine bool, st model.State, actor string) {
	r.mine = mine
	r.Refresh(st, actor)
}
func (r RowList) SelectedID(st model.State, actor string) string {
	return r.source.RowID(r.cursor, st, actor, r.mine)
}
func (r RowList) Actions(id string, st model.State, actor string) []RowAction {
	return r.source.Actions(id, st, actor)
}
func (r RowList) Cursor() int { return r.cursor }

// SetCursor moves to an absolute row (clamped to the valid range for
// rowCount), scrolling topRow the minimum amount needed to keep it
// visible -- never re-centering, the same "just enough" scroll ordinary
// list widgets use.
func (r *RowList) SetCursor(n, rowCount int) {
	if rowCount <= 0 {
		r.cursor, r.topRow = 0, 0
		return
	}
	r.cursor = clamp(n, 0, rowCount-1)
	r.clampTopRow(rowCount)
}
func (r *RowList) MoveCursor(delta, rowCount int) {
	r.SetCursor(r.cursor+delta, rowCount)
}
func (r *RowList) SetDimensions(w, h int) {
	r.width = w
	r.height = max(1, h)
}
func (r *RowList) clampToRowCount(rowCount int) {
	if rowCount <= 0 {
		r.cursor, r.topRow = 0, 0
		return
	}
	if r.cursor < 0 {
		r.cursor = 0
	}
	if r.cursor >= rowCount {
		r.cursor = rowCount - 1
	}
	r.clampTopRow(rowCount)
}
func (r *RowList) clampTopRow(rowCount int) {
	height := max(1, r.height)
	if r.topRow > r.cursor {
		r.topRow = r.cursor
	}
	if r.cursor >= r.topRow+height {
		r.topRow = r.cursor - height + 1
	}
	if maxTop := max(0, rowCount-height); r.topRow > maxTop {
		r.topRow = maxTop
	}
	if r.topRow < 0 {
		r.topRow = 0
	}
}

type rowStyles struct {
	Header, Cell, Selected lipgloss.Style
}

func rowListStyles(p palette) rowStyles {
	return rowStyles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(p.muted).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Foreground(p.text).Padding(0, 1),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(p.ink).Background(p.cyan).Padding(0, 1),
	}
}

// fmtStatus adds a visual indicator emoji prefix to status strings for enhanced visual hierarchy.
func fmtStatus(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ACTIVE", "ONLINE", "APPROVED":
		return "🟢 " + s
	case "PENDING", "WAITING", "OPEN":
		return "🟡 " + s
	case "SUSPENDED", "OFFLINE", "DRAINING":
		return "🟠 " + s
	case "REVOKED", "REJECTED", "CANCELLED", "ERROR", "EXPIRED", "DISABLED", "FAILED", "EXHAUSTED", "BLOCKED":
		return "🔴 " + s
	case "CLAIMED", "RUNNING", "SUCCEEDED", "IN-FLIGHT", "AUTOMATIC", "TRUSTED", "IN_PROGRESS":
		return "⚡ " + s
	case "COMPLETED":
		return "✅ " + s
	default:
		return s
	}
}

// cellWidth returns how many columns of col.Width are left for the cell's
// own text after reserving room for the Header/Cell style's Padding(0, 1)
// (1 column each side, added when styles.Header/Cell.Render wraps the
// already-fixed-width string below). Every RowSource.Columns(width) sizes
// its columns to sum to exactly `width` -- table.Column.Width has always
// meant "total rendered width, padding included" here, not "width of the
// text alone" -- so without this the four columns' Header/Cell padding
// (2 columns apiece) made every rendered row 2*len(cols) columns wider than
// its own RowSource believed it was, silently overflowing the pane's own
// Width() and wrapping the tail of the row (typically the last column's
// text) onto a spurious extra physical line under every single row, at
// every terminal size -- confirmed live on the Inbox and Runtimes views,
// and by rendering real screens at width 60 through 160 in
// TestRowCellsNeverWrapOntoAnExtraLine.
func cellWidth(colWidth int) int { return max(1, colWidth-2) }

// clampColumnsToWidth shrinks cols, right column first, so their Width sum
// never exceeds w. Every RowSource.Columns(width) computes its flexible
// column as `width - (the fixed ones)`, then floors it at some readable
// minimum (messageRowSource's subj, e.g., never goes below 15) -- and at a
// narrow enough terminal, that floor alone pushes the total over budget,
// independent of the padding-double-count this shares a bug class with
// (see cellWidth's comment): the row still overflows the pane's own
// Width() and wraps, corrupting the display and shifting every rowAtY
// click below it by however many extra lines it wrapped onto. Shrinking
// down to 3 columns (enough to show "…") before dropping a column
// entirely (Width <= 0, which renderHeader/renderTableRow already treat
// as "omit") guarantees the invariant every caller of RowList.View
// depends on: the rendered row is never wider than the width it was asked
// to fit in, at any terminal size.
func clampColumnsToWidth(cols []table.Column, w int) []table.Column {
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	over := total - w
	if over <= 0 {
		return cols
	}
	out := make([]table.Column, len(cols))
	copy(out, cols)
	for i := len(out) - 1; i >= 0 && over > 0; i-- {
		room := out[i].Width - 3
		if room <= 0 {
			continue
		}
		take := min(room, over)
		out[i].Width -= take
		over -= take
	}
	// Still over budget even at every column's floor of 3: drop columns
	// entirely from the right until it fits.
	for i := len(out) - 1; i >= 0 && over > 0; i-- {
		if out[i].Width <= 0 {
			continue
		}
		over -= out[i].Width
		out[i].Width = 0
	}
	return out
}

// renderHeader and renderTableRow replicate bubbles/table's own
// headersView/renderRow cell layout exactly (per-column width, ansi-safe
// truncation, whole-row Selected styling) so dropping table.Model changes
// nothing about what's on screen -- only how scroll position is tracked.
func renderHeader(cols []table.Column, styles rowStyles) string {
	cells := make([]string, 0, len(cols))
	for _, col := range cols {
		if col.Width <= 0 {
			continue
		}
		w := cellWidth(col.Width)
		cell := lipgloss.NewStyle().Width(w).MaxWidth(w).Inline(true).Render(ansi.Truncate(col.Title, w, "…"))
		cells = append(cells, styles.Header.Render(cell))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}
func renderTableRow(cols []table.Column, values table.Row, styles rowStyles, selected bool) string {
	cells := make([]string, 0, len(cols))
	cellStyle := styles.Cell
	if selected {
		cellStyle = styles.Selected
	}
	for i, col := range cols {
		if col.Width <= 0 {
			continue
		}
		var text string
		if i < len(values) {
			text = values[i]
		}
		w := cellWidth(col.Width)
		cell := lipgloss.NewStyle().Width(w).MaxWidth(w).Inline(true).Render(ansi.Truncate(text, w, "…"))
		cells = append(cells, cellStyle.Render(cell))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
}
func (r RowList) View(p palette, st model.State, actor string, w, h int) string {
	cols := clampColumnsToWidth(r.source.Columns(w), w)
	rows := r.source.Rows(st, actor, r.mine)
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No rows here yet.")
	}
	styles := rowListStyles(p)
	top := min(r.topRow, len(rows)-1)
	if top < 0 {
		top = 0
	}
	end := min(top+visibleRowCount(h), len(rows))
	lines := make([]string, 0, end-top+1)
	lines = append(lines, renderHeader(cols, styles))
	for i := top; i < end; i++ {
		lines = append(lines, renderTableRow(cols, rows[i], styles, i == r.cursor))
	}
	id := r.source.RowID(r.cursor, st, actor, r.mine)
	navParts := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[↑/↓]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("select"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[i]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("inspect"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[esc]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("back"),
	}
	parts := []string{strings.Join(navParts, " · ")}
	actions := r.source.Actions(id, st, actor)
	hiddenCount := 0
	for _, act := range actions {
		hint := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("["+act.Key+"]") +
			" " + lipgloss.NewStyle().Foreground(p.muted).Render(act.Label)
		candidate := strings.Join(append(parts, hint), " · ")
		if lipgloss.Width(candidate) <= w {
			parts = append(parts, hint)
		} else {
			hiddenCount++
		}
	}
	if hiddenCount > 0 {
		moreHint := lipgloss.NewStyle().Foreground(p.muted).Render(fmt.Sprintf("+%d more", hiddenCount))
		candidate := strings.Join(append(parts, moreHint), " · ")
		if lipgloss.Width(candidate) <= w {
			parts = append(parts, moreHint)
		}
	}
	// ansi.Truncate, not a bare join: parts always includes the base
	// "[↑/↓] select · [i] inspect · [esc] back" trio unconditionally --
	// only the per-action hints after it are already width-aware -- so at
	// a narrow enough w that trio alone still overflowed w and wrapped
	// onto extra physical lines lipgloss's default (non-Inline) wrapping
	// added silently. RowList.View's own line-count contract (1 header +
	// visibleRowCount(h) rows + this footer) assumes exactly one line
	// here; wrapping broke it the same way an unclamped data cell once did.
	footer := ansi.Truncate(strings.Join(parts, " · "), w, "…")
	return strings.Join(lines, "\n") + "\n\n" + footer
}

func (m *Model) activeRowList() *RowList {
	switch views[m.view] {
	case "Tasks", "My work":
		return &m.taskList
	case "Inbox":
		return &m.messageList
	case "Approvals":
		return &m.approvalList
	case "Agents":
		return &m.agentList
	case "Invocations":
		return &m.invocationList
	case "Runtimes":
		return &m.runtimeList
	case "Documents":
		return &m.documentList
	case "Contracts & decisions":
		return &m.decisionList
	case "Artifacts":
		return &m.artifactList
	case "Environment":
		return &m.envList
	}
	return nil
}
func (m Model) openCreateForm() (tea.Model, tea.Cmd) {
	switch views[m.view] {
	case "Tasks", "My work":
		return m.openTaskForm()
	case "Inbox":
		return m.openActionForm(messagePostForm, "message.post", "")
	case "Approvals":
		return m.openActionForm(approvalRequestForm, "approval.request", "")
	case "Agents":
		return m.openActionForm(agentRegisterForm, "agent.register", "")
	case "Invocations":
		return m.openActionForm(invocationRequestForm, "invocation.request", "")
	case "Runtimes":
		return m.openActionForm(runtimeRegisterForm, "runtime.register", "")
	case "Documents":
		return m.openActionForm(documentCreateForm, "document.create", "")
	case "Contracts & decisions":
		return m.openActionForm(decisionCreateForm, "document.create", "")
	case "Artifacts":
		return m.openActionForm(artifactAddForm, "artifact.add", "")
	case "Drafts":
		return m.openActionForm(draftSaveForm, "draft.save", "")
	case "Environment":
		return m.openActionForm(envSetForm, "env.set", "")
	}
	return m, nil
}

func (m Model) updateRowList(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncActiveRowListDimensions()
	list := m.activeRowList()
	if list == nil {
		m.rowFocus = false
		return m, nil
	}
	rowCount := len(list.source.Rows(m.state, m.actor, list.mine))
	// Mouse wheel scrolls the row cursor the same one row at a time that
	// LineUp/LineDown (k/j, up/down) already do.
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		switch wheel.Button {
		case tea.MouseWheelUp:
			list.MoveCursor(-1, rowCount)
		case tea.MouseWheelDown:
			list.MoveCursor(1, rowCount)
		}
		return m, nil
	}
	if click, ok := msg.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		if mouse.Button == tea.MouseLeft {
			p := colors(m.highContrast)
			if row, ok := m.rowAtY(p, mouse.Y); ok {
				// Recorded regardless of whether this turns out to be a
				// double-click, so a third click starts a fresh window
				// rather than chaining into more double-clicks.
				double := m.isDoubleClick(mouse.X, mouse.Y, time.Now())
				list.SetCursor(row, rowCount)
				if double {
					// [i] inspect, not actions[0]: firing the row's first
					// action -- activate for a PENDING agent, drain for an
					// ONLINE runtime, and so on -- used to be the double-
					// click behavior, on the reasoning that a Confirm prompt
					// or form always intervenes before anything irreversible
					// actually signs, so it was never unsafe. True, but not
					// the complaint: "double-click on an ONLINE runtime
					// starts draining it" is surprising regardless of
					// whether a prompt catches it, precisely because
					// double-click has no single, universal meaning across
					// row types the way it does for opening a file. [i]
					// does have one -- show more detail about the selected
					// row -- is always safe (a pure view toggle, no
					// governed transition involved at all), and is what a
					// user reaching for "tell me more" by double-clicking
					// actually expects.
					m.inspecting = true
					return m, nil
				}
			}
		}
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	// esc/left/q/ctrl+c/​/​/ctrl+p/r/i/?/h/n below are reserved globally --
	// matched here, in this explicit switch, before the default case ever
	// gets a chance to check a row's own Actions() for a matching key. No
	// RowAction anywhere in the app may use any of these as its own Key;
	// one already did ("r" for agent.go's actChangeRole) and was silently
	// unreachable, always losing to refresh, until this comment existed.
	switch k := key.String(); k {
	case "esc", "left":
		m.rowFocus = false
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/", "ctrl+p":
		m.palette = true
		return m, nil
	case "r":
		m.refresh()
		return m, nil
	case "i":
		m.inspecting = !m.inspecting
		return m, nil
	case "?":
		m.notice = "↑/↓ select row · [key] contextual action · [i] inspect · [n] new · / commands · [esc] back · [q] quit"
		return m, nil
	case "h":
		m.highContrast = !m.highContrast
		return m, nil
	case "n":
		return m.openCreateForm()
	default:
		id := list.SelectedID(m.state, m.actor)
		if id != "" {
			for _, act := range list.Actions(id, m.state, m.actor) {
				if act.Key == k {
					return m.triggerRowAction(act, id)
				}
			}
		}
		// Falls through to navigation only once no per-row action claimed
		// the key (matching the priority row actions already had before
		// this took over cursor movement from table.Model.Update).
		height := max(1, list.height)
		switch k {
		case "up", "k":
			list.MoveCursor(-1, rowCount)
		case "down", "j":
			list.MoveCursor(1, rowCount)
		case "b", "pgup":
			list.MoveCursor(-height, rowCount)
		case "f", "pgdown", " ":
			list.MoveCursor(height, rowCount)
		case "u", "ctrl+u":
			list.MoveCursor(-height/2, rowCount)
		case "d", "ctrl+d":
			list.MoveCursor(height/2, rowCount)
		case "home", "g":
			list.SetCursor(0, rowCount)
		case "end", "G":
			list.SetCursor(rowCount-1, rowCount)
		}
	}
	return m, nil
}
func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if click, ok := msg.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		if mouse.Button != tea.MouseLeft {
			return m, nil
		}
		// Deliberately conservative: a click that doesn't land precisely on
		// one of the two printed labels does nothing at all -- unlike row
		// selection or form focus, a wrong guess here could sign an
		// irreversible action (revoke, delete, an elevated-key-gated
		// grant), so there is no default direction an ambiguous click ever
		// resolves to.
		if yes, ok := m.confirmChoiceAt(colors(m.highContrast), mouse.X, mouse.Y); ok {
			return m.resolveConfirm(yes)
		}
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		return m.resolveConfirm(true)
	case "n", "N", "esc":
		return m.resolveConfirm(false)
	}
	return m, nil
}
func (m Model) resolveConfirm(yes bool) (tea.Model, tea.Cmd) {
	c := *m.confirm
	m.confirm = nil
	if !yes {
		m.notice = "Cancelled."
		return m, nil
	}
	if c.chainOrchestratorApproval {
		return m.dispatchOrchestratorApprovalChain(c)
	}
	next, cmd := m.dispatchEventWithPassphrase(c.typ, c.id, c.payload, c.passphrase)
	mm := next.(Model)
	if mm.err != nil && c.onError != nil {
		mm.err = c.onError(mm.err, c.id)
	}
	return mm, cmd
}

// confirmYesLabel/confirmNoLabel are shared between renderConfirm's actual
// button line and confirmChoiceAt's (mouse.go) click hit-testing, so the
// clickable regions can never drift from what's actually printed on screen.
const (
	confirmYesLabel = "[y / enter] Sign and apply"
	confirmNoLabel  = "[n / esc] Go back"
	confirmGap      = "    "
)

func (m Model) renderConfirm(p palette) string {
	rows := []string{
		lipgloss.NewStyle().Foreground(p.amber).Bold(true).Render("REVIEW / Signed change"),
		m.confirm.prompt,
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("This action becomes part of project history."),
		lipgloss.NewStyle().Foreground(p.amber).Render(confirmYesLabel + confirmGap + confirmNoLabel),
	}
	return lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.amber).PaddingLeft(2).Render(strings.Join(rows, "\n"))
}
func (m Model) dispatchEvent(typ, id string, payload any) (tea.Model, tea.Cmd) {
	return m.dispatchEventWithPassphrase(typ, id, payload, "")
}
func (m Model) dispatchEventWithPassphrase(typ, id string, payload any, passphrase string) (tea.Model, tea.Cmd) {
	_, err := m.svc.ExecuteWithPassphrase(m.actor, typ, id, payload, passphrase)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.notice = typ + " applied to " + id
	m.refreshState()
	return m, nil
}

// hasApprovedOrchestratorGrant mirrors internal/protocol/transitions.go's
// unexported hasOrchestratorGrantApproval for the one action
// agent.activate cares about: an APPROVED, HUMAN-tier approval.request for
// id's Orchestrator grant, at the exact conventional ID (see RFC 0023 --
// this must match that function's ID-scoped lookup exactly, not scan by
// action string, or the TUI's own "does an approval already exist" UI
// decision can disagree with what the server will actually accept).
// Client-side only -- it decides whether the TUI needs to offer the
// chained request+approve confirm below; the server re-checks the real
// thing regardless.
func hasApprovedOrchestratorGrant(st model.State, id string) bool {
	approval, exists := st.Approvals[protocol.OrchestratorGrantApprovalID(id)]
	return exists && approval.Tier == "HUMAN" && approval.Status == "APPROVED" &&
		approval.Action == protocol.OrchestratorGrantApprovalAction(id)
}

// dispatchOrchestratorApprovalChain runs the two steps
// protocol.ValidateTransition requires before c.payload's Orchestrator
// grant can succeed -- approval.request then approval.approve, both for
// c.id's grant -- immediately followed by the grant itself, as three
// separate signed events sharing the one passphrase already collected in
// the activate form. A human still explicitly confirmed this at the prior
// prompt; this only saves the separate trip through Approvals to create
// and approve the record by hand.
func (m Model) dispatchOrchestratorApprovalChain(c confirmState) (tea.Model, tea.Cmd) {
	approvalID := protocol.OrchestratorGrantApprovalID(c.id)
	action := protocol.OrchestratorGrantApprovalAction(c.id)
	if _, err := m.svc.Execute(m.actor, "approval.request", approvalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: action, Reason: "Orchestrator grant for " + c.id,
	}); err != nil {
		m.err = err
		return m, nil
	}
	if _, err := m.svc.ExecuteWithPassphrase(m.actor, "approval.approve", approvalID, model.ApprovalResponse{}, c.passphrase); err != nil {
		m.err = err
		return m, nil
	}
	if _, err := m.svc.ExecuteWithPassphrase(m.actor, c.typ, c.id, c.payload, c.passphrase); err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.notice = "Requested, approved, and granted Orchestrator to " + c.id
	m.refreshState()
	return m, nil
}

// triggerRowAction runs act against id exactly as pressing its own key
// would: a Confirm action opens its prompt, everything else goes through
// dispatchRowAction. Shared by the keyboard action lookup above and
// double-click's "open this row's primary action" so the two can never
// diverge in what a given action actually does.
func (m Model) triggerRowAction(act RowAction, id string) (tea.Model, tea.Cmd) {
	if act.Confirm {
		var payload any
		if act.Payload != nil {
			payload = act.Payload()
		}
		m.confirm = &confirmState{prompt: act.prompt(id), typ: act.EventType, id: id, payload: payload, onError: act.OnError}
		return m, nil
	}
	return m.dispatchRowAction(act, id)
}
func (m Model) dispatchRowAction(act RowAction, id string) (tea.Model, tea.Cmd) {
	if act.Form != nil {
		return m.openActionForm(act.Form, act.EventType, id)
	}
	if act.Dispatch != nil {
		return act.Dispatch(m, id)
	}
	var payload any
	if act.Payload != nil {
		payload = act.Payload()
	}
	next, cmd := m.dispatchEvent(act.EventType, id, payload)
	mm := next.(Model)
	if mm.err != nil && act.OnError != nil {
		mm.err = act.OnError(mm.err, id)
	}
	return mm, cmd
}
func (m Model) openActionForm(spec *ActionForm, typ, id string) (tea.Model, tea.Cmd) {
	m.inputs = make([]textinput.Model, len(spec.Fields))
	for i, f := range spec.Fields {
		input := textinput.New()
		input.Prompt = f.Label + ": "
		input.CharLimit = 1200
		if f.Mask {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '•'
		}
		if len(f.Options) > 0 {
			input.SetValue(f.Options[0])
		} else {
			input.Placeholder = f.Placeholder
		}
		m.inputs[i] = input
	}
	var cmd tea.Cmd
	if len(m.inputs) > 0 {
		cmd = m.inputs[0].Focus()
	}
	m.form, m.formTaskID, m.formSpec, m.formFocus, m.palette, m.query = typ, id, spec, 0, false, ""
	return m, cmd
}
