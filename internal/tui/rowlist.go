package tui

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

type FormField struct {
	Label       string
	Placeholder string
	Required    bool
}
type ActionForm struct {
	Title, Hint string
	Fields      []FormField
	Build       func(values []string) (any, error)
	ResolveID   func(fixedID string, values []string) string
	ConfirmIf   func(payload any) (ok bool, prompt string)
	Dispatch    func(m Model, values []string) (tea.Model, tea.Cmd)
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
	prompt  string
	typ     string
	id      string
	payload any
	onError func(err error, id string) error
}
type RowList struct {
	table  table.Model
	source RowSource
	mine   bool
}

func newRowList(source RowSource) RowList {
	return RowList{table: table.New(table.WithFocused(true), table.WithColumns(source.Columns(80))), source: source}
}
func (r *RowList) Refresh(st model.State, actor string) {
	rows := r.source.Rows(st, actor, r.mine)
	r.table.SetRows(rows)
	if len(rows) > 0 && r.table.Cursor() < 0 {
		r.table.SetCursor(0)
	}
}
func (r *RowList) SetMineFilter(mine bool, st model.State, actor string) {
	r.mine = mine
	r.Refresh(st, actor)
}
func (r RowList) SelectedID(st model.State, actor string) string {
	return r.source.RowID(r.table.Cursor(), st, actor, r.mine)
}
func (r RowList) Actions(id string, st model.State, actor string) []RowAction {
	return r.source.Actions(id, st, actor)
}
func rowListStyles(p palette) table.Styles {
	return table.Styles{
		Header:   lipgloss.NewStyle().Bold(true).Foreground(p.muted).Padding(0, 1),
		Cell:     lipgloss.NewStyle().Foreground(p.text).Padding(0, 1),
		Selected: lipgloss.NewStyle().Bold(true).Foreground(p.ink).Background(p.cyan).Padding(0, 1),
	}
}
func (r RowList) View(p palette, st model.State, actor string, w, h int) string {
	t := r.table
	t.SetColumns(r.source.Columns(w))
	t.SetWidth(w)
	t.SetHeight(max(3, h-3))
	t.SetStyles(rowListStyles(p))
	if len(t.Rows()) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No rows here yet.")
	}
	id := r.source.RowID(t.Cursor(), st, actor, r.mine)
	bindings := make([]key.Binding, 0)
	for _, act := range r.source.Actions(id, st, actor) {
		bindings = append(bindings, key.NewBinding(key.WithKeys(act.Key), key.WithHelp(act.Key, act.Label)))
	}
	footer := lipgloss.NewStyle().Foreground(p.muted).Render("↑/↓ select  esc back  ") + help.New().ShortHelpView(bindings)
	return t.View() + "\n" + footer
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
		return m.openActionForm(decisionCreateForm, "decision.create", "")
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
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	list := m.activeRowList()
	if list == nil {
		m.rowFocus = false
		return m, nil
	}
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
	case "?":
		m.notice = "↑/↓ select row · [key] run contextual action · n new · esc back · q quit"
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
			}
		}
	}
	var cmd tea.Cmd
	list.table, cmd = list.table.Update(msg)
	return m, cmd
}
func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "y", "Y", "enter":
		c := *m.confirm
		m.confirm = nil
		next, cmd := m.dispatchEvent(c.typ, c.id, c.payload)
		mm := next.(Model)
		if mm.err != nil && c.onError != nil {
			mm.err = c.onError(mm.err, c.id)
		}
		return mm, cmd
	case "n", "N", "esc":
		m.confirm = nil
		m.notice = "Cancelled."
	}
	return m, nil
}
func (m Model) renderConfirm(p palette) string {
	rows := []string{
		lipgloss.NewStyle().Foreground(p.amber).Bold(true).Render("REVIEW / Signed change"),
		m.confirm.prompt,
		"",
		lipgloss.NewStyle().Foreground(p.muted).Render("This action becomes part of project history."),
		lipgloss.NewStyle().Foreground(p.amber).Render("[y / enter] Sign and apply    [n / esc] Go back"),
	}
	return lipgloss.NewStyle().BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.amber).PaddingLeft(2).Render(strings.Join(rows, "\n"))
}
func (m Model) dispatchEvent(typ, id string, payload any) (tea.Model, tea.Cmd) {
	_, err := m.svc.Execute(m.actor, typ, id, payload)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	m.notice = typ + " applied to " + id
	m.refresh()
	return m, nil
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
		input.Placeholder = f.Placeholder
		input.CharLimit = 1200
		m.inputs[i] = input
	}
	var cmd tea.Cmd
	if len(m.inputs) > 0 {
		cmd = m.inputs[0].Focus()
	}
	m.form, m.formTaskID, m.formSpec, m.formFocus, m.palette, m.query = typ, id, spec, 0, false, ""
	return m, cmd
}
