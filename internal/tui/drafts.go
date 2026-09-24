package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
		if m.draftCursor >= len(m.drafts) {
			m.draftCursor = max(0, len(m.drafts)-1)
		}
	}
}

func (m Model) updateDrafts(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		switch wheel.Button {
		case tea.MouseWheelUp:
			m.draftCursor = max(0, m.draftCursor-1)
		case tea.MouseWheelDown:
			m.draftCursor = min(len(m.drafts)-1, m.draftCursor+1)
		}
		m.keepDraftCursorVisible()
		return m, nil
	}
	if click, ok := msg.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		p := colors(m.highContrast)
		_, _, _, contentH := m.bodyLayout(p)
		bodyTop := m.bodyPrefixHeight(p)
		if mouse.Button == tea.MouseLeft && mouse.X >= m.sidebarWidth()+1 &&
			mouse.Y >= bodyTop && mouse.Y < bodyTop+contentH {
			index := mouse.Y - bodyTop + m.scrollOffset - 1
			if index >= 0 && index < len(m.drafts) {
				m.draftCursor = index
			}
		}
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "left":
		m.rowFocus = false
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/", "ctrl+p":
		m.palette = true
		m.paletteSelected = 0
	case "up", "k":
		m.draftCursor = max(0, m.draftCursor-1)
	case "down", "j":
		m.draftCursor = min(len(m.drafts)-1, m.draftCursor+1)
		m.draftCursor = max(0, m.draftCursor)
	case "pgdown", "f":
		m.draftCursor = min(len(m.drafts)-1, m.draftCursor+max(1, m.draftPageSize()))
		m.draftCursor = max(0, m.draftCursor)
	case "pgup", "b":
		m.draftCursor = max(0, m.draftCursor-max(1, m.draftPageSize()))
	case "n":
		return m.openActionForm(draftSaveForm, "draft.save", "")
	case "r":
		m.refresh()
	case "d":
		if len(m.drafts) == 0 {
			m.notice = "No draft selected."
			return m, nil
		}
		id := m.drafts[m.draftCursor].ID
		m.confirm = &confirmState{
			prompt: "Delete local draft " + fmt.Sprintf("%q", id) + "?",
			id:     id, localDraft: true,
		}
	case "?":
		m.notice = "↑/↓ select draft · [d] delete selected · [n] save draft · [r] refresh · [esc] back"
	}
	m.keepDraftCursorVisible()
	return m, nil
}

func (m Model) draftPageSize() int {
	_, _, _, contentH := m.bodyLayout(colors(m.highContrast))
	return max(1, contentH-1)
}

func (m *Model) keepDraftCursorVisible() {
	if len(m.drafts) == 0 {
		m.draftCursor = 0
		return
	}
	row := m.draftCursor + 1 // heading occupies the first content row
	if row < m.scrollOffset {
		m.scrollOffset = row
	}
	if row >= m.scrollOffset+m.draftPageSize() {
		m.scrollOffset = row - m.draftPageSize() + 1
	}
}

func (m Model) draftsView(p palette) string {
	width := m.contentWidth()
	if len(m.drafts) == 0 {
		return lipgloss.NewStyle().Foreground(p.muted).Inline(true).Render(
			ansi.Truncate("No drafts saved yet. Press [n] to save one.", width, "…"))
	}
	rows := []string{lipgloss.NewStyle().Foreground(p.muted).Inline(true).Render(
		ansi.Truncate("DRAFT ID · KIND · UPDATED", width, "…"))}
	for i, d := range m.drafts {
		marker := "  "
		if m.rowFocus && i == m.draftCursor {
			marker = "> "
		}
		line := marker + d.ID + " · " + d.Kind + " · " + d.UpdatedAt.Local().Format("2006-01-02 15:04:05")
		rows = append(rows, lipgloss.NewStyle().Foreground(p.text).Inline(true).Render(ansi.Truncate(line, width, "…")))
	}
	draftFooterParts := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[n]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("save draft"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[d]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("delete selected"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[r]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("refresh"),
	}
	if !m.rowFocus {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Inline(true).Render(
			ansi.Truncate("Press [enter] to select a draft.", width, "…")))
	}
	rows = append(rows, "", ansi.Truncate(strings.Join(draftFooterParts, " · "), width, "…"))
	return strings.Join(rows, "\n")
}
