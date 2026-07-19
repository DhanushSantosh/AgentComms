package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var agentRegisterForm = &ActionForm{
	Title: "Register agent",
	Hint:  "Registration creates a pending identity; activate it next to assign a role and scopes.",
	Fields: []FormField{
		{Label: "Principal ID", Placeholder: "builder", Required: true},
		{Label: "Display name", Placeholder: ""},
		{Label: "Principal type (HUMAN/AGENT)", Placeholder: "AGENT"},
	},
	Dispatch: func(m Model, v []string) (tea.Model, tea.Cmd) {
		id := strings.TrimSpace(v[0])
		pt := strings.ToUpper(strings.TrimSpace(v[2]))
		if pt == "" {
			pt = "AGENT"
		}
		_, err := m.svc.Register(id, v[1], model.PrincipalType(pt))
		if err != nil {
			m.err = err
			return m, nil
		}
		m.err, m.form, m.inputs, m.formSpec = nil, "", nil, nil
		m.notice = "Registered " + id + " (pending activation)"
		m.refresh()
		return m, nil
	},
}
var activateForm = &ActionForm{
	Title: "Activate agent",
	Hint:  "Assign a role, capabilities, and write scopes before this principal can act.",
	Fields: []FormField{
		{Label: "Role (OWNER/ORCHESTRATOR/AGENT/OBSERVER)", Placeholder: "AGENT", Required: true},
		{Label: "Capabilities (comma-separated)", Placeholder: "go,test"},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
	},
	Build: func(v []string) (any, error) {
		role := strings.ToUpper(strings.TrimSpace(v[0]))
		return model.AgentActivated{Role: model.Role(role), Capabilities: splitCSV(v[1]), Scopes: splitCSV(v[2])}, nil
	},
}
var invocationPolicyForm = &ActionForm{
	Title: "Invocation policy",
	Hint:  "Controls which agents may wake this target. Sensitive work can remain human-gated.",
	Fields: []FormField{
		{Label: "Mode (MANUAL/TRUSTED/AUTOMATIC/DISABLED)", Placeholder: "MANUAL", Required: true},
		{Label: "Trusted actors (comma-separated)", Placeholder: ""},
		{Label: "Allowed scopes (comma-separated)", Placeholder: "src"},
		{Label: "Require human for sensitive (yes/no)", Placeholder: "yes", Required: true},
	},
	Build: func(values []string) (any, error) {
		requireHuman := strings.EqualFold(values[3], "yes") || strings.EqualFold(values[3], "true")
		return model.InvocationPolicyUpdated{
			Mode: strings.ToUpper(values[0]), TrustedActors: splitCSV(values[1]),
			AllowedScopes: splitCSV(values[2]), RequireHumanForSensitive: requireHuman,
		}, nil
	},
}

var (
	actActivate = RowAction{Key: "a", Label: "activate", EventType: "agent.activate", Form: activateForm}
	actSuspend  = RowAction{
		Key: "s", Label: "suspend", EventType: "agent.suspend", Confirm: true,
		Payload: func() any { return model.TaskStatus{} },
		Prompt:  func(id string) string { return "Suspend " + id + "? It cannot act again until reactivated." },
	}
	actRotateKey = RowAction{
		Key: "z", Label: "rotate key", EventType: "agent.rotate-key",
		Dispatch: func(m Model, id string) (tea.Model, tea.Cmd) {
			_, err := m.svc.RotateKey(m.actor)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.err = nil
			m.notice = "Rotated signing key for " + m.actor
			m.refresh()
			return m, nil
		},
	}
	actInvocationPolicy = RowAction{
		Key: "p", Label: "policy", EventType: "invocation.policy.update", Form: invocationPolicyForm,
	}
)

// agentActionsFor mirrors service.go's elevated() gate: activate and suspend
// both require the viewing actor to hold Owner or Orchestrator role,
// regardless of whose row is selected. Key rotation is always self-service
// (Service.RotateKey rotates the calling actor's own credential, never an
// arbitrary target), so it only appears on the actor's own row.
func agentActionsFor(a model.Agent, id, actor string, role model.Role) []RowAction {
	elevated := role == model.RoleOwner || role == model.RoleOrchestrator
	var acts []RowAction
	if elevated {
		switch a.Status {
		case "PENDING":
			acts = append(acts, actActivate)
		case "ACTIVE":
			acts = append(acts, actSuspend)
		}
		if a.PrincipalType == model.PrincipalAgent && a.Status == "ACTIVE" {
			acts = append(acts, actInvocationPolicy)
		}
	}
	if elevated && id == actor {
		acts = append(acts, actRotateKey)
	}
	return acts
}

type agentRowSource struct{}

func (agentRowSource) Columns(width int) []table.Column {
	state, principal, role, ptype := 11, 16, 13, 8
	scopes := width - state - principal - role - ptype
	if scopes < 10 {
		scopes = 10
	}
	return []table.Column{
		{Title: "STATE", Width: state},
		{Title: "PRINCIPAL", Width: principal},
		{Title: "ROLE", Width: role},
		{Title: "TYPE", Width: ptype},
		{Title: "SCOPES", Width: scopes},
	}
}
func (s agentRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := service.SortedKeys(st.Agents)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		a := st.Agents[id]
		rows = append(rows, table.Row{a.Status, id, string(a.Role), string(a.PrincipalType), strings.Join(a.Scopes, ",")})
	}
	return rows
}
func (s agentRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := service.SortedKeys(st.Agents)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}
func (agentRowSource) Actions(id string, st model.State, actor string) []RowAction {
	a, ok := st.Agents[id]
	if !ok {
		return nil
	}
	return agentActionsFor(a, id, actor, st.Agents[actor].Role)
}

// openActorSwitchForm lists only actor identities whose private key was
// generated on this machine for this project (identity.LoadUserConfig's
// profiles) — those are the only actors this session can actually sign
// events as; switching to anyone else would fail at the first Execute call.
func (m Model) openActorSwitchForm() (tea.Model, tea.Cmd) {
	cfg, err := m.svc.Store.Config()
	if err != nil {
		m.err = err
		return m, nil
	}
	uc, _ := identity.LoadUserConfig()
	var options []string
	for _, p := range uc.Profiles {
		if p.ProjectID == cfg.ProjectID {
			options = append(options, p.Actor)
		}
	}
	sort.Strings(options)
	return m.openActionForm(actorSwitchForm(m.actor, options), "actor.switch", "")
}
func actorSwitchForm(current string, options []string) *ActionForm {
	hint := "No other local identities registered on this machine."
	if len(options) > 0 {
		hint = "Available locally: " + strings.Join(options, ", ")
	}
	return &ActionForm{
		Title:  "Switch actor",
		Hint:   hint,
		Fields: []FormField{{Label: "Actor ID", Placeholder: current, Required: true}},
		Dispatch: func(m Model, v []string) (tea.Model, tea.Cmd) {
			candidate := strings.TrimSpace(v[0])
			found := false
			for _, o := range options {
				if o == candidate {
					found = true
					break
				}
			}
			if !found {
				m.err = fmt.Errorf("no local credential for actor %q; available: %s", candidate, strings.Join(options, ", "))
				return m, nil
			}
			m.actor = candidate
			m.form, m.inputs, m.formSpec, m.err = "", nil, nil, nil
			m.notice = "Switched to " + candidate
			m.refresh()
			return m, nil
		},
	}
}
