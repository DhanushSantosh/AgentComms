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

var views = []string{
	"Overview", "My work", "Tasks", "Inbox", "Agents", "Approvals", "Invocations",
	"Runtimes", "Project settings", "Documents", "Contracts & decisions",
	"Blockers", "Audit & health", "Activity", "Archive search",
}

type navigationHub struct {
	Name  string
	Views []string
}

var navigationHubs = []navigationHub{
	{Name: "Command", Views: []string{"Overview", "My work", "Blockers", "Approvals"}},
	{Name: "Work", Views: []string{"Tasks", "Documents", "Contracts & decisions", "Archive search"}},
	{Name: "Team", Views: []string{"Agents", "Runtimes"}},
	{Name: "Relay", Views: []string{"Inbox", "Invocations", "Activity"}},
	{Name: "Project", Views: []string{"Project settings", "Audit & health"}},
}

type Model struct {
	svc            *service.Service
	state          model.State
	actor          string
	projectID      string
	width, height  int
	view, cursor   int
	palette        bool
	query, notice  string
	err            error
	highContrast   bool
	form           string
	inputs         []textinput.Model
	formFocus      int
	formTaskID     string
	formSpec       *ActionForm
	rowFocus       bool
	taskList       RowList
	messageList    RowList
	approvalList   RowList
	agentList      RowList
	invocationList RowList
	runtimeList    RowList
	settingsFocus  bool
	settingsCursor int
	confirm        *confirmState
	watcher        *fsnotify.Watcher
}

func New(s *service.Service, actor string) (Model, error) {
	st, e := s.State()
	projectID := "local project"
	if config, err := s.Store.Config(); err == nil && config.ProjectID != "" {
		projectID = config.ProjectID
	}
	hc := false
	if uc, err := identity.LoadUserConfig(); err == nil && uc.Theme == "high-contrast" {
		hc = true
	}
	return Model{
		svc: s, state: st, actor: actor, projectID: projectID, width: 100, height: 30, highContrast: hc,
		taskList: newRowList(taskRowSource{}), messageList: newRowList(messageRowSource{}),
		approvalList: newRowList(approvalRowSource{}), agentList: newRowList(agentRowSource{}),
		invocationList: newRowList(invocationRowSource{}), runtimeList: newRowList(runtimeRowSource{}),
	}, e
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
	if m.settingsFocus {
		return m.updateSettings(msg)
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
			m.moveHub(-1)
		case "down", "j":
			m.moveHub(1)
		case "left":
			m.moveHubView(-1)
		case "right":
			m.moveHubView(1)
		case "[":
			m.moveHubView(-1)
		case "]":
			m.moveHubView(1)
		case "enter":
			m.view = m.cursor
			m.focusCurrentView()
		case "/", "ctrl+p":
			m.palette = true
		case "o":
			m.openView("Overview")
		case "g":
			m.openView("Agents")
		case "i":
			m.openView("Invocations")
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

func (m *Model) openView(name string) {
	for index, viewName := range views {
		if viewName == name {
			m.view = index
			m.cursor = index
			m.notice = "Opened " + name
			return
		}
	}
}

func (m *Model) activeHubIndex() int {
	current := views[m.view]
	for hubIndex, hub := range navigationHubs {
		for _, viewName := range hub.Views {
			if viewName == current {
				return hubIndex
			}
		}
	}
	return 0
}

func (m *Model) moveHub(delta int) {
	next := max(0, min(len(navigationHubs)-1, m.activeHubIndex()+delta))
	m.openView(navigationHubs[next].Views[0])
	m.notice = ""
}

func (m *Model) moveHubView(delta int) {
	hub := navigationHubs[m.activeHubIndex()]
	current := views[m.view]
	position := 0
	for index, viewName := range hub.Views {
		if viewName == current {
			position = index
			break
		}
	}
	position = (position + delta + len(hub.Views)) % len(hub.Views)
	m.openView(hub.Views[position])
	m.notice = ""
}

func (m *Model) focusCurrentView() {
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
	case "Invocations":
		m.rowFocus = true
		m.invocationList.Refresh(m.state, m.actor)
	case "Runtimes":
		m.rowFocus = true
		m.runtimeList.Refresh(m.state, m.actor)
	case "Project settings":
		m.settingsFocus = true
	}
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
	m.invocationList.Refresh(m.state, m.actor)
	m.runtimeList.Refresh(m.state, m.actor)
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
	return palette{lipgloss.Color("#071216"), lipgloss.Color("#0D2024"), lipgloss.Color("#56D6C9"), lipgloss.Color("#E8B85C"), lipgloss.Color("#F07167"), lipgloss.Color("#B9A7E8"), lipgloss.Color("#78918F"), lipgloss.Color("#D7E5E3")}
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
	v.WindowTitle = "Agent Comms · Project Control"
	return v
}
func (m Model) sidebarWidth() int {
	if m.width < 72 {
		return 16
	}
	return 21
}
func (m Model) renderSidebar(p palette, w, h int) string {
	title := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("● AGENT COMMS")
	sub := lipgloss.NewStyle().Foreground(p.muted).Render(truncate(m.projectID, max(8, w-2)))
	rows := []string{title, sub, "", lipgloss.NewStyle().Foreground(p.muted).Render("OPERATIONS"), ""}
	activeHub := m.activeHubIndex()
	for i, hub := range navigationHubs {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.muted)
		if i == activeHub {
			marker = "▌ "
			style = style.Foreground(p.cyan).Bold(true)
		}
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
	case "Invocations":
		for _, invocation := range m.state.Invocations {
			if invocation.Status != "COMPLETED" && invocation.Status != "REJECTED" &&
				invocation.Status != "EXPIRED" && invocation.Status != "CANCELLED" {
				n++
			}
		}
	case "Runtimes":
		for _, runtime := range m.state.AgentRuntimes {
			if runtime.Status == "ONLINE" {
				n++
			}
		}
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
	title := views[m.view]
	if title == "Overview" {
		title = "PROJECT CONTROL"
	}
	header := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(title)
	meta := m.commandRail(p, w)
	tabs := m.renderHubTabs(p, w)
	pane := lipgloss.NewStyle().Width(w).Height(h).Padding(1, 2).Background(p.panel).Foreground(p.text)
	if m.form != "" {
		content := m.renderForm(p)
		return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content)
	}
	if m.confirm != nil {
		content := m.renderConfirm(p)
		return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content)
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
	case "Invocations":
		bodyContent = m.invocationList.View(p, m.state, m.actor, contentW, contentH)
	case "Runtimes":
		bodyContent = m.runtimeList.View(p, m.state, m.actor, contentW, contentH)
	case "Approvals":
		bodyContent = m.approvalList.View(p, m.state, m.actor, contentW, contentH)
	case "Documents":
		bodyContent = wrap.Render(m.documents(p))
	case "Contracts & decisions":
		bodyContent = wrap.Render(m.decisions(p))
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
	if m.err != nil {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.red).MaxWidth(contentW).Render("Error: "+m.err.Error())
	} else if m.notice != "" {
		content += "\n\n" + lipgloss.NewStyle().Foreground(p.cyan).MaxWidth(contentW).Render(m.notice)
	}
	return pane.Render(meta + "\n" + tabs + "\n\n" + header + "\n\n" + content + "\n\n" + m.navigationIndicator(p))
}

func (m Model) commandRail(p palette, width int) string {
	sequence := max(m.state.Integrity.ServerSequence, m.state.Integrity.CacheSequence)
	freshness := empty(m.state.Integrity.Connectivity, "LOCAL")
	hub := navigationHubs[m.activeHubIndex()].Name
	left := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("LIVE")
	detail := fmt.Sprintf("  %s / %s  ·  %s  ·  seq %d", hub, views[m.view], freshness, sequence)
	authority := strings.ToLower(string(m.state.Agents[m.actor].Role))
	right := "authority " + empty(authority, "unknown")
	gap := max(1, width-lipgloss.Width(left+detail)-lipgloss.Width(right)-4)
	return left + lipgloss.NewStyle().Foreground(p.muted).Render(detail) +
		strings.Repeat(" ", gap) + lipgloss.NewStyle().Foreground(p.amber).Render(right)
}

func (m Model) navigationIndicator(p palette) string {
	hub := navigationHubs[m.activeHubIndex()].Name
	location := lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("YOU ARE HERE") +
		lipgloss.NewStyle().Foreground(p.text).Render("  "+hub+"  ›  "+views[m.view])
	controls := lipgloss.NewStyle().Foreground(p.muted).
		Render("↑↓ change hub   ←→ change tab   Enter open workspace   / commands")
	return location + "\n" + controls
}

func (m Model) renderHubTabs(p palette, width int) string {
	hub := navigationHubs[m.activeHubIndex()]
	current := views[m.view]
	tabs := make([]string, 0, len(hub.Views))
	for _, name := range hub.Views {
		label := name
		style := lipgloss.NewStyle().Foreground(p.muted).Padding(0, 1)
		if name == current {
			style = style.Foreground(p.ink).Background(p.cyan).Bold(true)
		}
		tabs = append(tabs, style.Render(label))
	}
	return lipgloss.NewStyle().Width(width).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.muted).Render(strings.Join(tabs, " "))
}
func (m Model) renderForm(p palette) string {
	title, hint := "Form", ""
	if m.formSpec != nil {
		title, hint = m.formSpec.Title, m.formSpec.Hint
	}
	rows := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("EDIT / " + title),
		lipgloss.NewStyle().Foreground(p.muted).Render(hint),
		"",
	}
	for i, input := range m.inputs {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.text)
		if i == m.formFocus {
			marker = "▌ "
			style = style.Foreground(p.cyan).Bold(true)
		}
		rows = append(rows, style.Render(marker)+input.View(), "")
	}
	rows = append(rows,
		lipgloss.NewStyle().Foreground(p.muted).Render("Tab / Shift+Tab moves between fields"),
		lipgloss.NewStyle().Foreground(p.amber).Render("Enter continues · final Enter reviews changes · Esc cancels"),
	)
	if m.notice != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.amber).Render(m.notice))
	}
	if m.err != nil {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.red).Render(m.err.Error()))
	}
	return lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.cyan).PaddingLeft(2).MaxWidth(max(40, m.width-m.sidebarWidth()-10)).
		Render(strings.Join(rows, "\n"))
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
	keys := lipgloss.NewStyle().Foreground(p.muted).Render("[g] agents   [i] invocations   [n] create   [r] refresh   [/] commands")
	return lipgloss.NewStyle().Foreground(p.cyan).Render(status) + "\n\n" + top + "\n\n" + activity + "\n" + keys
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
		for _, runtime := range m.state.AgentRuntimes {
			if runtime.AgentID != agentID {
				continue
			}
			switch {
			case runtime.Health == "DEGRADED":
				signal = "▲ DEGRADED"
			case runtime.Status == "ONLINE":
				signal = "● ONLINE"
			case runtime.Status == "DRAINING":
				signal = "◐ DRAINING"
			default:
				signal = "○ " + runtime.Status
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
		if width < 54 {
			rows = append(rows, fmt.Sprintf("%-12s %s\n             %s", signal, agentID, truncate(work, width-13)))
			continue
		}
		rows = append(rows, fmt.Sprintf(
			"%-12s %-14s %-10s %s",
			signal, truncate(agent.DisplayName, 13), strings.ToLower(string(agent.Role)), truncate(work, max(10, width-42)),
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
		case "DEAD_LETTER":
			rows = append(rows, "✕ "+invocation.ID+"  delivery failed: "+invocation.Reason)
		case "PENDING":
			rows = append(rows, "→ "+invocation.ID+"  pending delivery to "+invocation.Target)
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
	return fmt.Sprintf("%s Chain verified: %t\n  Signed events: %d\n  Head: %s\n  Checkpoint: %s\n  Remote: %s\n  Consistency: %s\n  Connectivity: %s\n  Server sequence: %d\n  Cache sequence: %d\n\nRun `agent-comms verify` before recovery or migration.", mark, m.state.Integrity.Verified, m.state.Integrity.EventCount, m.state.Integrity.Head, m.state.Integrity.SyncState, empty(m.state.Integrity.Remote, "not configured"), empty(m.state.Integrity.Consistency, "LEGACY_LOCAL"), empty(m.state.Integrity.Connectivity, "LOCAL"), m.state.Integrity.ServerSequence, m.state.Integrity.CacheSequence)
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
