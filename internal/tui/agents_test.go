package tui

import (
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func agentLabels(acts []RowAction) []string {
	if len(acts) == 0 {
		return nil
	}
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Label
	}
	return out
}

func TestAgentActionsForStates(t *testing.T) {
	cases := []struct {
		name  string
		a     model.Agent
		id    string
		actor string
		role  model.Role
		want  []string
	}{
		{"pending non-elevated sees nothing", model.Agent{Status: "PENDING"}, "builder", "watcher", model.RoleAgent, nil},
		{"pending elevated sees activate rename and revoke", model.Agent{Status: "PENDING"}, "builder", "owner", model.RoleOwner, []string{"activate", "rename", "revoke"}},
		{"active elevated sees suspend rename and revoke", model.Agent{Status: "ACTIVE"}, "builder", "owner", model.RoleOwner, []string{"suspend", "rename", "revoke"}},
		{"active non-elevated sees nothing", model.Agent{Status: "ACTIVE"}, "builder", "watcher", model.RoleAgent, nil},
		{"suspended elevated sees rename and revoke", model.Agent{Status: "SUSPENDED"}, "builder", "owner", model.RoleOwner, []string{"rename", "revoke"}},
		{"revoked offers only delete", model.Agent{Status: "REVOKED"}, "builder", "owner", model.RoleOwner, []string{"delete"}},
		{"own row elevated adds rotate key", model.Agent{Status: "ACTIVE"}, "owner", "owner", model.RoleOwner, []string{"suspend", "rename", "revoke", "rotate key"}},
		{"own row non-elevated has no rotate key", model.Agent{Status: "ACTIVE"}, "watcher", "watcher", model.RoleAgent, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agentLabels(agentActionsFor(c.a, c.id, c.actor, c.role))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func enterAgentsView(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 2; i++ {
		m = pressKey(t, m, keyText("j"))
	}
	m = pressKey(t, m, keyEnter())
	if !m.rowFocus {
		t.Fatal("expected row focus after entering Agents")
	}
	return m
}

func TestRegisterThenActivateAgent(t *testing.T) {
	s := newTestService(t)

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "agent.register" || len(m.inputs) != 3 {
		t.Fatalf("expected agent.register form with 3 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("builder")
	m.inputs[2].SetValue("AGENT")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("register form stayed open: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Agents["builder"].Status != "PENDING" {
		t.Fatalf("status = %q, want PENDING", st.Agents["builder"].Status)
	}
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}

	m = pressKey(t, m, keyText("a"))
	if m.form != "agent.activate" || len(m.inputs) != 3 {
		t.Fatalf("expected agent.activate form with 3 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("AGENT")
	m.inputs[2].SetValue("src")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("activate form stayed open: %v", m.err)
	}

	st, e = s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Agents["builder"].Status != "ACTIVE" {
		t.Fatalf("status = %q, want ACTIVE", st.Agents["builder"].Status)
	}
}

func TestSuspendRequiresConfirm(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("s"))
	if m.confirm == nil {
		t.Fatal("suspend should require confirmation")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("suspend failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Agents["builder"].Status != "SUSPENDED" {
		t.Fatalf("status = %q, want SUSPENDED", st.Agents["builder"].Status)
	}
}

func TestRevokeRequiresConfirm(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("x"))
	if m.confirm == nil {
		t.Fatal("revoke should require confirmation")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("revoke failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Agents["builder"].Status != "REVOKED" {
		t.Fatalf("status = %q, want REVOKED", st.Agents["builder"].Status)
	}
}

func TestRotateKeyOnlyOnOwnRow(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	for _, a := range m.agentList.Actions("builder", m.state, m.actor) {
		if a.Label == "rotate key" {
			t.Fatal("rotate key should not appear on another principal's row")
		}
	}

	m = pressKey(t, m, keyText("j"))
	if id := m.agentList.SelectedID(m.state, m.actor); id != "owner" {
		t.Fatalf("selected id = %q, want owner", id)
	}
	m = pressKey(t, m, keyText("z"))
	if m.err != nil {
		t.Fatalf("rotate key failed: %v", m.err)
	}
}

func TestRenameAgent(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("e"))
	if m.form != "agent.rename" {
		t.Fatalf("expected agent.rename form, got %q", m.form)
	}
	m.inputs[0].SetValue("Builder Bot")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.err != nil {
		t.Fatalf("rename failed: %v", m.err)
	}
	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Agents["builder"].DisplayName != "Builder Bot" {
		t.Fatalf("display name = %q, want Builder Bot", st.Agents["builder"].DisplayName)
	}
}

func TestDeleteRequiresRevokedStatusAndReason(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	for _, a := range m.agentList.Actions("builder", m.state, m.actor) {
		if a.Label == "delete" {
			t.Fatal("delete should not be offered before the agent is revoked")
		}
	}
	m = pressKey(t, m, keyText("x"))
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("revoke failed: %v", m.err)
	}

	m.rowFocus = true
	if id := m.agentList.SelectedID(m.state, m.actor); id != "builder" {
		t.Fatalf("selected id = %q, want builder", id)
	}
	m = pressKey(t, m, keyText("d"))
	if m.form != "agent.delete" {
		t.Fatalf("expected agent.delete form, got %q", m.form)
	}
	// Confirm empty reason is rejected before ever reaching Execute.
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "agent.delete" {
		t.Fatalf("empty reason should not have submitted the form, form=%q notice=%q", m.form, m.notice)
	}
	m.inputs[0].SetValue("decommissioned")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.confirm == nil {
		t.Fatal("delete should require confirmation before signing")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("delete failed: %v", m.err)
	}
}

func TestActorSwitchChangesActorAndRejectsUnknown(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = pressKey(t, m, keyText("a"))
	if m.form != "actor.switch" || len(m.inputs) != 1 {
		t.Fatalf("expected actor.switch form with 1 field, got form=%q inputs=%d", m.form, len(m.inputs))
	}

	m.inputs[0].SetValue("nobody")
	m.formFocus = 0
	m = pressKey(t, m, keyEnter())
	if m.err == nil {
		t.Fatal("expected an error switching to an unknown local actor")
	}
	if m.actor != "owner" {
		t.Fatalf("actor changed unexpectedly to %q", m.actor)
	}

	m.inputs[0].SetValue("builder")
	m = pressKey(t, m, keyEnter())
	if m.actor != "builder" {
		t.Fatalf("actor = %q, want builder", m.actor)
	}
}

func TestFileWatchTriggersRefresh(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.EnableFileWatch()
	if m.watcher == nil {
		t.Skip("fsnotify watcher unavailable in this environment")
	}
	t.Cleanup(func() { _ = m.watcher.Close() })

	if _, e := s.Execute("owner", "task.create", "task-1", model.TaskCreated{Title: "t", Repository: "local", Branch: "b", Resources: []string{"src"}, Risk: "ROUTINE"}); e != nil {
		t.Fatal(e)
	}

	cmd := watchEventsCmd(m.watcher)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a filesystem watch event")
	}
	if _, ok := msg.(fsEventMsg); !ok {
		t.Fatalf("expected fsEventMsg, got %#v", msg)
	}

	next, _ := m.Update(msg)
	mm := next.(Model)
	if _, ok := mm.state.Tasks["task-1"]; !ok {
		t.Fatal("state was not refreshed after the file-watch event")
	}
}
