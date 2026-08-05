package tui

import (
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var envSetForm = &ActionForm{
	Title: "Set environment key",
	Hint:  "Project-scoped, plain-text configuration -- never store secrets here.",
	Fields: []FormField{
		{Label: "Key", Placeholder: "LOG_LEVEL", Required: true},
		{Label: "Value", Placeholder: "", Required: true},
	},
	Build: func(v []string) (any, error) {
		return model.EnvSetPayload{Key: v[0], Value: v[1]}, nil
	},
}

// envUpdateForm uses Dispatch instead of Build because the payload needs the
// row's own key (m.formTaskID) -- see decisions.go's decisionSupersedeForm
// for the same pattern.
var envUpdateForm = &ActionForm{
	Title: "Update value",
	Hint:  "Publishes a new value for this key.",
	Fields: []FormField{
		{Label: "Value", Placeholder: "", Required: true},
	},
	Dispatch: func(m Model, values []string, _ string) (tea.Model, tea.Cmd) {
		key := m.formTaskID
		_, err := m.svc.Execute(m.actor, "env.set", key, model.EnvSetPayload{Key: key, Value: values[0]})
		if err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Updated " + key
		m.refresh()
		return m, nil
	},
}

var (
	envUpdate = RowAction{Key: "e", Label: "update", EventType: "env.set", Form: envUpdateForm}
	envDelete = RowAction{
		Key: "x", Label: "delete", EventType: "env.delete",
		Dispatch: func(m Model, id string) (tea.Model, tea.Cmd) {
			_, err := m.svc.Execute(m.actor, "env.delete", id, model.EnvDeletePayload{Key: id})
			if err != nil {
				m.err = err
				return m, nil
			}
			m.err = nil
			m.notice = "Deleted " + id
			m.refresh()
			return m, nil
		},
	}
)

type envRowSource struct{}

func (envRowSource) Columns(width int) []table.Column {
	key, updatedBy, updatedAt := 20, 14, 18
	if width < 80 {
		key, updatedBy, updatedAt = 14, 10, 12
	}
	value := max(8, width-key-updatedBy-updatedAt)
	return []table.Column{
		{Title: "KEY", Width: key},
		{Title: "VALUE", Width: value},
		{Title: "UPDATED BY", Width: updatedBy},
		{Title: "UPDATED AT", Width: updatedAt},
	}
}
func (envRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := service.SortedKeys(st.Env)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		e := st.Env[id]
		updatedAt := "—"
		if !e.UpdatedAt.IsZero() {
			updatedAt = e.UpdatedAt.Local().Format(time.RFC3339)
		}
		rows = append(rows, table.Row{id, e.Value, e.UpdatedBy, updatedAt})
	}
	return rows
}
func (envRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := service.SortedKeys(st.Env)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}

// envActionsFor mirrors env.*'s elevation requirement
// (internal/protocol/transitions.go's elevated() list) -- ordinary
// owner-or-orchestrator, no elevated key involved.
func (envRowSource) Actions(id string, st model.State, actor string) []RowAction {
	if _, ok := st.Env[id]; !ok {
		return nil
	}
	role := st.Agents[actor].Role
	if role != model.RoleOwner && role != model.RoleOrchestrator {
		return nil
	}
	return []RowAction{envUpdate, envDelete}
}
