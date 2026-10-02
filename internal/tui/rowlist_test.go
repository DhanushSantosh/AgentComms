package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestRowListShowsBoundedScrollPosition(t *testing.T) {
	messages := map[string]model.Message{}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("msg-%02d", i)
		// Posted in ID order, so newest-first puts msg-11 at the top and
		// msg-00 in the last window.
		messages[id] = model.Message{ID: id, Kind: "FYI", To: []string{"reviewer"}, Subject: id, CreatedSequence: uint64(i + 1)}
	}
	state := model.State{Messages: messages}
	list := newRowList(messageRowSource{})
	list.Refresh(state, "reviewer")
	list.SetDimensions(60, visibleRowCount(5))
	first := list.View(colors(), state, "reviewer", 60, 5)
	if !strings.Contains(first, "↕ 1–3/12") || lipgloss.Height(first) > 5 {
		t.Fatalf("list should show the first bounded window and position:\n%s", first)
	}
	list.SetCursor(11, 12)
	last := list.View(colors(), state, "reviewer", 60, 5)
	if !strings.Contains(last, "↕ 10–12/12") || !strings.Contains(last, "msg-00") {
		t.Fatalf("list should reach the last bounded window:\n%s", last)
	}
}

func TestRowListKeepsSelectedEntityWhenRecentActivityReordersRows(t *testing.T) {
	state := model.State{Tasks: map[string]model.Task{
		"older": {ID: "older", Status: "OPEN", EntityClock: model.EntityClock{UpdatedSequence: 1}},
		"newer": {ID: "newer", Status: "OPEN", EntityClock: model.EntityClock{UpdatedSequence: 2}},
	}}
	list := newRowList(taskRowSource{})
	list.Refresh(state, "owner")
	list.SetCursor(1, 2)
	if got := list.SelectedID(state, "owner"); got != "older" {
		t.Fatalf("selected %q before update, want older", got)
	}
	task := state.Tasks["older"]
	task.UpdatedSequence = 3
	state.Tasks["older"] = task
	list.Refresh(state, "owner")
	if got := list.SelectedID(state, "owner"); got != "older" {
		t.Fatalf("refresh changed selection to %q after reordering", got)
	}
	if got := list.Cursor(); got != 0 {
		t.Fatalf("selected entity moved to row %d, want row 0", got)
	}
}

func labels(acts []RowAction) []string {
	if len(acts) == 0 {
		return nil
	}
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Label
	}
	return out
}

func TestTaskActionsForStates(t *testing.T) {
	cases := []struct {
		name  string
		task  model.Task
		actor string
		role  model.Role
		want  []string
	}{
		{"open unowned agent can claim", model.Task{Status: "OPEN"}, "claude-builder", model.Role("MEMBER"), []string{"claim"}},
		{"offered unowned agent can claim", model.Task{Status: "OFFERED"}, "claude-builder", model.Role("MEMBER"), []string{"claim"}},
		{"open unowned custom-role agent can claim", model.Task{Status: "OPEN"}, "watcher", model.Role("Tester"), []string{"claim"}},
		{"claimed owned by actor", model.Task{Status: "CLAIMED", Owner: "claude-builder"}, "claude-builder", model.Role("MEMBER"), []string{"start", "renew", "handoff", "cancel"}},
		{"in_progress owned routine", model.Task{Status: "IN_PROGRESS", Owner: "claude-builder", Risk: "ROUTINE"}, "claude-builder", model.Role("MEMBER"), []string{"renew", "block", "review", "handoff", "cancel", "complete"}},
		{"in_progress owned elevated risk", model.Task{Status: "IN_PROGRESS", Owner: "claude-builder", Risk: "HIGH"}, "claude-builder", model.Role("MEMBER"), []string{"renew", "block", "review", "handoff", "cancel"}},
		{"blocked owned", model.Task{Status: "BLOCKED", Owner: "claude-builder"}, "claude-builder", model.Role("MEMBER"), []string{"start", "renew", "cancel"}},
		{"review owned routine risk no complete", model.Task{Status: "REVIEW", Owner: "claude-builder", Risk: "ROUTINE"}, "claude-builder", model.Role("MEMBER"), []string{"renew", "cancel"}},
		{"review owned elevated risk non-elevated role", model.Task{Status: "REVIEW", Owner: "claude-builder", Risk: "HIGH"}, "claude-builder", model.Role("MEMBER"), []string{"renew", "cancel"}},
		{"review owned elevated risk owner role", model.Task{Status: "REVIEW", Owner: "claude-builder", Risk: "HIGH"}, "claude-builder", model.RoleOwner, []string{"renew", "cancel", "complete"}},
		{"completed terminal", model.Task{Status: "COMPLETED", Owner: "claude-builder"}, "claude-builder", model.Role("MEMBER"), nil},
		{"cancelled terminal", model.Task{Status: "CANCELLED", Owner: "claude-builder"}, "claude-builder", model.Role("MEMBER"), nil},
		{"handoff target sees accept plus takeover since it is not the owner", model.Task{Status: "IN_PROGRESS", Owner: "claude-builder", HandoffTo: "claude-reviewer", Risk: "ROUTINE"}, "claude-reviewer", model.Role("MEMBER"), []string{"accept handoff", "takeover"}},
		{"non-owner non-elevated sees only takeover", model.Task{Status: "IN_PROGRESS", Owner: "claude-builder", Risk: "ROUTINE"}, "other", model.Role("MEMBER"), []string{"takeover"}},
		{"non-owner elevated gets generic actions plus takeover", model.Task{Status: "IN_PROGRESS", Owner: "claude-builder", Risk: "ROUTINE"}, "orchestrator", model.RoleOrchestrator, []string{"block", "review", "cancel", "complete", "takeover"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := labels(taskActionsFor(c.task, c.actor, c.role))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
