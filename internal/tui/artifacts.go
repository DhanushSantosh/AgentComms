package tui

import (
	"strconv"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

// artifactAddForm calls Service.AddArtifact directly rather than going
// through the generic Execute path -- artifact.add hashes and copies a real
// file from disk (internal/service/service.go), it isn't a plain signed
// payload the caller constructs, so it needs the same direct-call shape
// agentRegisterForm already uses for Service.Register.
var artifactAddForm = &ActionForm{
	Title: "Add artifact",
	Hint:  "Hashes and stores a local file by its SHA-256 digest; the path must exist on this machine.",
	Fields: []FormField{
		{Label: "File path", Placeholder: "/path/to/file", Required: true},
	},
	Dispatch: func(m Model, values []string) (tea.Model, tea.Cmd) {
		_, err := m.svc.AddArtifact(m.actor, values[0])
		if err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Added artifact from " + values[0]
		m.refresh()
		return m, nil
	},
}

type artifactRowSource struct{}

func (artifactRowSource) Columns(width int) []table.Column {
	sha, size, storage := 20, 12, 10
	name := width - sha - size - storage
	if name < 14 {
		name = 14
	}
	return []table.Column{
		{Title: "SHA256", Width: sha},
		{Title: "NAME", Width: name},
		{Title: "SIZE", Width: size},
		{Title: "STORAGE", Width: storage},
	}
}
func (artifactRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := service.SortedKeys(st.Artifacts)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		a := st.Artifacts[id]
		rows = append(rows, table.Row{truncate(id, 18), a.Name, strconv.FormatInt(a.Size, 10) + "B", a.Storage})
	}
	return rows
}
func (artifactRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := service.SortedKeys(st.Artifacts)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}

// Actions returns nothing: artifacts are content-addressed and immutable
// once added (no update/delete concept exists anywhere in this project, CLI
// included -- `artifact show`/`verify` are read-only queries, not row
// actions).
func (artifactRowSource) Actions(id string, st model.State, actor string) []RowAction {
	return nil
}
