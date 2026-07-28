package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/DhanushSantosh/AgentComms/internal/testsupport"
)

func newTestService(t *testing.T) *service.Service {
	t.Helper()
	s, _ := testsupport.StartPersonalProject(t)
	return s
}
func registerAgent(t *testing.T, s *service.Service, id string, role model.Role, scopes ...string) {
	t.Helper()
	if _, e := s.Register(id, id, model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if role == model.RoleOrchestrator {
		// ORCHESTRATOR grants now require a separately-approved, HUMAN-tier
		// approval on top of the ordinary elevation/human-principal checks
		// (internal/protocol/transitions.go) — apply and approve it here so
		// existing fixtures asking for an orchestrator still get one.
		approvalID := id + "-orchestrator-approval"
		if _, e := s.Execute("owner", "approval.request", approvalID, model.ApprovalRequested{
			Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction(id), Reason: "test fixture",
		}); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Execute("owner", "agent.activate", id, model.AgentActivated{Role: role, Capabilities: []string{"*"}, Scopes: scopes}); e != nil {
		t.Fatal(e)
	}
}
func createTask(t *testing.T, s *service.Service, id string) {
	t.Helper()
	if _, e := s.Execute("owner", "task.create", id, model.TaskCreated{Title: "Implement API", Repository: "local", Branch: "feature/api", Resources: []string{"src/api"}, Risk: "ROUTINE"}); e != nil {
		t.Fatal(e)
	}
}
func pressKey(t *testing.T, m Model, msg tea.KeyPressMsg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	mm, ok := next.(Model)
	if !ok {
		t.Fatal("Update did not return a tui.Model")
	}
	return mm
}
func keyText(s string) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Text: s, Code: rune(s[0])}) }
func keyEnter() tea.KeyPressMsg        { return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}) }

func enterTasksView(t *testing.T, m Model) Model {
	t.Helper()
	m = pressKey(t, m, keyText("j"))
	m = pressKey(t, m, keyEnter())
	if !m.rowFocus {
		t.Fatal("expected row focus after entering Tasks")
	}
	return m
}

func TestRowSelectionAndClaimEndToEnd(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	createTask(t, s, "task-1")

	m, e := New(s, "builder")
	if e != nil {
		t.Fatal(e)
	}
	m = enterTasksView(t, m)
	if id := m.taskList.SelectedID(m.state, m.actor); id != "task-1" {
		t.Fatalf("selected id = %q, want task-1", id)
	}
	m = pressKey(t, m, keyText("c"))
	if m.confirm != nil {
		t.Fatal("claim should not require confirmation")
	}
	if m.err != nil {
		t.Fatalf("claim failed: %v", m.err)
	}
	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Tasks["task-1"].Owner != "builder" {
		t.Fatalf("owner = %q, want builder", st.Tasks["task-1"].Owner)
	}
}

func TestTakeoverBlockedThenApprovedSucceeds(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	createTask(t, s, "task-1")
	if _, e := s.Execute("builder", "task.claim", "task-1", model.TaskClaimed{}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterTasksView(t, m)

	m = pressKey(t, m, keyText("t"))
	if m.confirm == nil {
		t.Fatal("takeover should require confirmation")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err == nil || !strings.Contains(m.err.Error(), "approval request") {
		t.Fatalf("expected takeover to fail without an approval, got %v", m.err)
	}
	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Tasks["task-1"].Owner != "builder" {
		t.Fatalf("owner changed unexpectedly to %q", st.Tasks["task-1"].Owner)
	}

	if _, e := s.Execute("owner", "approval.request", "approval-1", model.ApprovalRequested{Tier: "ORCHESTRATOR", Action: "task.takeover:task-1", Reason: "test"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute("owner", "approval.approve", "approval-1", model.ApprovalResponse{}); e != nil {
		t.Fatal(e)
	}

	m.err = nil
	m = pressKey(t, m, keyText("t"))
	if m.confirm == nil {
		t.Fatal("takeover should require confirmation again")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("takeover with an approval should succeed: %v", m.err)
	}
	st, e = s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Tasks["task-1"].Owner != "owner" {
		t.Fatalf("owner = %q, want owner", st.Tasks["task-1"].Owner)
	}
}

func TestRenewFormSubmitsProgress(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	createTask(t, s, "task-1")
	if _, e := s.Execute("builder", "task.claim", "task-1", model.TaskClaimed{}); e != nil {
		t.Fatal(e)
	}
	before, e := s.State()
	if e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "builder")
	if e != nil {
		t.Fatal(e)
	}
	m = enterTasksView(t, m)
	m = pressKey(t, m, keyText("e"))
	if m.form != "task.renew" || len(m.inputs) != 1 {
		t.Fatalf("expected renew form with 1 field, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("Handlers complete")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("form stayed open: %v", m.err)
	}

	after, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if !after.Tasks["task-1"].LeaseUntil.After(before.Tasks["task-1"].LeaseUntil) {
		t.Fatal("renew did not extend the lease")
	}
}

func TestHandoffTwoFieldForm(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	createTask(t, s, "task-1")
	if _, e := s.Execute("builder", "task.claim", "task-1", model.TaskClaimed{}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "builder")
	if e != nil {
		t.Fatal(e)
	}
	m = enterTasksView(t, m)
	m = pressKey(t, m, keyText("o"))
	if m.form != "task.handoff" || len(m.inputs) != 2 {
		t.Fatalf("expected handoff form with 2 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("reviewer-2")
	m.inputs[1].SetValue("Ready for handoff")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("form stayed open: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Tasks["task-1"].HandoffTo != "reviewer-2" {
		t.Fatalf("handoff_to = %q, want reviewer-2", st.Tasks["task-1"].HandoffTo)
	}
}
