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
		{Label: "Principal type", Options: []string{"AGENT", "HUMAN"}},
	},
	Dispatch: func(m Model, v []string, _ string) (tea.Model, tea.Cmd) {
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
		m.refreshState()
		return m, nil
	},
}
var activateForm = &ActionForm{
	Title: "Activate agent",
	Hint:  "Assign a role, capabilities, and write scopes before this principal can act. Granting Orchestrator additionally requires your elevated-key passphrase, if one is registered.",
	Fields: []FormField{
		{Label: "Role", Options: []string{"AGENT", "OBSERVER", "ORCHESTRATOR", "OWNER"}, Required: true},
		{Label: "Capabilities (comma-separated)", Placeholder: "go,test"},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Elevated-key passphrase (Orchestrator grants only)", Mask: true},
	},
	CollectsPassphrase: true,
	// A plain Build+ConfirmIf can't see whether an Orchestrator grant
	// already has its required HUMAN-tier approval (ConfirmIf only gets the
	// built payload, not id or state), so this needs the full Dispatch
	// escape hatch: activate directly when no approval is needed or one
	// already exists, otherwise offer one confirm that chains
	// request+approve+activate as three separate signed events using the
	// passphrase already typed into this form.
	Dispatch: func(m Model, v []string, passphrase string) (tea.Model, tea.Cmd) {
		id := m.formTaskID
		role := model.Role(strings.ToUpper(strings.TrimSpace(v[0])))
		payload := model.AgentActivated{Role: role, Capabilities: splitCSV(v[1]), Scopes: splitCSV(v[2])}
		if role == model.RoleOrchestrator && !hasApprovedOrchestratorGrant(m.state, id) {
			m.form, m.inputs, m.formSpec = "", nil, nil
			m.confirm = &confirmState{
				prompt: "Granting " + id + " the Orchestrator role needs a HUMAN-tier approval first. " +
					"Request and approve it now, then activate?",
				typ: "agent.activate", id: id, payload: payload, passphrase: passphrase,
				chainOrchestratorApproval: true,
			}
			return m, nil
		}
		_, err := m.svc.ExecuteWithPassphrase(m.actor, "agent.activate", id, payload, passphrase)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
		m.notice = "Applied agent.activate to " + id
		m.refreshState()
		return m, nil
	},
}
var invocationPolicyForm = &ActionForm{
	Title: "Invocation policy",
	Hint:  "Controls which agents may wake this target. Sensitive work can remain human-gated.",
	Fields: []FormField{
		{Label: "Mode", Options: []string{"MANUAL", "TRUSTED", "AUTOMATIC", "DISABLED"}, Required: true},
		{Label: "Trusted actors (comma-separated)", Placeholder: ""},
		{Label: "Allowed scopes (comma-separated)", Placeholder: "src"},
		{Label: "Require human for sensitive", Options: []string{"yes", "no"}, Required: true},
		{Label: "Default consumer", Options: []string{"EITHER", "INTERACTIVE_ONLY", "WORKER_ONLY"}, Required: true},
		{Label: "Allowed consumers (comma-separated)", Placeholder: "INTERACTIVE_ONLY,WORKER_ONLY,EITHER"},
		{Label: "Preferred interactive runtime", Placeholder: ""},
	},
	Build: func(values []string) (any, error) {
		requireHuman := strings.EqualFold(values[3], "yes") || strings.EqualFold(values[3], "true")
		return model.InvocationPolicyUpdated{
			Mode: strings.ToUpper(values[0]), TrustedActors: splitCSV(values[1]),
			AllowedScopes:                 splitCSV(values[2]),
			DefaultConsumerMode:           model.ConsumerMode(strings.ToUpper(values[4])),
			AllowedConsumerModes:          consumerModeValues(splitCSV(values[5])),
			PreferredInteractiveRuntimeID: values[6],
			RequireHumanForSensitive:      requireHuman,
		}, nil
	},
}

func consumerModeValues(values []string) []model.ConsumerMode {
	result := make([]model.ConsumerMode, 0, len(values))
	for _, value := range values {
		result = append(result, model.ConsumerMode(strings.ToUpper(value)))
	}
	return result
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
			m.refreshState()
			return m, nil
		},
	}
	actInvocationPolicy = RowAction{
		Key: "p", Label: "policy", EventType: "invocation.policy.update", Form: invocationPolicyForm,
	}
	actRevoke = RowAction{
		Key: "x", Label: "revoke", EventType: "agent.revoke", Confirm: true,
		Payload: func() any { return model.RuntimeStatusChanged{Reason: "revoked from control room"} },
		Prompt:  func(id string) string { return "Revoke " + id + "? This cannot be reversed." },
	}
	// "e" not "n": the row-list's "n" key is globally reserved for opening
	// the panel's create form (updateRowList checks it before any row
	// action), so a RowAction keyed "n" would never be reachable.
	actRename = RowAction{
		Key: "e", Label: "rename", EventType: "agent.rename",
		Form: &ActionForm{
			Title: "Rename agent",
			Hint:  "Changes the principal's display name only; its ID and history are unchanged.",
			Fields: []FormField{
				{Label: "Display name", Placeholder: "", Required: true},
			},
			Build: func(v []string) (any, error) { return model.AgentRenamed{DisplayName: v[0]}, nil },
		},
	}
	// actDelete requires BOTH the ordinary owner/orchestrator elevation this
	// file already gates on AND a literal HUMAN principal
	// (internal/protocol/transitions.go's separate agent.delete check) AND
	// the actor's passphrase-protected elevated key
	// (protocol.RequiresElevatedKey, since the target is REVOKED) --
	// offered here the same way HUMAN-tier approval.approve already is
	// (approvals.go's approvalActionsFor). The masked "Elevated-key
	// passphrase" field below (CollectsPassphrase) feeds straight into
	// Service.ExecuteWithPassphrase, which decrypts the elevated key with
	// it directly -- this action genuinely completes in the TUI, it is not
	// CLI-only. Only leaving that field blank falls through to
	// nonInteractivePassphrasePrompt's clean CLI-only refusal, the same
	// fallback registering a *new* elevated key always requires
	// (agent elevate-key has no TUI/MCP form at all -- that step, not this
	// one, is the genuinely CLI-only part of this project's elevated-key
	// story).
	actDelete = RowAction{
		Key: "d", Label: "delete", EventType: "agent.delete",
		Form: &ActionForm{
			Title: "Delete revoked agent",
			Hint:  "Permanently removes this identity from active use; its signed history remains in the event log. Requires your elevated-key passphrase, if one is registered.",
			Fields: []FormField{
				{Label: "Reason", Placeholder: "duplicate registration, decommissioned, etc.", Required: true},
				{Label: "Elevated-key passphrase", Mask: true},
			},
			CollectsPassphrase: true,
			Build:              func(v []string) (any, error) { return model.AgentDeleted{Reason: v[0]}, nil },
			ConfirmIf: func(any) (bool, string) {
				return true, "Permanently delete this revoked agent? This cannot be reversed."
			},
		},
	}
)

// revokeActionFor mirrors protocol.RequiresElevatedKey's agent.revoke branch:
// revoking a different Orchestrator or HUMAN principal needs the actor's
// elevated key, exactly as sensitive as granting the role in the first
// place, so it gets a form with a masked passphrase field instead of the
// plain one-keypress confirm every other revoke uses. id == actor is never
// elevated (self-revocation is not an escalation) and always takes the
// plain path.
func revokeActionFor(a model.Agent, id, actor string) RowAction {
	if id == actor || (a.Role != model.RoleOrchestrator && a.PrincipalType != model.PrincipalHuman) {
		return actRevoke
	}
	return RowAction{
		Key: "x", Label: "revoke", EventType: "agent.revoke",
		Form: &ActionForm{
			Title: "Revoke agent",
			Hint:  "This principal holds Orchestrator role or HUMAN standing, so revoking it requires your elevated-key passphrase, if one is registered.",
			Fields: []FormField{
				{Label: "Elevated-key passphrase", Mask: true},
			},
			CollectsPassphrase: true,
			Build: func(v []string) (any, error) {
				return model.RuntimeStatusChanged{Reason: "revoked from control room"}, nil
			},
			ConfirmIf: func(any) (bool, string) { return true, "Revoke " + id + "? This cannot be reversed." },
		},
	}
}

// agentActionsFor mirrors service.go's elevated() gate: activate, suspend,
// rename, revoke, and delete all require the viewing actor to hold Owner or
// Orchestrator role, regardless of whose row is selected. Key rotation is
// always self-service (Service.RotateKey rotates the calling actor's own
// credential, never an arbitrary target), so it only appears on the actor's
// own row. Revoke is terminal (the target can never be reactivated,
// renamed, or suspended again) and offered from every non-terminal status;
// delete is offered only once REVOKED. The owner principal and, unless
// self-revoking, an orchestrator or human principal cannot be revoked by a
// non-human actor, and delete additionally requires a literal HUMAN
// principal unconditionally plus the actor's elevated key --
// internal/protocol/transitions.go enforces all of this regardless of what
// the TUI shows or hides.
func agentActionsFor(a model.Agent, id, actor string, role model.Role) []RowAction {
	elevated := role == model.RoleOwner || role == model.RoleOrchestrator
	var acts []RowAction
	if elevated {
		revoke := revokeActionFor(a, id, actor)
		switch a.Status {
		case "PENDING":
			acts = append(acts, actActivate, actRename, revoke)
		case "ACTIVE":
			acts = append(acts, actSuspend, actRename, revoke)
		case "SUSPENDED":
			acts = append(acts, actRename, revoke)
		case "REVOKED":
			acts = append(acts, actDelete)
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
	state, principal, role, ptype := 13, 16, 13, 8
	if width < 75 {
		state, principal, role, ptype = 11, 12, 10, 6
	}
	scopes := max(6, width-state-principal-role-ptype)
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
		rows = append(rows, table.Row{fmtStatus(a.Status), id, string(a.Role), string(a.PrincipalType), strings.Join(a.Scopes, ",")})
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
	return m.openActionForm(actorSwitchForm(m.actor, options, m.state.Agents), "actor.switch", "")
}

// actorRoleLabel is the picker's own defense against the confirmed-live
// trap of picking the wrong locally-saved credential blind: every candidate
// this project's own state actually knows about is shown with its role
// (and non-ACTIVE status, if any) right next to its ID, so "which of these
// is actually my owner/orchestrator identity" doesn't require leaving this
// form to check `agent list` first. A candidate with a locally-saved
// credential but no entry in current project state at all (registered
// elsewhere, or since revoked/deleted) is labeled accordingly rather than
// silently omitted.
func actorRoleLabel(id string, agents map[string]model.Agent) string {
	a, ok := agents[id]
	if !ok {
		return id + " (unregistered here)"
	}
	label := id + " (" + strings.ToLower(string(a.Role)) + ")"
	if a.Status != "" && a.Status != "ACTIVE" {
		label += " [" + strings.ToLower(a.Status) + "]"
	}
	return label
}

func actorSwitchForm(current string, options []string, agents map[string]model.Agent) *ActionForm {
	hint := "No other local identities registered on this machine."
	if len(options) > 0 {
		labeled := make([]string, len(options))
		for i, o := range options {
			labeled[i] = actorRoleLabel(o, agents)
		}
		hint = "Available locally: " + strings.Join(labeled, ", ")
	}
	return &ActionForm{
		Title:  "Switch actor",
		Hint:   hint,
		Fields: []FormField{{Label: "Actor ID", Placeholder: current, Required: true}},
		Dispatch: func(m Model, v []string, _ string) (tea.Model, tea.Cmd) {
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
			m.refreshState()
			return m, nil
		},
	}
}
