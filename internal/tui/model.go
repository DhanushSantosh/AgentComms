package tui

import (
	"fmt"
	"image/color"
	"io"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/fsnotify/fsnotify"
)

var views = []string{"Overview", "My work", "Tasks", "Inbox", "Agents", "Approvals", "Documents", "Contracts & decisions", "Blockers", "Integrity & sync", "Activity", "Archive search"}

type Model struct {
	svc           *service.Service
	state         model.State
	actor         string
	width, height int
	view, cursor  int
	palette       bool
	query, notice string
	err           error
	highContrast  bool
	form          string
	inputs        []textinput.Model
	formFocus     int
	formTaskID    string
	formSpec      *ActionForm
	rowFocus      bool
	taskList      RowList
	messageList   RowList
	approvalList  RowList
	agentList     RowList
	confirm       *confirmState
	watcher       *fsnotify.Watcher
}

func New(s *service.Service, actor string) (Model, error) {
	st, e := s.State()
	hc := false
	if uc, err := identity.LoadUserConfig(); err == nil && uc.Theme == "high-contrast" {
		hc = true
	}
	return Model{svc: s, state: st, actor: actor, width: 100, height: 30, highContrast: hc, taskList: newRowList(taskRowSource{}), messageList: newRowList(messageRowSource{}), approvalList: newRowList(approvalRowSource{}), agentList: newRowList(agentRowSource{})}, e
}
func (m Model) Init() tea.Cmd {
	if m.watcher != nil {
		return tea.Batch(tea.RequestBackgroundColor, watchEventsCmd(m.watcher))
	}
	return tea.RequestBackgroundColor
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(fsEventMsg); ok {
		m.refreshSilent()
		if m.watcher != nil {
			return m, watchEventsCmd(m.watcher)
		}
		return m, nil
	}
	if m.form != "" {
		return m.updateForm(msg)
	}
	if m.confirm != nil {
		return m.updateConfirm(msg)
	}
	if m.rowFocus {
		return m.updateRowList(msg)
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
	case tea.KeyPressMsg:
		k := v.String()
		if m.palette {
			switch k {
			case "esc":
				m.palette = false
				m.query = ""
			case "enter":
				m.applyPalette()
			case "backspace":
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				}
			default:
				if len(k) == 1 {
					m.query += k
				}
			}
			return m, nil
		}
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.view = m.cursor
		case "down", "j":
			if m.cursor < len(views)-1 {
				m.cursor++
			}
			m.view = m.cursor
		case "left":
			m.view = m.cursor
		case "right":
			m.view = m.cursor
			fallthrough
		case "enter":
			m.view = m.cursor
			switch views[m.view] {
			case "Tasks", "My work":
				m.rowFocus = true
				m.taskList.SetMineFilter(views[m.view] == "My work", m.state, m.actor)
			case "Inbox":
				m.rowFocus = true
				m.messageList.Refresh(m.state, m.actor)
			case "Approvals":
				m.rowFocus = true
				m.approvalList.Refresh(m.state, m.actor)
			case "Agents":
				m.rowFocus = true
				m.agentList.Refresh(m.state, m.actor)
			}
		case "/", "ctrl+p":
			m.palette = true
		case "r":
			m.refresh()
		case "?":
			m.notice = "↑/↓ navigate · → open · ← back · / commands · a switch actor · r refresh · q quit"
		case "h":
			m.highContrast = !m.highContrast
			theme := "auto"
			if m.highContrast {
				theme = "high-contrast"
			}
			if uc, err := identity.LoadUserConfig(); err == nil {
				uc.Theme = theme
				_ = identity.SaveUserConfig(uc)
			}
		case "n":
			return m.openCreateForm()
		case "a":
			return m.openActorSwitchForm()
		}
	}
	return m, nil
}
func (m *Model) refresh() {
	m.state, m.err = m.svc.State()
	if m.err == nil {
		m.notice = "State refreshed at " + time.Now().Format("15:04:05")
		m.refreshLists()
	}
}

// refreshSilent re-reads state without disturbing the current notice/error,
// used by the background file-watch tick so it never stomps a just-shown
// action result. Read errors are swallowed; the last-known-good state stays
// displayed until the next successful read.
func (m *Model) refreshSilent() {
	st, err := m.svc.State()
	if err != nil {
		return
	}
	m.state = st
	m.refreshLists()
}
func (m *Model) refreshLists() {
	m.taskList.Refresh(m.state, m.actor)
	m.messageList.Refresh(m.state, m.actor)
	m.approvalList.Refresh(m.state, m.actor)
	m.agentList.Refresh(m.state, m.actor)
}
func (m *Model) applyPalette() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	if q == "new task" || q == "create task" {
		next, _ := m.openTaskForm()
		*m = next.(Model)
		return
	}
	for i, v := range views {
		if strings.Contains(strings.ToLower(v), q) {
			m.view = i
			m.cursor = i
			m.notice = "Opened " + v
			break
		}
	}
	m.palette = false
	m.query = ""
}

func (m Model) openTaskForm() (tea.Model, tea.Cmd) {
	return m.openActionForm(taskCreateForm, "task.create", "")
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
			return m, nil
		case "tab", "down":
			m.inputs[m.formFocus].Blur()
			m.formFocus = (m.formFocus + 1) % len(m.inputs)
			return m, m.inputs[m.formFocus].Focus()
		case "shift+tab", "up":
			m.inputs[m.formFocus].Blur()
			m.formFocus = (m.formFocus - 1 + len(m.inputs)) % len(m.inputs)
			return m, m.inputs[m.formFocus].Focus()
		case "enter":
			if m.formFocus < len(m.inputs)-1 {
				m.inputs[m.formFocus].Blur()
				m.formFocus++
				return m, m.inputs[m.formFocus].Focus()
			}
			values := make([]string, len(m.inputs))
			for i := range m.inputs {
				values[i] = strings.TrimSpace(m.inputs[i].Value())
			}
			for i, f := range m.formSpec.Fields {
				if f.Required && values[i] == "" {
					m.notice = "Complete every required field."
					return m, nil
				}
			}
			if m.formSpec.Dispatch != nil {
				return m.formSpec.Dispatch(m, values)
			}
			payload, err := m.formSpec.Build(values)
			if err != nil {
				m.err = err
				return m, nil
			}
			id := m.formTaskID
			if m.formSpec.ResolveID != nil {
				id = m.formSpec.ResolveID(m.formTaskID, values)
			}
			typ := m.form
			if m.formSpec.ConfirmIf != nil {
				if ok, prompt := m.formSpec.ConfirmIf(payload); ok {
					m.form, m.inputs, m.formSpec = "", nil, nil
					m.confirm = &confirmState{prompt: prompt, typ: typ, id: id, payload: payload}
					return m, nil
				}
			}
			_, err = m.svc.Execute(m.actor, typ, id, payload)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
			m.notice = "Applied " + typ + " to " + id
			m.refresh()
			return m, nil
		}
	}
	commands := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		m.inputs[i], commands[i] = m.inputs[i].Update(msg)
	}
	return m, tea.Batch(commands...)
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

type palette struct{ ink, panel, cyan, amber, red, violet, muted, text color.Color }

func colors(high bool) palette {
	if high {
		return palette{lipgloss.Color("#000000"), lipgloss.Color("#111111"), lipgloss.Color("#00FFFF"), lipgloss.Color("#FFFF00"), lipgloss.Color("#FF4444"), lipgloss.Color("#DD88FF"), lipgloss.Color("#BBBBBB"), lipgloss.Color("#FFFFFF")}
	}
	return palette{lipgloss.Color("#071019"), lipgloss.Color("#0E1C27"), lipgloss.Color("#39D7E7"), lipgloss.Color("#F0B95B"), lipgloss.Color("#F46A6A"), lipgloss.Color("#B69CFF"), lipgloss.Color("#78909C"), lipgloss.Color("#E8F1F5")}
}
func (m Model) View() tea.View {
	p := colors(m.highContrast)
	sidebarW := m.sidebarWidth()
	contentW := max(30, m.width-sidebarW-3)
	availH := max(10, m.height)
	side := m.renderSidebar(p, sidebarW, availH)
	body := m.renderBody(p, contentW, availH)
	screen := lipgloss.JoinHorizontal(lipgloss.Top, side, " ", body)
	screen = lipgloss.NewStyle().MaxWidth(m.width).Render(screen)
	if m.palette {
		screen = m.renderPalette(p, screen)
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Agent Comms · Signal room"
	return v
}
func (m Model) sidebarWidth() int {
	titleW := lipgloss.Width("◉ AGENT COMMS")
	maxNameW := titleW
	for _, name := range views {
		w := lipgloss.Width(name)
		b := m.badge(name)
		if b != "" {
			w += lipgloss.Width(b) + 1
		}
		maxNameW = max(maxNameW, w)
	}
	ideal := maxNameW + 5
	maxAllow := m.width / 3
	if m.width < 60 {
		maxAllow = m.width / 4
	}
	if ideal > maxAllow {
		ideal = maxAllow
	}
	return max(16, ideal)
}
func (m Model) renderSidebar(p palette, w, h int) string {
	title := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("◉ AGENT COMMS")
	sub := lipgloss.NewStyle().Foreground(p.muted).Render("SIGNAL ROOM")
	rows := []string{title, sub, ""}
	for i, name := range views {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.muted)
		if i == m.cursor {
			marker = "› "
			style = style.Foreground(p.cyan).Bold(true)
		}
		badge := m.badge(name)
		maxLabelW := w - 6
		if badge != "" {
			maxLabelW -= lipgloss.Width(badge) + 1
		}
		label := name
		if lipgloss.Width(label) > maxLabelW {
			label = label[:max(0, min(len(label), maxLabelW-1))] + "…"
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%s %s", marker, label, badge)))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.muted).Render("/ commands   ? help"))
	return lipgloss.NewStyle().Width(w).Height(h).Padding(1).Background(p.ink).Foreground(p.text).Render(strings.Join(rows, "\n"))
}
func (m Model) badge(name string) string {
	n := 0
	switch name {
	case "Tasks":
		n = len(m.state.Tasks)
	case "Inbox":
		for _, x := range m.state.Messages {
			if x.Status == "OPEN" {
				n++
			}
		}
	case "Agents":
		n = len(m.state.Agents)
	case "Approvals":
		for _, x := range m.state.Approvals {
			if x.Status == "PENDING" {
				n++
			}
		}
	case "Blockers":
		for _, x := range m.state.Tasks {
			if x.Status == "BLOCKED" {
				n++
			}
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}
func (m Model) renderBody(p palette, w, h int) string {
	header := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(views[m.view])
	meta := lipgloss.NewStyle().Foreground(p.muted).Render(fmt.Sprintf("profile %s · %d events · %s", m.actor, m.state.Integrity.EventCount, m.state.Integrity.SyncState))
	pane := lipgloss.NewStyle().Width(w).Height(h).Padding(1, 2).Background(p.panel).Foreground(p.text)
	if m.form != "" {
		content := m.renderForm(p)
		return pane.Render(header + "\n" + meta + "\n\n" + content)
	}
	if m.confirm != nil {
		content := m.renderConfirm(p)
		return pane.Render(header + "\n" + meta + "\n\n" + content)
	}
	contentW := max(30, w-4)
	contentH := max(6, h-8)
	wrap := lipgloss.NewStyle().MaxWidth(contentW)
	content := ""
	bodyContent := ""
	switch views[m.view] {
	case "Overview":
		bodyContent = wrap.Render(m.overview(p))
	case "My work", "Tasks":
		bodyContent = m.taskList.View(p, m.state, m.actor, contentW, contentH)
	case "Inbox":
		bodyContent = m.messageList.View(p, m.state, m.actor, contentW, contentH)
	case "Agents":
		bodyContent = m.agentList.View(p, m.state, m.actor, contentW, contentH)
	case "Approvals":
		bodyContent = m.approvalList.View(p, m.state, m.actor, contentW, contentH)
	case "Documents":
		bodyContent = wrap.Render(m.documents(p))
	case "Contracts & decisions":
		bodyContent = wrap.Render(m.decisions(p))
	case "Blockers":
		bodyContent = wrap.Render(m.blockers(p))
	case "Integrity & sync":
		bodyContent = wrap.Render(m.integrity(p))
	case "Activity":
		bodyContent = wrap.Render(m.chain(p))
	case "Archive search":
		bodyContent = wrap.Render(m.archive(p))
	}
	content = bodyContent
	if m.err != nil {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.red).MaxWidth(contentW).Render("Error: "+m.err.Error())
	} else if m.notice != "" {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.cyan).MaxWidth(contentW).Render(m.notice)
	}
	return pane.Render(header + "\n" + meta + "\n\n" + content)
}
func (m Model) renderForm(p palette) string {
	title, hint := "Form", ""
	if m.formSpec != nil {
		title, hint = m.formSpec.Title, m.formSpec.Hint
	}
	rows := []string{lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render(title), lipgloss.NewStyle().Foreground(p.muted).Render(hint), ""}
	for i, input := range m.inputs {
		line := "  " + input.View()
		if i == m.formFocus {
			line = lipgloss.NewStyle().Foreground(p.cyan).Render("> ") + input.View()
		}
		rows = append(rows, line)
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.muted).Render("enter next/save | tab move | esc cancel"))
	if m.notice != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.amber).Render(m.notice))
	}
	if m.err != nil {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.red).Render(m.err.Error()))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.cyan).Padding(1, 2).Render(strings.Join(rows, "\n"))
}
func box(p palette, title, value, detail string) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.muted).Width(21).Padding(0, 1).Render(lipgloss.NewStyle().Foreground(p.muted).Render(title) + "\n" + lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render(value) + "\n" + detail)
}
func (m Model) overview(p palette) string {
	open, blocked, pending := 0, 0, 0
	for _, t := range m.state.Tasks {
		if !t.Archived && t.Status != "COMPLETED" && t.Status != "CANCELLED" {
			open++
		}
		if t.Status == "BLOCKED" {
			blocked++
		}
	}
	for _, a := range m.state.Approvals {
		if a.Status == "PENDING" {
			pending++
		}
	}
	cards := lipgloss.JoinHorizontal(lipgloss.Top, box(p, "ACTIVE WORK", fmt.Sprint(open), "tasks in motion"), " ", box(p, "NEEDS ATTENTION", fmt.Sprint(blocked+pending), "blocks + approvals"), " ", box(p, "INTEGRITY", map[bool]string{true: "VERIFIED", false: "FAILED"}[m.state.Integrity.Verified], fmt.Sprintf("%d signed events", m.state.Integrity.EventCount)))
	return cards + "\n\n" + lipgloss.NewStyle().Foreground(p.amber).Bold(true).Render("Attention queue") + "\n" + m.attention(p) + "\n\n" + lipgloss.NewStyle().Foreground(p.violet).Bold(true).Render("Event chain") + "\n" + m.chain(p)
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
	if len(rows) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No urgent coordination items. Open a task or review project activity.")
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}
func (m Model) documents(p palette) string {
	rows := []string{"STATUS    VERSION  DOCUMENT             AUTHOR        TAGS"}
	for _, id := range service.SortedKeys(m.state.Documents) {
		d := m.state.Documents[id]
		rows = append(rows, fmt.Sprintf("%-9s %-7d %-20s %-13s %s", d.Status, d.Version, id, d.Author, strings.Join(d.Tags, ",")))
	}
	if len(rows) == 1 {
		return "No living documents yet."
	}
	return strings.Join(rows, "\n")
}
func (m Model) decisions(p palette) string {
	rows := []string{}
	for _, id := range service.SortedKeys(m.state.Decisions) {
		d := m.state.Decisions[id]
		rows = append(rows, fmt.Sprintf("◆ %s  %s\n  %s", id, d.Title, d.Statement))
	}
	for _, id := range service.SortedKeys(m.state.Messages) {
		x := m.state.Messages[id]
		if x.Kind == "CONTRACT" {
			rows = append(rows, fmt.Sprintf("◇ %s  %s · %s", id, x.Subject, x.Status))
		}
	}
	if len(rows) == 0 {
		return "No contracts or decisions recorded."
	}
	return strings.Join(rows, "\n\n")
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
	return fmt.Sprintf("%s Chain verified: %t\n  Signed events: %d\n  Head commit: %s\n  Checkpoint: %s\n  Remote: %s\n\nRun `agent-comms verify` before recovery or migration.", mark, m.state.Integrity.Verified, m.state.Integrity.EventCount, m.state.Integrity.Head, m.state.Integrity.SyncState, empty(m.state.Integrity.Remote, "not configured"))
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
	return fmt.Sprintf("%d archived tasks remain in immutable history.\n\nUse `agent-comms search <query>` for full-text event search or `agent-comms export markdown` for a review packet.", n)
}
func (m Model) renderPalette(p palette, under string) string {
	panel := lipgloss.NewStyle().Width(min(62, m.width-8)).Border(lipgloss.DoubleBorder()).BorderForeground(p.cyan).Background(p.ink).Foreground(p.text).Padding(1, 2).Render("COMMAND PALETTE\n\n> " + m.query + "█\n\nType a view name or `new task` · enter run · esc close")
	return under + "\n" + panel
}
func empty(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
func Run(s *service.Service, actor string, in io.Reader, out io.Writer) error {
	m, e := New(s, actor)
	if e != nil {
		return e
	}
	m.EnableFileWatch()
	if m.watcher != nil {
		defer m.watcher.Close()
	}
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
	_, e = p.Run()
	return e
}
func RenderForTest(s *service.Service, actor string, w, h int) (string, error) {
	m, e := New(s, actor)
	if e != nil {
		return "", e
	}
	m.width = w
	m.height = h
	return m.View().Content, nil
}
