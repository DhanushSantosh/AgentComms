package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var decisionCreateForm = &ActionForm{
	Title: "Record decision",
	Hint:  "Publishes a standalone decision; use supersede on an existing row to replace one.",
	Fields: []FormField{
		{Label: "Decision ID", Placeholder: "decision-001", Required: true},
		{Label: "Title", Placeholder: "Adopt trunk-based development", Required: true},
		{Label: "Statement", Placeholder: "", Required: true},
		{Label: "To (comma-separated, optional)", Placeholder: ""},
	},
	Build: func(v []string) (any, error) {
		return model.DecisionPayload{Title: v[1], Statement: v[2], To: splitCSV(v[3])}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
}

// decisionSupersedeForm uses Dispatch instead of Build/ResolveID because the
// payload itself needs the ROW's own ID (the decision being replaced, held
// in m.formTaskID by the RowAction that opened this form) as Supersedes,
// while the event's target ID is the NEW decision being published --
// ActionForm's Build has no access to formTaskID, only Dispatch does (see
// invocations.go's invRedeliver for the same pattern).
var decisionSupersedeForm = &ActionForm{
	Title: "Supersede decision",
	Hint:  "Publishes a new decision and marks this one superseded.",
	Fields: []FormField{
		{Label: "New decision ID", Placeholder: "decision-002", Required: true},
		{Label: "Title", Placeholder: "", Required: true},
		{Label: "Statement", Placeholder: "", Required: true},
		{Label: "To (comma-separated, optional)", Placeholder: ""},
	},
	Dispatch: func(m Model, values []string, _ string) (tea.Model, tea.Cmd) {
		oldID := m.formTaskID
		_, err := m.svc.Execute(m.actor, "decision.supersede", values[0], model.DecisionPayload{
			Title: values[1], Statement: values[2], To: splitCSV(values[3]), Supersedes: oldID,
		})
		if err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Superseded " + oldID + " with " + values[0]
		m.refreshState()
		return m, nil
	},
}

var decSupersede = RowAction{Key: "s", Label: "supersede", EventType: "decision.supersede", Form: decisionSupersedeForm}

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
	ids := service.SortedKeys(st.Decisions)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		d := st.Decisions[id]
		rows = append(rows, table.Row{d.Status, d.Title, d.Statement})
	}
	return rows
}
func (decisionRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := service.SortedKeys(st.Decisions)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}

// decisionActionsFor offers supersede on any decision -- decision.* has no
// elevation requirement beyond the ordinary active-principal write check
// (internal/protocol/transitions.go), and superseding an already-superseded
// decision is permitted (matching the CLI, which applies no status gate
// either).
func (decisionRowSource) Actions(id string, st model.State, actor string) []RowAction {
	if _, ok := st.Decisions[id]; !ok {
		return nil
	}
	return []RowAction{decSupersede}
}

// decisionMessages renders CONTRACT-kind messages read-only, unchanged from
// before this file existed -- contracts are governed via message.* actions
// on the Inbox panel, not decision.*, so they stay display-only here.
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
