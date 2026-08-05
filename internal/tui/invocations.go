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

// consumerAutomatic is the picker sentinel for an empty ConsumerMode --
// leaving the field blank was always valid (it means "use the target's
// invocation policy default"), but a picker field always has some value, so
// this stands in for blank and is translated back to "" before building the
// payload.
const consumerAutomatic = "AUTOMATIC (policy default)"

var invocationRequestForm = &ActionForm{
	Title: "Invoke an agent",
	Hint:  "Creates a durable request. Target policy decides whether approval is required.",
	Fields: []FormField{
		{Label: "Invocation ID", Placeholder: "inv-review-001", Required: true},
		{Label: "Target agent", Placeholder: "reviewer", Required: true},
		{Label: "Instruction", Placeholder: "Review the current implementation", Required: true},
		{Label: "Expected result", Placeholder: "Post a concise review"},
		{Label: "Priority", Options: []string{"NORMAL", "LOW", "HIGH", "URGENT"}},
		{Label: "Related task ID", Placeholder: ""},
		{Label: "Related message ID", Placeholder: ""},
		{Label: "Scopes (comma-separated)", Placeholder: "src"},
		{Label: "Consumer", Options: []string{consumerAutomatic, "INTERACTIVE_ONLY", "WORKER_ONLY", "EITHER"}},
		{Label: "Preferred runtime ID", Placeholder: ""},
	},
	Build: func(values []string) (any, error) {
		priority := strings.ToUpper(values[4])
		if priority == "" {
			priority = "NORMAL"
		}
		consumer := values[8]
		if consumer == consumerAutomatic {
			consumer = ""
		}
		return model.InvocationRequested{
			Target: values[1], Instruction: values[2], ExpectedResult: values[3],
			Priority: priority, TaskID: values[5], MessageID: values[6], Scopes: splitCSV(values[7]),
			ConsumerMode:       model.ConsumerMode(strings.ToUpper(consumer)),
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
			Dispatch: func(m Model, values []string, _ string) (tea.Model, tea.Cmd) {
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
	status, priority, target, requester := 15, 10, 10, 10
	instruction := max(10, width-status-priority-target-requester)
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
			fmtStatus(invocation.Status), invocation.Priority, invocation.Target,
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

// invocationDeliveryDetails renders RFC 0013's delivery pipeline
// (request -> resolve runtime -> delivery-attempt -> transport ->
// notify/failed) as a chip sequence per attempt rather than a raw evidence
// log -- the pipeline is a state machine, this shows it as one. Relative
// timestamps replace the old RFC3339Nano log lines.
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
	rows := []string{lipgloss.NewStyle().Foreground(p.violet).Bold(true).Render("DELIVERY PIPELINE / " + id), fmt.Sprintf(
		"consumer %s  runtime %s",
		empty(string(invocation.ConsumerMode), string(model.ConsumerModeEither)),
		empty(invocation.PreferredRuntimeID, "automatic"),
	), ""}
	for _, delivery := range deliveries {
		rows = append(rows, fmt.Sprintf(
			"Attempt #%d  %s  %s", delivery.Attempt,
			deliveryPipelineChips(p, delivery), relativeTimeOrNow(delivery.AttemptedAt),
		))
		via := fmt.Sprintf("  via %s/%s", empty(delivery.RuntimeID, "unknown"), empty(delivery.Transport, "legacy"))
		if delivery.Error != "" {
			via += " · " + truncate(delivery.Error, max(20, width-len(via)-5))
		}
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render(via))
	}
	if len(deliveries) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(p.muted).Render("No delivery attempt recorded; the governed request remains pending."))
	}
	rows = append(rows, "")
	if invocation.ClaimedAt != nil {
		rows = append(rows, "Target acknowledged "+relativeTimeOrNow(invocation.ClaimedAt))
	}
	if invocation.CompletedAt != nil {
		rows = append(rows, "Completed "+relativeTimeOrNow(invocation.CompletedAt))
	}
	return lipgloss.NewStyle().Foreground(p.text).MaxWidth(width).
		BorderLeft(true).BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(p.violet).PaddingLeft(1).Render(strings.Join(rows, "\n"))
}

// deliveryPipelineChips renders one attempt's progress through RFC 0013's
// five-step delivery state machine as status chips.
func deliveryPipelineChips(p palette, d model.InvocationDelivery) string {
	chips := []string{
		stageChip(p, "request", stageDone),
		stageChip(p, "resolve", stageFor(d.RuntimeID != "")),
		stageChip(p, "attempt", stageDone),
		stageChip(p, "transport", stageFor(len(d.Evidence) > 0)),
	}
	// A delivery's own Status is "SUCCEEDED" on notify -- "NOTIFIED" is the
	// broader invocation's status, a different field entirely (see
	// internal/projection/apply.go's InvocationNotified/
	// InvocationDeliveryFailed cases).
	switch d.Status {
	case "SUCCEEDED":
		chips = append(chips, stageChip(p, "notified", stageDone))
	case "FAILED", "EXHAUSTED":
		chips = append(chips, stageChip(p, "failed", stageFailed))
	default:
		chips = append(chips, stageChip(p, "notify", stagePending))
	}
	return strings.Join(chips, " ")
}

type stageState int

const (
	stagePending stageState = iota
	stageDone
	stageFailed
)

func stageFor(done bool) stageState {
	if done {
		return stageDone
	}
	return stagePending
}

func stageChip(p palette, label string, state stageState) string {
	switch state {
	case stageDone:
		return lipgloss.NewStyle().Foreground(p.cyan).Render("✓" + label)
	case stageFailed:
		return lipgloss.NewStyle().Foreground(p.red).Render("✕" + label)
	default:
		return lipgloss.NewStyle().Foreground(p.muted).Render("○" + label)
	}
}

// relativeTimeOrNow renders t as "Xm ago"-style relative time, or "just
// now" for anything under a minute. Falls back to "unknown" for a nil
// pointer so a caller can pass an *time.Time field straight through.
func relativeTimeOrNow(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
