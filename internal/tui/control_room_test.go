package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestControlRoomRendersWorkforceAndOperationalViews(t *testing.T) {
	instance := newTestService(t)
	registerAgent(t, instance, "builder", model.RoleAgent, "src")
	if _, err := instance.Execute("builder", "runtime.register", "runtime-builder",
		model.RuntimeRegistered{AgentID: "builder", Connector: "MANUAL", MaxConcurrent: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := instance.Execute("owner", "invocation.request", "inv-control",
		model.InvocationRequested{Target: "builder", Instruction: "Review control room"}); err != nil {
		t.Fatal(err)
	}
	rendered, err := RenderForTest(instance, "owner", 140, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"AGENT WORKFORCE", "Command", "Work", "Team", "Relay", "Project",
		"inv-control", "Overview", "My work", "LIVE ACTIVITY",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("control room missing %q", expected)
		}
	}
}

func TestInvocationRowActionsFollowStateAndAuthority(t *testing.T) {
	state := model.State{Agents: map[string]model.Agent{
		"builder": {ID: "builder", Role: model.RoleAgent, Status: "ACTIVE"},
		"owner":   {ID: "owner", Role: model.RoleOwner, Status: "ACTIVE"},
	}}
	cases := []struct {
		status string
		actor  string
		want   []string
	}{
		{"PENDING", "builder", []string{"claim", "reject"}},
		{"CLAIMED", "builder", []string{"start", "reject"}},
		{"RUNNING", "builder", []string{"wait", "complete"}},
		{"WAITING", "builder", []string{"resume", "complete"}},
		{"PENDING", "owner", []string{"cancel"}},
		{"COMPLETED", "owner", nil},
	}
	for _, testCase := range cases {
		state.Invocations = map[string]model.Invocation{
			"inv": {
				ID: "inv", Target: "builder", RequestedBy: "owner",
				Status: testCase.status,
			},
		}
		actions := invocationRowSource{}.Actions("inv", state, testCase.actor)
		var got []string
		if len(actions) > 0 {
			got = make([]string, len(actions))
			for index, action := range actions {
				got[index] = action.Label
			}
		}
		if !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("status=%s actor=%s actions=%v, want %v", testCase.status, testCase.actor, got, testCase.want)
		}
	}
}

func TestRuntimeRowActionsExposeDrainResumeAndRevoke(t *testing.T) {
	state := model.State{
		Agents: map[string]model.Agent{
			"owner":   {ID: "owner", Role: model.RoleOwner, Status: "ACTIVE"},
			"builder": {ID: "builder", Role: model.RoleAgent, Status: "ACTIVE"},
		},
		AgentRuntimes: map[string]model.AgentRuntime{
			"runtime": {ID: "runtime", AgentID: "builder", Status: "ONLINE"},
		},
	}
	actions := runtimeRowSource{}.Actions("runtime", state, "owner")
	if got := []string{actions[0].Label, actions[1].Label}; !reflect.DeepEqual(got, []string{"drain", "revoke"}) {
		t.Fatalf("online runtime actions=%v", got)
	}
	runtime := state.AgentRuntimes["runtime"]
	runtime.Status = "DRAINING"
	state.AgentRuntimes["runtime"] = runtime
	actions = runtimeRowSource{}.Actions("runtime", state, "builder")
	if len(actions) != 1 || actions[0].Label != "resume" {
		t.Fatalf("draining runtime actions=%v", actions)
	}
}

func TestControlRoomCreatesInvocationThroughGuidedForm(t *testing.T) {
	instance := newTestService(t)
	registerAgent(t, instance, "builder", model.RoleAgent, "src")
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for index, name := range views {
		if name == "Invocations" {
			view.view, view.cursor = index, index
			break
		}
	}
	view.rowFocus = true
	next, _ := view.openCreateForm()
	view = next.(Model)
	values := []string{"inv-tui", "builder", "Review this layer", "Post findings", "HIGH", "", "", "src"}
	for index, value := range values {
		view.inputs[index].SetValue(value)
	}
	view.formFocus = len(view.inputs) - 1
	view = pressKey(t, view, keyEnter())
	if view.err != nil {
		t.Fatal(view.err)
	}
	state, err := instance.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invocations["inv-tui"].Priority != "HIGH" {
		t.Fatalf("guided invocation was not created: %+v", state.Invocations["inv-tui"])
	}
}
