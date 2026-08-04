package tui

import (
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// draftSaveForm calls Service.SaveDraft directly, bypassing Execute entirely
// -- drafts are explicitly non-authoritative (no signed event, never enters
// project history), matching the CLI's own "authoritative": false framing
// (internal/app/app.go's draftCmd).
var draftSaveForm = &ActionForm{
	Title: "Save draft",
	Hint:  "Local, non-authoritative scratch space -- never signed, never part of project history.",
	Fields: []FormField{
		{Label: "Draft ID", Placeholder: "draft-review-notes", Required: true},
		{Label: "Kind (document/message/artifact)", Placeholder: "document", Required: true},
		{Label: "Content", Placeholder: "", Required: true},
	},
	Dispatch: func(m Model, values []string, _ string) (tea.Model, tea.Cmd) {
		kind := strings.ToLower(strings.TrimSpace(values[1]))
		raw := []byte(values[2])
		if !json.Valid(raw) {
			var err error
			raw, err = json.Marshal(map[string]string{"content": values[2]})
			if err != nil {
				m.err = err
				return m, nil
			}
		}
		if err := m.svc.SaveDraft(values[0], kind, json.RawMessage(raw)); err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Saved draft " + values[0]
		m.refreshDrafts()
		return m, nil
	},
}

// refreshDrafts re-fetches this project's local drafts. Deliberately not
// called from refreshSilent's background file-watch tick -- drafts are a
// separate remote round-trip from the already-cached model.State, and
// unlike state there is no local file-watch signal to justify polling it on
// every background tick; it's refreshed on an explicit 'r' refresh, after
// saving one, and once at startup.
func (m *Model) refreshDrafts() {
	if drafts, err := m.svc.Drafts(50); err == nil {
		m.drafts = drafts
	}
}

func (m Model) draftsView(p palette) string {
	if len(m.drafts) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Render("No drafts saved yet. Press [n] to save one.")
	}
	rows := []string{lipgloss.NewStyle().Foreground(p.muted).Render("KIND        DRAFT                          UPDATED")}
	for _, d := range m.drafts {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.text).Render(
			padTo(d.Kind, 11)+" "+padTo(d.ID, 30)+" "+d.UpdatedAt.Local().Format("2006-01-02 15:04:05"),
		))
	}
	rows = append(rows, "", lipgloss.NewStyle().Foreground(p.amber).Render("[n] save draft   [r] refresh"))
	return strings.Join(rows, "\n")
}

func padTo(s string, width int) string {
	s = truncate(s, width)
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

