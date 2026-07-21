package tui

import (
	"reflect"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

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
		{"open unowned agent can claim", model.Task{Status: "OPEN"}, "builder", model.RoleAgent, []string{"claim"}},
		{"offered unowned agent can claim", model.Task{Status: "OFFERED"}, "builder", model.RoleAgent, []string{"claim"}},
		{"open unowned observer cannot claim", model.Task{Status: "OPEN"}, "watcher", model.RoleObserver, nil},
		{"claimed owned by actor", model.Task{Status: "CLAIMED", Owner: "builder"}, "builder", model.RoleAgent, []string{"start", "renew", "handoff", "cancel"}},
		{"in_progress owned routine", model.Task{Status: "IN_PROGRESS", Owner: "builder", Risk: "ROUTINE"}, "builder", model.RoleAgent, []string{"renew", "block", "review", "handoff", "cancel", "complete"}},
		{"in_progress owned elevated risk", model.Task{Status: "IN_PROGRESS", Owner: "builder", Risk: "HIGH"}, "builder", model.RoleAgent, []string{"renew", "block", "review", "handoff", "cancel"}},
		{"blocked owned", model.Task{Status: "BLOCKED", Owner: "builder"}, "builder", model.RoleAgent, []string{"start", "renew", "cancel"}},
		{"review owned routine risk no complete", model.Task{Status: "REVIEW", Owner: "builder", Risk: "ROUTINE"}, "builder", model.RoleAgent, []string{"renew", "cancel"}},
		{"review owned elevated risk non-elevated role", model.Task{Status: "REVIEW", Owner: "builder", Risk: "HIGH"}, "builder", model.RoleAgent, []string{"renew", "cancel"}},
		{"review owned elevated risk owner role", model.Task{Status: "REVIEW", Owner: "builder", Risk: "HIGH"}, "builder", model.RoleOwner, []string{"renew", "cancel", "complete"}},
		{"completed terminal", model.Task{Status: "COMPLETED", Owner: "builder"}, "builder", model.RoleAgent, nil},
		{"cancelled terminal", model.Task{Status: "CANCELLED", Owner: "builder"}, "builder", model.RoleAgent, nil},
		{"handoff target sees accept plus takeover since it is not the owner", model.Task{Status: "IN_PROGRESS", Owner: "builder", HandoffTo: "reviewer", Risk: "ROUTINE"}, "reviewer", model.RoleAgent, []string{"accept handoff", "takeover"}},
		{"non-owner non-elevated sees only takeover", model.Task{Status: "IN_PROGRESS", Owner: "builder", Risk: "ROUTINE"}, "other", model.RoleAgent, []string{"takeover"}},
		{"non-owner elevated gets generic actions plus takeover", model.Task{Status: "IN_PROGRESS", Owner: "builder", Risk: "ROUTINE"}, "orchestrator", model.RoleOrchestrator, []string{"block", "review", "cancel", "complete", "takeover"}},
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
