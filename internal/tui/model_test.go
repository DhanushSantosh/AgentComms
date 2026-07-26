package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
