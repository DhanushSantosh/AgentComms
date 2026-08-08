package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

var taskCreateForm = &ActionForm{
	Title: "Create task",
	Hint:  "Declare the branch and protected write resources before work begins.",
	Fields: []FormField{
		{Label: "Task ID", Placeholder: "task-001", Required: true},
		{Label: "Title", Placeholder: "Implement API", Required: true},
		{Label: "Branch", Placeholder: "feature/api", Required: true},
		{Label: "Resources (comma-separated)", Placeholder: "src/api,tests/api", Required: true},
	},
	Build: func(v []string) (any, error) {
		resources := splitCSV(v[3])
		if len(resources) == 0 {
			return nil, errors.New("at least one resource is required")
		}
		return model.TaskCreated{Title: v[1], Repository: "local", Branch: v[2], Resources: resources, Risk: "ROUTINE"}, nil
	},
	ResolveID: func(_ string, v []string) string { return v[0] },
}
var renewForm = &ActionForm{
	Title: "Renew lease",
	Hint:  "Provide a progress summary before extending the lease.",
	Fields: []FormField{
		{Label: "Progress", Placeholder: "Handlers complete", Required: true},
	},
	Build: func(v []string) (any, error) { return model.TaskRenewed{Progress: v[0]}, nil },
}

func summaryForm(title, hint string) *ActionForm {
	return &ActionForm{
		Title:  title,
		Hint:   hint,
		Fields: []FormField{{Label: "Summary", Placeholder: ""}},
		Build:  func(v []string) (any, error) { return model.TaskStatus{Summary: v[0]}, nil },
	}
}

var (
	blockForm    = summaryForm("Block task", "Explain what is blocking progress.")
	reviewForm   = summaryForm("Send for review", "Summarize what is ready for review.")
	cancelForm   = summaryForm("Cancel task", "Optional reason for cancelling.")
	completeForm = &ActionForm{
		Title: "Complete task",
		Hint:  "Summarize the outcome and list evidence artifacts.",
		Fields: []FormField{
			{Label: "Summary", Placeholder: ""},
			{Label: "Evidence (comma-separated)", Placeholder: ""},
		},
		Build: func(v []string) (any, error) { return model.TaskStatus{Summary: v[0], Evidence: splitCSV(v[1])}, nil },
	}
	handoffForm = &ActionForm{
		Title: "Hand off task",
		Hint:  "Name the next owner and summarize state for them.",
		Fields: []FormField{
			{Label: "Hand off to", Placeholder: "builder-2", Required: true},
			{Label: "Summary", Placeholder: ""},
		},
		Build: func(v []string) (any, error) { return model.TaskHandoff{To: v[0], Summary: v[1]}, nil },
	}
)

var (
	actClaim         = RowAction{Key: "c", Label: "claim", EventType: "task.claim", Payload: func() any { return model.TaskClaimed{} }}
	actStart         = RowAction{Key: "s", Label: "start", EventType: "task.start", Payload: func() any { return model.TaskStatus{} }}
	actRenew         = RowAction{Key: "e", Label: "renew", EventType: "task.renew", Form: renewForm}
	actBlock         = RowAction{Key: "l", Label: "block", EventType: "task.block", Form: blockForm}
	actReview        = RowAction{Key: "v", Label: "review", EventType: "task.review", Form: reviewForm}
	actComplete      = RowAction{Key: "p", Label: "complete", EventType: "task.complete", Form: completeForm}
	actCancel        = RowAction{Key: "x", Label: "cancel", EventType: "task.cancel", Form: cancelForm}
	actHandoff       = RowAction{Key: "o", Label: "handoff", EventType: "task.handoff", Form: handoffForm}
	actHandoffAccept = RowAction{Key: "a", Label: "accept handoff", EventType: "task.handoff.accept", Payload: func() any { return model.TaskStatus{} }}
	actTakeover      = RowAction{
		Key: "t", Label: "takeover", EventType: "task.takeover", Confirm: true,
		Payload: func() any { return model.TaskStatus{} },
		Prompt: func(id string) string {
			return "Take over " + id + "? This requires an existing approved `task.takeover:" + id + "` approval."
		},
		OnError: func(err error, id string) error {
			return fmt.Errorf("%w — request one via `agent-comms approval request --action task.takeover:%s`", err, id)
		},
	}
)

// taskActionsFor mirrors the exact ownership/role/status gates enforced by
// service.Execute (service.go:314-377): renew and handoff require literal
// task ownership with no elevated-role bypass, while start/block/review/
// cancel/complete allow an Owner or Orchestrator to act on a task they don't
// own once it has an owner.
func taskActionsFor(t model.Task, actor string, role model.Role) []RowAction {
	if t.Status == "COMPLETED" || t.Status == "CANCELLED" {
		return nil
	}
	elevated := role == model.RoleOwner || role == model.RoleOrchestrator
	mine := t.Owner != "" && t.Owner == actor
	genericAllowed := mine || elevated
	var acts []RowAction

	if t.Owner == "" && role != model.RoleObserver && (t.Status == "OPEN" || t.Status == "OFFERED") {
		acts = append(acts, actClaim)
	}
	switch t.Status {
	case "CLAIMED":
		if genericAllowed {
			acts = append(acts, actStart)
		}
		if mine {
			acts = append(acts, actRenew, actHandoff)
		}
		if genericAllowed {
			acts = append(acts, actCancel)
		}
	case "IN_PROGRESS":
		if mine {
			acts = append(acts, actRenew)
		}
		if genericAllowed {
			acts = append(acts, actBlock, actReview)
		}
		if mine {
			acts = append(acts, actHandoff)
		}
		if genericAllowed {
			acts = append(acts, actCancel)
		}
		if genericAllowed && (t.Risk == "" || t.Risk == "ROUTINE") {
			acts = append(acts, actComplete)
		}
	case "BLOCKED":
		if genericAllowed {
			acts = append(acts, actStart)
		}
		if mine {
			acts = append(acts, actRenew)
		}
		if genericAllowed {
			acts = append(acts, actCancel)
		}
	case "REVIEW":
		if mine {
			acts = append(acts, actRenew)
		}
		if genericAllowed {
			acts = append(acts, actCancel)
		}
		if elevated && t.Risk != "" && t.Risk != "ROUTINE" {
			acts = append(acts, actComplete)
		}
	}
	if t.HandoffTo == actor {
		acts = append(acts, actHandoffAccept)
	}
	if t.Owner != "" && t.Owner != actor {
		acts = append(acts, actTakeover)
	}
	return acts
}

type taskRowSource struct{}

func (taskRowSource) Columns(width int) []table.Column {
	status, id, owner, lease := 15, 14, 10, 8
	if width < 75 {
		status, id, owner, lease = 11, 10, 8, 6
	}
	res := max(6, width-status-id-owner-lease)
	return []table.Column{
		{Title: "STATUS", Width: status},
		{Title: "TASK", Width: id},
		{Title: "OWNER", Width: owner},
		{Title: "LEASE", Width: lease},
		{Title: "RESOURCES", Width: res},
	}
}
func (taskRowSource) filteredIDs(st model.State, actor string, mine bool) []string {
	ids := make([]string, 0, len(st.Tasks))
	for _, id := range service.SortedKeys(st.Tasks) {
		t := st.Tasks[id]
		if t.Archived || (mine && t.Owner != actor) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
func (s taskRowSource) Rows(st model.State, actor string, mine bool) []table.Row {
	ids := s.filteredIDs(st, actor, mine)
	rows := make([]table.Row, 0, len(ids))
	for _, id := range ids {
		t := st.Tasks[id]
		lease := "—"
		if !t.LeaseUntil.IsZero() {
			lease = time.Until(t.LeaseUntil).Round(time.Minute).String()
		}
		rows = append(rows, table.Row{fmtStatus(t.Status), id, t.Owner, lease, strings.Join(t.Resources, ",")})
	}
	return rows
}
func (s taskRowSource) RowID(idx int, st model.State, actor string, mine bool) string {
	ids := s.filteredIDs(st, actor, mine)
	if idx < 0 || idx >= len(ids) {
		return ""
	}
	return ids[idx]
}
func (taskRowSource) Actions(id string, st model.State, actor string) []RowAction {
	t, ok := st.Tasks[id]
	if !ok {
		return nil
	}
	return taskActionsFor(t, actor, st.Agents[actor].Role)
}
