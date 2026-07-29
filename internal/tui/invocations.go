package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/google/uuid"
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
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Consumer (INTERACTIVE_ONLY/WORKER_ONLY/EITHER)", Placeholder: ""},
		{Label: "Preferred runtime ID", Placeholder: ""},
	},
	Build: func(values []string) (any, error) {
		priority := strings.ToUpper(values[4])
		if priority == "" {
			priority = "NORMAL"
		}
		return model.InvocationRequested{
			Target: values[1], Instruction: values[2], ExpectedResult: values[3],
			Priority: priority, TaskID: values[5], MessageID: values[6], Scopes: splitCSV(values[7]),
			ConsumerMode:       model.ConsumerMode(strings.ToUpper(values[8])),
			PreferredRuntimeID: values[9],
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
	invClaim     = RowAction{Key: "c", Label: "claim", EventType: "invocation.claim", Form: invocationClaimForm}
	invStart     = RowAction{Key: "s", Label: "start", EventType: "invocation.start", Payload: func() any { return model.InvocationProgress{} }}
	invWait      = RowAction{Key: "w", Label: "wait", EventType: "invocation.wait", Form: invocationWaitForm}
	invResume    = RowAction{Key: "u", Label: "resume", EventType: "invocation.resume", Payload: func() any { return model.InvocationProgress{} }}
	invComplete  = RowAction{Key: "x", Label: "complete", EventType: "invocation.complete", Form: invocationCompleteForm}
	invReject    = RowAction{Key: "j", Label: "reject", EventType: "invocation.reject", Form: invocationReasonForm("Reject invocation", "The target agent must provide a reason.")}
	invCancel    = RowAction{Key: "k", Label: "cancel", EventType: "invocation.cancel", Form: invocationReasonForm("Cancel invocation", "Owners, orchestrators, or the requester may cancel active work.")}
	invRedeliver = RowAction{
		Key: "d", Label: "redeliver",
		Form: &ActionForm{
			Title:  "Redeliver invocation",
			Hint:   "Creates a new audited delivery attempt for one explicit runtime.",
			Fields: []FormField{{Label: "Runtime ID", Placeholder: "reviewer-interactive", Required: true}},
			Dispatch: func(m Model, values []string) (tea.Model, tea.Cmd) {
				invocationID := m.formTaskID
				invocation := m.state.Invocations[invocationID]
				runtimeState, exists := m.state.AgentRuntimes[values[0]]
				if !exists || runtimeState.AgentID != invocation.Target {
					m.err = fmt.Errorf("runtime %s is not owned by %s", values[0], invocation.Target)
					return m, nil
				}
				_, err := m.svc.Execute(m.actor, "invocation.delivery-attempt", invocationID,
					model.InvocationDeliveryAttempted{
						DeliveryID: uuid.NewString(), RuntimeID: runtimeState.ID,
						Transport: runtimeState.Connector, HostID: runtimeState.HostID, Manual: true,
					})
				if err != nil {
					m.err = err
					return m, nil
				}
				m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
				m.notice = "Redelivery attempted for " + invocationID
				m.refresh()
				return m, nil
			},
		},
	}
)

func (m Model) invocationControlBar(p palette, width int) string {
	pending, active, failedDeliveries := 0, 0, 0
	for _, invocation := range m.state.Invocations {
		switch invocation.Status {
		case "PENDING", "NOTIFIED":
			pending++
		case "CLAIMED", "RUNNING", "WAITING":
			active++
		}
	}
	for _, delivery := range m.state.InvocationDeliveries {
		if delivery.Status == "FAILED" || delivery.Status == "EXHAUSTED" {
			failedDeliveries++
		}
	}
	mode := "NAVIGATION · Enter to manage selected invocation"
	color := p.muted
	if m.rowFocus {
		mode = "MANAGE MODE · ↑/↓ select · Esc returns to navigation"
		color = p.cyan
	}
	title := lipgloss.NewStyle().Foreground(color).Bold(true).Render(mode)
	status := lipgloss.NewStyle().Foreground(p.text).Render(
		fmt.Sprintf("Pending %d   Active %d   Failed deliveries %d", pending, active, failedDeliveries),
	)
	actions := lipgloss.NewStyle().Foreground(p.amber).Render(
		"[n] invoke agent   [r] refresh   [Enter] manage selected",
	)
	return lipgloss.NewStyle().Width(width).BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(color).PaddingLeft(1).Render(title + "\n" + status + "\n" + actions)
}

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
	if (invocation.Status == "PENDING" || invocation.Status == "NOTIFIED") &&
		(actor == invocation.RequestedBy || actor == invocation.Target || elevated) {
		actions = append(actions, invRedeliver)
	}
	terminal := invocation.Status == "COMPLETED" || invocation.Status == "REJECTED" ||
		invocation.Status == "EXPIRED" || invocation.Status == "CANCELLED" ||
		invocation.Status == "DEAD_LETTER"
	if !terminal && (actor == invocation.RequestedBy || elevated) {
		actions = append(actions, invCancel)
	}
	return actions
}

func (m Model) invocationDeliveryDetails(p palette, width int) string {
	id := m.invocationList.SelectedID(m.state, m.actor)
	if id == "" {
		return ""
	}
	invocation := m.state.Invocations[id]
	deliveries := make([]model.InvocationDelivery, 0)
	for _, delivery := range m.state.InvocationDeliveries {
		if delivery.InvocationID == id {
			deliveries = append(deliveries, delivery)
		}
	}
	sort.Slice(deliveries, func(left, right int) bool {
		return deliveries[left].Attempt < deliveries[right].Attempt
	})
	rows := []string{fmt.Sprintf(
		"%s  consumer %s  runtime %s",
		id, empty(string(invocation.ConsumerMode), string(model.ConsumerModeEither)),
		empty(invocation.PreferredRuntimeID, "automatic"),
	)}
	if invocation.ClaimedAt != nil {
		rows = append(rows, "Target acknowledged at "+invocation.ClaimedAt.Format(time.RFC3339))
	}
	for _, delivery := range deliveries {
		line := fmt.Sprintf(
			"#%d %s via %s/%s", delivery.Attempt, delivery.Status,
			empty(delivery.RuntimeID, "unknown"), empty(delivery.Transport, "legacy"),
		)
		if delivery.Error != "" {
			line += " · " + truncate(delivery.Error, max(20, width-45))
		}
		rows = append(rows, line)
		for _, evidence := range delivery.Evidence {
			rows = append(rows, "  "+evidence.Stage+"  "+evidence.At.Format(time.RFC3339Nano))
		}
	}
	if len(deliveries) == 0 {
		rows = append(rows, "No delivery attempt recorded; the governed request remains pending.")
	}
	return lipgloss.NewStyle().Foreground(p.muted).MaxWidth(width).
		BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.violet).PaddingLeft(1).Render(strings.Join(rows, "\n"))
}
