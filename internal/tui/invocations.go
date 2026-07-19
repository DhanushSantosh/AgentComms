package tui

import (
	"strings"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var invocationRequestForm = &ActionForm{
	Title: "Invoke an agent",
	Hint:  "Creates a durable request. Target policy decides whether approval is required.",
	Fields: []FormField{
		{Label: "Invocation ID", Placeholder: "inv-review-001", Required: true},
		{Label: "Target agent", Placeholder: "reviewer", Required: true},
		{Label: "Instruction", Placeholder: "Review the current implementation", Required: true},
		{Label: "Expected result", Placeholder: "Post a concise review"},
		{Label: "Priority (LOW/NORMAL/HIGH/URGENT)", Placeholder: "NORMAL"},
		{Label: "Related task ID", Placeholder: ""},
		{Label: "Related message ID", Placeholder: ""},
	},
	Build: func(values []string) (any, error) {
		priority := strings.ToUpper(values[4])
		if priority == "" {
			priority = "NORMAL"
		}
		return model.InvocationRequested{
			Target: values[1], Instruction: values[2], ExpectedResult: values[3],
			Priority: priority, TaskID: values[5], MessageID: values[6],
		}, nil
	},
	ResolveID: func(_ string, values []string) string { return values[0] },
}

var invocationClaimForm = &ActionForm{
	Title: "Claim invocation",
	Hint:  "The authoritative transaction permits only one runtime instance to claim it.",
	Fields: []FormField{
		{Label: "Runtime ID", Placeholder: "reviewer-runtime", Required: true},
	},
	Build: func(values []string) (any, error) {
		return model.InvocationClaimed{RuntimeID: values[0]}, nil
	},
}

var invocationWaitForm = &ActionForm{
	Title: "Wait for input",
	Hint:  "Explain exactly what must happen before the agent can resume.",
	Fields: []FormField{
		{Label: "Reason", Placeholder: "Waiting for CI results", Required: true},
	},
	Build: func(values []string) (any, error) {
		return model.InvocationWaiting{Reason: values[0]}, nil
	},
}

var invocationCompleteForm = &ActionForm{
	Title: "Complete invocation",
	Hint:  "Attach a result message when the result needs a durable body or evidence.",
	Fields: []FormField{
		{Label: "Summary", Placeholder: "Review completed", Required: true},
		{Label: "Result message ID", Placeholder: ""},
	},
	Build: func(values []string) (any, error) {
		return model.InvocationCompleted{Summary: values[0], ResultMessageID: values[1]}, nil
	},
}

func invocationReasonForm(title, hint string) *ActionForm {
	return &ActionForm{
		Title: title, Hint: hint,
		Fields: []FormField{{Label: "Reason", Placeholder: "Explain why", Required: true}},
		Build: func(values []string) (any, error) {
			return model.InvocationRejected{Reason: values[0]}, nil
		},
	}
}

var (
	invClaim    = RowAction{Key: "c", Label: "claim", EventType: "invocation.claim", Form: invocationClaimForm}
	invStart    = RowAction{Key: "s", Label: "start", EventType: "invocation.start", Payload: func() any { return model.InvocationProgress{} }}
	invWait     = RowAction{Key: "w", Label: "wait", EventType: "invocation.wait", Form: invocationWaitForm}
	invResume   = RowAction{Key: "u", Label: "resume", EventType: "invocation.resume", Payload: func() any { return model.InvocationProgress{} }}
	invComplete = RowAction{Key: "x", Label: "complete", EventType: "invocation.complete", Form: invocationCompleteForm}
	invReject   = RowAction{Key: "j", Label: "reject", EventType: "invocation.reject", Form: invocationReasonForm("Reject invocation", "The target agent must provide a reason.")}
	invCancel   = RowAction{Key: "k", Label: "cancel", EventType: "invocation.cancel", Form: invocationReasonForm("Cancel invocation", "Owners, orchestrators, or the requester may cancel active work.")}
)

type invocationRowSource struct{}

func (invocationRowSource) Columns(width int) []table.Column {
	status, priority, target, requester := 12, 9, 14, 14
	instruction := max(18, width-status-priority-target-requester)
	return []table.Column{
		{Title: "STATUS", Width: status}, {Title: "PRIORITY", Width: priority},
		{Title: "TARGET", Width: target}, {Title: "REQUESTER", Width: requester},
		{Title: "INSTRUCTION", Width: instruction},
	}
}

func (invocationRowSource) Rows(state model.State, actor string, mine bool) []table.Row {
	ids := service.SortedKeys(state.Invocations)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		invocation := state.Invocations[id]
		if mine && invocation.Target != actor && invocation.RequestedBy != actor {
			continue
		}
		rows = append(rows, table.Row{
			invocation.Status, invocation.Priority, invocation.Target,
			invocation.RequestedBy, invocation.Instruction,
		})
	}
	return rows
}

func invocationIDs(state model.State, actor string, mine bool) []string {
	ids := make([]string, 0, len(state.Invocations))
	for _, id := range service.SortedKeys(state.Invocations) {
		invocation := state.Invocations[id]
		if !mine || invocation.Target == actor || invocation.RequestedBy == actor {
			ids = append(ids, id)
		}
	}
	return ids
}

func (invocationRowSource) RowID(index int, state model.State, actor string, mine bool) string {
	ids := invocationIDs(state, actor, mine)
	if index < 0 || index >= len(ids) {
		return ""
	}
	return ids[index]
}

func (invocationRowSource) Actions(id string, state model.State, actor string) []RowAction {
	invocation, exists := state.Invocations[id]
	if !exists {
		return nil
	}
	var actions []RowAction
	if actor == invocation.Target {
		switch invocation.Status {
		case "PENDING", "NOTIFIED":
			actions = append(actions, invClaim, invReject)
		case "CLAIMED":
			actions = append(actions, invStart, invReject)
		case "RUNNING":
			actions = append(actions, invWait, invComplete)
		case "WAITING":
			actions = append(actions, invResume, invComplete)
		}
	}
	principal := state.Agents[actor]
	elevated := principal.Role == model.RoleOwner || principal.Role == model.RoleOrchestrator
	terminal := invocation.Status == "COMPLETED" || invocation.Status == "REJECTED" ||
		invocation.Status == "EXPIRED" || invocation.Status == "CANCELLED" ||
		invocation.Status == "DEAD_LETTER"
	if !terminal && (actor == invocation.RequestedBy || elevated) {
		actions = append(actions, invCancel)
	}
	return actions
}
