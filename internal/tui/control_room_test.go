package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// TestWorkforceSignalIsStableAcrossMultipleRuntimeRecords is the regression
// test for a real bug: an agent can end up with more than one AgentRuntime
// record (a stale, revoked one left behind alongside its current, live
// one -- registering under a new runtime ID never removes an older record
// for the same agent). workforce() used to keep whichever matching record
// its range over the AgentRuntimes map happened to visit last -- and Go
// deliberately randomizes map iteration order on every single range call,
// so the exact same, unchanged state rendered a *different* signal from
// one call to the next: 42 of 50 real runs of this exact scenario showed
// "REVOKED" and the other 8 showed "ONLINE", with no state change between
// them. Builds an agent with an old, revoked runtime and a newer, online
// one and asserts every one of many renders agrees on the most recent
// record (ONLINE), never flickering back to the stale one.
func TestWorkforceSignalIsStableAcrossMultipleRuntimeRecords(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	newer := time.Now()
	m := Model{
		state: model.State{
			Agents: map[string]model.Agent{
				"HENRY": {ID: "HENRY", DisplayName: "HENRY", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
			},
			AgentRuntimes: map[string]model.AgentRuntime{
				"henry-test": {ID: "henry-test", AgentID: "HENRY", Status: "REVOKED", Health: "UNKNOWN", RegisteredAt: older, LastSeenAt: older},
				"HENRY":      {ID: "HENRY", AgentID: "HENRY", Status: "ONLINE", Health: "HEALTHY", RegisteredAt: newer, LastSeenAt: newer},
			},
		},
	}
	p := colors(false)
	for i := 0; i < 50; i++ {
		out := m.workforce(p, 100)
		if !strings.Contains(out, "ONLINE") || strings.Contains(out, "REVOKED") {
			t.Fatalf("iteration %d: expected a stable ONLINE signal for HENRY, got:\n%s", i, out)
		}
	}
}

// TestWorkforceFallsBackToAgentIDWhenDisplayNameIsBlank is the regression
// test for an agent registered without a display name (optional at
// registration) rendering as a blank AGENT column -- indistinguishable
// from missing data -- instead of falling back to its ID, which is always
// present.
func TestWorkforceFallsBackToAgentIDWhenDisplayNameIsBlank(t *testing.T) {
	m := Model{
		state: model.State{
			Agents: map[string]model.Agent{
				"PETER": {ID: "PETER", DisplayName: "", Role: model.RoleAgent, PrincipalType: model.PrincipalAgent},
			},
		},
	}
	out := m.workforce(colors(false), 100)
	if !strings.Contains(out, "PETER") {
		t.Fatalf("expected the blank-display-name agent to fall back to its ID %q, got:\n%s", "PETER", out)
	}
}

// TestAuditHealthSurfacesDoctorFindings confirms Audit & health shows
// exactly what `agent-comms doctor` would report -- previously this panel
// only rendered chain integrity and lifecycle, never doctor's findings, so
// diagnosing a project required leaving the TUI entirely.
func TestAuditHealthSurfacesDoctorFindings(t *testing.T) {
	instance := newTestService(t)
	// "builder" is one of doctor's own TEST_LIKE_RUNTIME triggers
	// (internal/doctor.Findings), so this is a real, already-present finding
	// rather than fabricated state.
	registerAgent(t, instance, "builder", model.RoleAgent, "src")
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Audit & health")
	// Findings are computed lazily, on focus, not eagerly in New() -- see
	// New()'s and refreshSilent's comments on why (doctor.Findings dials
	// every ONLINE interactive runtime's PTY socket, too slow to pay on
	// every TUI launch or background tick).
	view.focusCurrentView()
	rendered := view.View().Content
	for _, expected := range []string{"Doctor findings", "TEST_LIKE_RUNTIME"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("Audit & health missing %q:\n%s", expected, rendered)
		}
	}
}

func TestArrowNavigationMovesBetweenHubsAndTabs(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view = pressKey(t, view, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if got := views[view.view]; got != "My work" {
		t.Fatalf("right arrow selected %q, want My work", got)
	}
	view = pressKey(t, view, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if got := views[view.view]; got != "Tasks" {
		t.Fatalf("down arrow selected %q, want Tasks", got)
	}
	view = pressKey(t, view, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if got := views[view.view]; got != "Documents" {
		t.Fatalf("right arrow selected %q, want Documents", got)
	}
}

func TestAgentControlsAreVisibleBeforeEnteringManageMode(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.openView("Agents")
	rendered := view.View().Content
	for _, expected := range []string{
		"STATE",
		"PRINCIPAL",
		"ROLE",
		"suspend",
		"rename",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("agent workspace missing %q", expected)
		}
	}
}

func TestCommandPaletteShowsInputAndMatchingCommands(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	view.palette = true
	view.query = "agent"
	rendered := view.View().Content
	for _, expected := range []string{"COMMANDS", "Command", "> agent█", "new agent", "Agents"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("command palette missing %q", expected)
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
		{"PENDING", "builder", []string{"claim", "reject", "redeliver"}},
		{"CLAIMED", "builder", []string{"start", "reject"}},
		{"RUNNING", "builder", []string{"wait", "complete"}},
		{"WAITING", "builder", []string{"resume", "complete"}},
		{"PENDING", "owner", []string{"redeliver", "cancel"}},
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
	if len(actions) != 2 || actions[0].Label != "resume" || actions[1].Label != "configure" {
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
