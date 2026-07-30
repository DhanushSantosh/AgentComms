package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func TestProjectControlResponsiveViews(t *testing.T) {
	s := newTestService(t)
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}} {
		v, e := RenderForTest(s, "owner", size[0], size[1])
		if e != nil {
			t.Fatal(e)
		}
		for _, want := range []string{"AGENT COMMS", "PROJECT CONTROL", "AGENT WORKFORCE", "ATTENTION", "LIVE ACTIVITY"} {
			if !strings.Contains(v, want) {
				t.Errorf("%dx%d missing %q", size[0], size[1], want)
			}
		}
	}
}

func TestProjectControlDirectNavigation(t *testing.T) {
	instance := newTestService(t)
	view, err := New(instance, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := view.Update(tea.KeyPressMsg(tea.Key{Code: 'g'}))
	view = next.(Model)
	if views[view.view] != "Agents" {
		t.Fatalf("g opened %q, want Agents", views[view.view])
	}
	next, _ = view.Update(tea.KeyPressMsg(tea.Key{Code: 'i'}))
	view = next.(Model)
	if views[view.view] != "Invocations" {
		t.Fatalf("i opened %q, want Invocations", views[view.view])
	}
}

func TestCyclePickerOption(t *testing.T) {
	options := []string{"AGENT", "OBSERVER", "ORCHESTRATOR", "OWNER"}
	if got := cyclePickerOption(options, "AGENT", false); got != "OBSERVER" {
		t.Fatalf("forward from AGENT = %q, want OBSERVER", got)
	}
	if got := cyclePickerOption(options, "AGENT", true); got != "OWNER" {
		t.Fatalf("backward from AGENT (wrap) = %q, want OWNER", got)
	}
	if got := cyclePickerOption(options, "OWNER", false); got != "AGENT" {
		t.Fatalf("forward from OWNER (wrap) = %q, want AGENT", got)
	}
	if got := cyclePickerOption(options, "not-a-real-value", false); got != "OBSERVER" {
		t.Fatalf("unknown current value should fall back to options[0] then advance, got %q", got)
	}
}

// TestPickerFieldCyclesWithArrowKeysAndRejectsTypedText exercises the
// picker end to end through a real form: left/right must change the
// selected value, and typed characters must be silently ignored rather
// than corrupting it -- this is what makes an enum field impossible to
// mistype, the actual point of the picker.
func TestPickerFieldCyclesWithArrowKeysAndRejectsTypedText(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Register("builder", "builder", model.PrincipalAgent); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("a")) // activate
	if m.form != "agent.activate" {
		t.Fatalf("expected agent.activate form, got %q", m.form)
	}
	if got := m.inputs[0].Value(); got != "AGENT" {
		t.Fatalf("role should default to Options[0]=AGENT, got %q", got)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if got := m.inputs[0].Value(); got != "OBSERVER" {
		t.Fatalf("right should cycle AGENT -> OBSERVER, got %q", got)
	}
	// A typed character must not reach the field at all.
	m = pressKey(t, m, keyText("z"))
	if got := m.inputs[0].Value(); got != "OBSERVER" {
		t.Fatalf("typed text should be ignored on a picker field, got %q", got)
	}
	m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if got := m.inputs[0].Value(); got != "AGENT" {
		t.Fatalf("left should cycle back OBSERVER -> AGENT, got %q", got)
	}
}

func TestGuidedTaskFormUsesGovernedService(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.openTaskForm()
	m = next.(Model)
	for i, value := range []string{"task-ui", "Created in TUI", "feature/ui", "src/ui,tests/ui"} {
		m.inputs[i].SetValue(value)
	}
	m.formFocus = len(m.inputs) - 1
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if m.form != "" {
		t.Fatalf("form stayed open: %v", m.err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks["task-ui"].Title != "Created in TUI" {
		t.Fatal("guided write did not reach service")
	}
}
