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
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
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
}

func New(s *service.Service, actor string) (Model, error) {
	st, e := s.State()
	return Model{svc: s, state: st, actor: actor, width: 100, height: 30}, e
}
func (m Model) Init() tea.Cmd { return tea.RequestBackgroundColor }
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.form != "" {
		return m.updateForm(msg)
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
		case "down", "j":
			if m.cursor < len(views)-1 {
				m.cursor++
			}
		case "enter":
			m.view = m.cursor
		case "/", "ctrl+p":
			m.palette = true
		case "r":
			m.refresh()
		case "?":
			m.notice = "↑/↓ navigate · enter open · / commands · r refresh · q quit"
		case "h":
			m.highContrast = !m.highContrast
		case "n":
			if views[m.view] == "Tasks" || views[m.view] == "My work" {
				return m.openTaskForm()
			}
		}
	}
	return m, nil
}
func (m *Model) refresh() {
	m.state, m.err = m.svc.State()
	if m.err == nil {
		m.notice = "State refreshed at " + time.Now().Format("15:04:05")
	}
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
	labels := []string{"Task ID", "Title", "Branch", "Resources (comma-separated)"}
	placeholders := []string{"task-001", "Implement API", "feature/api", "src/api,tests/api"}
	m.inputs = make([]textinput.Model, len(labels))
	for i := range labels {
		input := textinput.New()
		input.Prompt = labels[i] + ": "
		input.Placeholder = placeholders[i]
		input.CharLimit = 240
		m.inputs[i] = input
	}
	cmd := m.inputs[0].Focus()
	m.form, m.formFocus, m.palette, m.query = "create-task", 0, false, ""
	return m, cmd
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc":
			m.form, m.inputs, m.err = "", nil, nil
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
			id, title := strings.TrimSpace(m.inputs[0].Value()), strings.TrimSpace(m.inputs[1].Value())
			branch, resources := strings.TrimSpace(m.inputs[2].Value()), splitCSV(m.inputs[3].Value())
			if id == "" || title == "" || branch == "" || len(resources) == 0 {
				m.notice = "Complete every required field."
				return m, nil
			}
			_, err := m.svc.Execute(m.actor, "task.create", id, model.TaskCreated{Title: title, Repository: "local", Branch: branch, Resources: resources, Risk: "ROUTINE"})
			if err != nil {
				m.err = err
				return m, nil
			}
			m.form, m.inputs, m.err = "", nil, nil
			m.notice = "Created task " + id
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
	sidebarW := 25
	if m.width < 90 {
		sidebarW = 20
	}
	contentW := m.width - sidebarW - 3
	if contentW < 40 {
		contentW = 40
	}
	side := m.renderSidebar(p, sidebarW)
	body := m.renderBody(p, contentW)
	screen := lipgloss.JoinHorizontal(lipgloss.Top, side, " ", body)
	if m.palette {
		screen = m.renderPalette(p, screen)
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "Agent Comms · Signal room"
	return v
}
func (m Model) renderSidebar(p palette, w int) string {
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
		label := name
		if lipgloss.Width(label) > w-7 {
			label = label[:min(len(label), w-8)] + "…"
		}
		rows = append(rows, style.Render(fmt.Sprintf("%s%-*s %s", marker, w-7, label, badge)))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.muted).Render("/ commands   ? help"))
	return lipgloss.NewStyle().Width(w).Height(max(20, m.height-1)).Padding(1).Background(p.ink).Foreground(p.text).Render(strings.Join(rows, "\n"))
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
func (m Model) renderBody(p palette, w int) string {
	header := lipgloss.NewStyle().Foreground(p.text).Bold(true).Render(views[m.view])
	meta := lipgloss.NewStyle().Foreground(p.muted).Render(fmt.Sprintf("profile %s · %d events · %s", m.actor, m.state.Integrity.EventCount, m.state.Integrity.SyncState))
	if m.form != "" {
		content := m.renderForm(p)
		return lipgloss.NewStyle().Width(w).Height(max(20, m.height-1)).Padding(1, 2).Background(p.panel).Foreground(p.text).Render(header + "\n" + meta + "\n\n" + content)
	}
	content := ""
	switch views[m.view] {
	case "Overview":
		content = m.overview(p)
	case "My work":
		content = m.tasks(p, true)
	case "Tasks":
		content = m.tasks(p, false)
	case "Inbox":
		content = m.inbox(p)
	case "Agents":
		content = m.agents(p)
	case "Approvals":
		content = m.approvals(p)
	case "Documents":
		content = m.documents(p)
	case "Contracts & decisions":
		content = m.decisions(p)
	case "Blockers":
		content = m.blockers(p)
	case "Integrity & sync":
		content = m.integrity(p)
	case "Activity":
		content = m.chain(p)
	case "Archive search":
		content = m.archive(p)
	}
	status := ""
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(p.red).Render("Error: " + m.err.Error())
	} else if m.notice != "" {
		status = lipgloss.NewStyle().Foreground(p.cyan).Render(m.notice)
	}
	return lipgloss.NewStyle().Width(w).Height(max(20, m.height-1)).Padding(1, 2).Background(p.panel).Foreground(p.text).Render(header + "\n" + meta + "\n\n" + content + "\n\n" + status)
}
func (m Model) renderForm(p palette) string {
	rows := []string{lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("Create task"), lipgloss.NewStyle().Foreground(p.muted).Render("Declare the branch and protected write resources before work begins."), ""}
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
func (m Model) tasks(p palette, mine bool) string {
	rows := []string{"STATUS       TASK                 OWNER         LEASE      RESOURCES"}
	for _, id := range service.SortedKeys(m.state.Tasks) {
		t := m.state.Tasks[id]
		if t.Archived || (mine && t.Owner != m.actor) {
			continue
		}
		lease := "—"
		if !t.LeaseUntil.IsZero() {
			lease = time.Until(t.LeaseUntil).Round(time.Minute).String()
		}
		rows = append(rows, fmt.Sprintf("%-12s %-20s %-13s %-10s %s", t.Status, id, t.Owner, lease, strings.Join(t.Resources, ",")))
	}
	if len(rows) == 1 {
		return "No tasks here. Press / and choose Create task to coordinate the next piece of work."
	}
	return strings.Join(rows, "\n")
}
func (m Model) inbox(p palette) string {
	rows := []string{"KIND       FROM          SUBJECT                              STATE"}
	for _, id := range service.SortedKeys(m.state.Messages) {
		x := m.state.Messages[id]
		for _, to := range x.To {
			if to == m.actor || m.actor == "owner" {
				rows = append(rows, fmt.Sprintf("%-10s %-13s %-36s %s", x.Kind, x.From, x.Subject, x.Status))
				break
			}
		}
	}
	if len(rows) == 1 {
		return "Inbox is clear. Durable actions, contracts, blockers, and decisions will appear here."
	}
	return strings.Join(rows, "\n")
}
func (m Model) agents(p palette) string {
	rows := []string{"STATE       PRINCIPAL        ROLE            TYPE       SCOPES"}
	for _, id := range service.SortedKeys(m.state.Agents) {
		a := m.state.Agents[id]
		rows = append(rows, fmt.Sprintf("%-11s %-16s %-15s %-10s %s", a.Status, id, a.Role, a.PrincipalType, strings.Join(a.Scopes, ",")))
	}
	return strings.Join(rows, "\n")
}
func (m Model) approvals(p palette) string {
	rows := []string{}
	for _, id := range service.SortedKeys(m.state.Approvals) {
		a := m.state.Approvals[id]
		rows = append(rows, fmt.Sprintf("◆ %-16s %-9s %-10s %s", id, a.Tier, a.Status, a.Action))
	}
	if len(rows) == 0 {
		return "No approval requests. Elevated actions will wait here for an eligible principal."
	}
	return strings.Join(rows, "\n")
}
func (m Model) documents(p palette) string {
	rows := []string{"STATUS    VERSION  DOCUMENT             AUTHOR        TAGS"}
	for _, id := range service.SortedKeys(m.state.Documents) {
		d := m.state.Documents[id]
		rows = append(rows, fmt.Sprintf("%-9s %-7d %-20s %-13s %s", d.Status, d.Version, id, d.Author, strings.Join(d.Tags, ",")))
	}
	if len(rows) == 1 {
		return "No living documents yet. Use `agent-comms document create` to record API contracts, guardrails, and shared decisions."
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
		return "No contracts or decisions recorded. Use durable records when shared understanding matters."
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
	ev, e := m.svc.Store.Events()
	if e != nil {
		return e.Error()
	}
	start := max(0, len(ev)-7)
	rows := []string{}
	for i := start; i < len(ev); i++ {
		v := ev[i]
		joint := "├─"
		if i == len(ev)-1 {
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
