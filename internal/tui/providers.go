package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// providerReadOnly explains why an actor sees the Providers view without
// actions; the authority would reject the form anyway (RFC 0050).
const providerReadOnly = "only the owner or an orchestrator can register or retire providers"

// canManageProviders mirrors provider.*'s place in
// internal/protocol/transitions.go's elevated() list: the owner or an active
// orchestrator, human or agent. Client-side only; the authority re-checks.
func canManageProviders(st model.State, actor string) bool {
	principal, ok := st.Agents[actor]
	return ok && principal.Status == "ACTIVE" &&
		(principal.Role == model.RoleOwner || principal.Role == model.RoleOrchestrator)
}

var providerRegisterForm = &ActionForm{
	Title: "Register provider",
	Hint:  "Agents can then register as <name> or <name>-<suffix>. 2-24 lower-case letters and digits, no hyphens.",
	Fields: []FormField{
		{Label: "Name", Placeholder: "gemini", Required: true},
		{Label: "Display name", Placeholder: "Google Gemini CLI"},
		{Label: "Description", Placeholder: ""},
	},
	Dispatch: func(m Model, v []string, _ string) (tea.Model, tea.Cmd) {
		name := strings.ToLower(strings.TrimSpace(v[0]))
		_, err := m.svc.Execute(m.actor, "provider.register", name, model.ProviderRegistered{
			DisplayName: strings.TrimSpace(v[1]), Description: strings.TrimSpace(v[2]),
		})
		if err != nil {
			m.err = err
			return m, nil
		}
		m.err, m.form, m.inputs, m.formSpec = nil, "", nil, nil
		m.notice = "Registered provider " + name
		m.refreshState()
		m.providerList.SelectID(name, m.state, m.actor)
		return m, nil
	},
}

// providerRetireForm uses Dispatch because the payload needs the row's own
// name (m.formTaskID); see env.go's envUpdateForm for the same pattern.
var providerRetireForm = &ActionForm{
	Title: "Retire provider",
	Hint:  "New agents can no longer register under it. Existing agents keep working; suspend or revoke them separately.",
	Fields: []FormField{
		{Label: "Reason", Placeholder: "", Required: true},
	},
	Dispatch: func(m Model, v []string, _ string) (tea.Model, tea.Cmd) {
		name := m.formTaskID
		listings, _ := model.ProviderListings(m.state)
		active := listings[name].ActiveAgents
		if _, err := m.svc.Execute(m.actor, "provider.retire", name, model.ProviderRetired{Reason: strings.TrimSpace(v[0])}); err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Retired provider " + name
		if len(active) > 0 {
			m.notice += "; still active: " + strings.Join(active, ", ")
		}
		m.refreshState()
		return m, nil
	},
}

var (
	providerRetire     = RowAction{Key: "x", Label: "retire", EventType: "provider.retire", Form: providerRetireForm}
	providerReactivate = RowAction{
		Key: "a", Label: "reactivate", EventType: "provider.register", Confirm: true,
		Payload: func() any { return model.ProviderRegistered{} },
		Prompt: func(id string) string {
			return "Reactivate provider " + id + "? New agents can register under it again."
		},
	}
)

type providerRowSource struct{}

func (providerRowSource) Columns(width int) []table.Column {
	name, status, kind, agents, added := 14, 9, 11, 7, 18
	if width < 80 {
		name, status, kind, agents, added = 11, 8, 10, 6, 11
	}
	display := max(8, width-name-status-kind-agents-added)
	return []table.Column{
		{Title: "NAME", Width: name},
		{Title: "STATUS", Width: status},
		{Title: "KIND", Width: kind},
		{Title: "AGENTS", Width: agents},
		{Title: "DISPLAY NAME", Width: display},
		{Title: "ADDED", Width: added},
	}
}
func (providerRowSource) IDs(st model.State, _ string, _ bool) []string {
	_, order := model.ProviderListings(st)
	return order
}
func (providerRowSource) Rows(st model.State, _ string, _ bool) []table.Row {
	listings, order := model.ProviderListings(st)
	rows := make([]table.Row, 0, len(order))
	for _, name := range order {
		listing := listings[name]
		kind, added := "registered", "—"
		if !listing.CreatedAt.IsZero() {
			added = listing.CreatedAt.Local().Format(time.RFC3339)
		}
		if listing.BuiltIn {
			kind, added = "built-in", "—"
		}
		rows = append(rows, table.Row{name, listing.Status, kind, fmt.Sprint(len(listing.ActiveAgents)), listing.DisplayName, added})
	}
	return rows
}
func (providerRowSource) RowID(idx int, st model.State, _ string, _ bool) string {
	_, order := model.ProviderListings(st)
	if idx < 0 || idx >= len(order) {
		return ""
	}
	return order[idx]
}

// Actions: built-ins have none; a registered provider can be retired while
// ACTIVE and reactivated once RETIRED, by the owner or an orchestrator.
func (providerRowSource) Actions(id string, st model.State, actor string) []RowAction {
	provider, ok := st.Providers[id]
	if !ok || !canManageProviders(st, actor) {
		return nil
	}
	if provider.Status == model.ProviderStatusRetired {
		return []RowAction{providerReactivate}
	}
	return []RowAction{providerRetire}
}

// agentProviderLabel names an agent's provider and how the project knows
// it, for the agent inspector.
func agentProviderLabel(st model.State, agent model.Agent) string {
	if agent.PrincipalType != model.PrincipalAgent {
		return "—"
	}
	name, ok := model.RecognizedProviders(st).ProviderOf(agent.ID)
	if !ok {
		return "—"
	}
	switch {
	case model.IsBuiltInProvider(name):
		return name + " (built-in)"
	case st.Providers[name].Status == model.ProviderStatusRetired:
		return name + " (retired)"
	default:
		return name + " (registered)"
	}
}
