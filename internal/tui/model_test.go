package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/service"
)

func TestSignalRoomResponsiveViews(t *testing.T) {
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	s := service.New(d)
	s.Store.SetCredentialStore(identity.NewMemoryStore())
	if e := s.Store.Init("owner"); e != nil {
		t.Fatal(e)
	}
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}} {
		v, e := RenderForTest(s, "owner", size[0], size[1])
		if e != nil {
			t.Fatal(e)
		}
		for _, want := range []string{"AGENT COMMS", "Overview", "Attention queue", "Event chain"} {
			if !strings.Contains(v, want) {
				t.Errorf("%dx%d missing %q", size[0], size[1], want)
			}
		}
	}
}

func TestGuidedTaskFormUsesGovernedService(t *testing.T) {
	d := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = d
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(b))
	}
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(d, "user"))
	s := service.New(d)
	s.Store.SetCredentialStore(identity.NewMemoryStore())
	if err := s.Store.Init("owner"); err != nil {
		t.Fatal(err)
	}
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
