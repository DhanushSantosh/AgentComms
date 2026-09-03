package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// RFC 0029: a "decision" is a governed document tagged `decision`. This
// view keeps its name and its place next to CONTRACT messages, but its
// rows are decision-tagged documents and its actions dispatch document.*.

const decisionTag = "decision"

func isDecisionDoc(d model.Document) bool {
	for _, tag := range d.Tags {
		if tag == decisionTag {
			return true
		}
	}
	return false
}

func decisionDocIDs(st model.State) []string {
	ids := make([]string, 0)
	for _, id := range service.SortedKeys(st.Documents) {
		if isDecisionDoc(st.Documents[id]) {
			ids = append(ids, id)
		}
	}
	return ids
}

var decisionCreateForm = &ActionForm{
	Title: "Record decision",
	Hint:  "Publishes a decision-tagged document; use supersede on an existing row to replace one.",
	Fields: []FormField{
		{Label: "Decision ID", Placeholder: "decision-001", Required: true},
		{Label: "Title", Placeholder: "Adopt trunk-based development", Required: true},
		{Label: "Statement", Placeholder: "", Required: true},
	},
	Build: func(v []string) (any, error) {
		return model.DocumentPayload{Title: v[1], Body: v[2], Tags: []string{decisionTag}}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
}

var decisionSupersedeForm = &ActionForm{
	Title: "Supersede decision",
	Hint:  "Publishes a new decision-tagged document and marks this one superseded.",
	Fields: []FormField{
		{Label: "New decision ID", Placeholder: "decision-002", Required: true},
		{Label: "Title", Placeholder: "", Required: true},
		{Label: "Statement", Placeholder: "", Required: true},
	},
	Dispatch: func(m Model, values []string, _ string) (tea.Model, tea.Cmd) {
		oldID := m.formTaskID
		if _, err := m.svc.Execute(m.actor, "document.create", values[0], model.DocumentPayload{
			Title: values[1], Body: values[2], Tags: []string{decisionTag},
		}); err != nil {
			m.err = err
			return m, nil
		}
		if _, err := m.svc.Execute(m.actor, "document.supersede", oldID, model.DocumentPayload{ReplacementID: values[0]}); err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Superseded " + oldID + " with " + values[0]
		m.refreshState()
		return m, nil
	},
}

var decSupersede = RowAction{Key: "s", Label: "supersede", EventType: "document.supersede", Form: decisionSupersedeForm}

type decisionRowSource struct{}

func (decisionRowSource) Columns(width int) []table.Column {
	status, title := 12, 24
	if width < 75 {
		status, title = 10, 16
	}
	statement := max(8, width-status-title)
	return []table.Column{
		{Title: "STATUS", Width: status},
		{Title: "TITLE", Width: title},
		{Title: "STATEMENT", Width: statement},
	}
}
func (decisionRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := decisionDocIDs(st)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		d := st.Documents[id]
		rows = append(rows, table.Row{d.Status, d.Title, d.Body})
	}
	return rows
}
func (decisionRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := decisionDocIDs(st)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}

func (decisionRowSource) Actions(id string, st model.State, actor string) []RowAction {
	if d, ok := st.Documents[id]; !ok || !isDecisionDoc(d) {
		return nil
	}
	return []RowAction{decSupersede}
}

// decisionMessages renders CONTRACT-kind messages read-only -- contracts
// are governed via message.* actions on the Inbox panel, not here.
func decisionMessages(st model.State) string {
	rows := []string{}
	for _, id := range service.SortedKeys(st.Messages) {
		x := st.Messages[id]
		if x.Kind == "CONTRACT" {
			rows = append(rows, "◇ "+id+"  "+x.Subject+" · "+x.Status)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	return "CONTRACTS (read-only; manage from Inbox)\n" + strings.Join(rows, "\n")
}
