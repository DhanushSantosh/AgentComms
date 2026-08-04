package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var approvalRequestForm = &ActionForm{
	Title: "Request approval",
	Hint:  "Destructive, irreversible, external, production-data, credential, and force-push actions require an approver.",
	Fields: []FormField{
		{Label: "Approval ID", Placeholder: "approval-001", Required: true},
		{Label: "Tier", Options: []string{"ORCHESTRATOR", "HUMAN"}},
		{Label: "Action", Placeholder: "task.takeover:task-001", Required: true},
		{Label: "Reason", Placeholder: ""},
		{Label: "Affected (comma-separated)", Placeholder: ""},
	},
	Build: func(v []string) (any, error) {
		tier := strings.ToUpper(strings.TrimSpace(v[1]))
		if tier == "" {
			tier = "ORCHESTRATOR"
		}
		return model.ApprovalRequested{Tier: tier, Action: v[2], Reason: v[3], Affected: splitCSV(v[4])}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
}

var (
	appApprove = RowAction{Key: "y", Label: "approve", EventType: "approval.approve", Payload: func() any { return model.ApprovalResponse{} }}
	appReject  = RowAction{
		Key: "x", Label: "reject", EventType: "approval.reject", Confirm: true,
		Payload: func() any { return model.ApprovalResponse{} },
		Prompt: func(id string) string {
			return "Reject approval " + id + "? The requester will need to submit a new request."
		},
	}
)

// approveActionFor mirrors protocol.RequiresElevatedKey's approval.approve
// branch: completing a HUMAN-tier approval needs the actor's elevated key,
// so it gets a form with a masked passphrase field instead of the plain
// one-keypress approve every other (ORCHESTRATOR-tier) approval uses --
// keeping the fast path fast for the common case.
func approveActionFor(a model.Approval) RowAction {
	if a.Tier != "HUMAN" {
		return appApprove
	}
	return RowAction{
		Key: "y", Label: "approve", EventType: "approval.approve",
		Form: &ActionForm{
			Title: "Approve (HUMAN tier)",
			Hint:  "This is a HUMAN-tier approval; completing it requires your elevated-key passphrase, if one is registered.",
			Fields: []FormField{
				{Label: "Elevated-key passphrase", Mask: true},
			},
			CollectsPassphrase: true,
			Build:              func(v []string) (any, error) { return model.ApprovalResponse{}, nil },
		},
	}
}

// approvalActionsFor mirrors service.go's elevated() gate (approval.approve
// and approval.reject both require Owner or Orchestrator role) plus the
// HUMAN-tier check on approve (an AGENT principal can never approve a
// HUMAN-tier approval, regardless of role).
func approvalActionsFor(a model.Approval, role model.Role, pt model.PrincipalType) []RowAction {
	if a.Status != "PENDING" {
		return nil
	}
	if role != model.RoleOwner && role != model.RoleOrchestrator {
		return nil
	}
	var acts []RowAction
	if a.Tier != "HUMAN" || pt == model.PrincipalHuman {
		acts = append(acts, approveActionFor(a))
	}
	acts = append(acts, appReject)
	return acts
}

type approvalRowSource struct{}

func (approvalRowSource) Columns(width int) []table.Column {
	id, tier, status := 14, 12, 10
	action := width - id - tier - status
	if action < 15 {
		action = 15
	}
	return []table.Column{
		{Title: "ID", Width: id},
		{Title: "TIER", Width: tier},
		{Title: "STATUS", Width: status},
		{Title: "ACTION", Width: action},
	}
}
func (approvalRowSource) filteredIDs(st model.State) []string {
	return service.SortedKeys(st.Approvals)
}
func (s approvalRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := s.filteredIDs(st)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		a := st.Approvals[id]
		rows = append(rows, table.Row{id, a.Tier, a.Status, a.Action})
	}
	return rows
}
func (s approvalRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := s.filteredIDs(st)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}
func (approvalRowSource) Actions(id string, st model.State, actor string) []RowAction {
	a, ok := st.Approvals[id]
	if !ok {
		return nil
	}
	agent := st.Agents[actor]
	return approvalActionsFor(a, agent.Role, agent.PrincipalType)
}
