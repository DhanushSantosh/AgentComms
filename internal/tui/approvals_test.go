package tui

import (
	"reflect"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func approvalLabels(acts []RowAction) []string {
	if len(acts) == 0 {
		return nil
	}
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.Label
	}
	return out
}

func TestApprovalActionsForStates(t *testing.T) {
	cases := []struct {
		name string
		a    model.Approval
		role model.Role
		pt   model.PrincipalType
		want []string
	}{
		{"pending non-elevated role sees nothing", model.Approval{Status: "PENDING", Tier: "ORCHESTRATOR"}, model.Role("MEMBER"), model.PrincipalAgent, nil},
		{"pending orchestrator-tier owner sees both", model.Approval{Status: "PENDING", Tier: "ORCHESTRATOR"}, model.RoleOwner, model.PrincipalAgent, []string{"approve", "reject"}},
		{"pending human-tier agent-principal orchestrator hides approve", model.Approval{Status: "PENDING", Tier: "HUMAN"}, model.RoleOrchestrator, model.PrincipalAgent, []string{"reject"}},
		{"pending human-tier human-principal owner sees both", model.Approval{Status: "PENDING", Tier: "HUMAN"}, model.RoleOwner, model.PrincipalHuman, []string{"approve", "reject"}},
		{"approved is terminal", model.Approval{Status: "APPROVED", Tier: "ORCHESTRATOR"}, model.RoleOwner, model.PrincipalHuman, nil},
		{"rejected is terminal", model.Approval{Status: "REJECTED", Tier: "ORCHESTRATOR"}, model.RoleOwner, model.PrincipalHuman, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := approvalLabels(approvalActionsFor(c.a, c.role, c.pt))
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func enterApprovalsView(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 3; i++ {
		m = pressKey(t, m, keyText("]"))
	}
	m = pressKey(t, m, keyEnter())
	if !m.rowFocus {
		t.Fatal("expected row focus after entering Approvals")
	}
	return m
}

func TestApprovalRequestThenApproveEndToEnd(t *testing.T) {
	s := newTestService(t)

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterApprovalsView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "approval.request" || len(m.inputs) != 8 {
		t.Fatalf("expected approval.request form with 8 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("approval-1")
	m.inputs[1].SetValue("ORCHESTRATOR")
	m.inputs[2].SetValue("task.takeover:task-1")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("form stayed open: %v", m.err)
	}

	if id := m.approvalList.SelectedID(m.state, m.actor); id != "approval-1" {
		t.Fatalf("selected id = %q, want approval-1", id)
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("approve failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Approvals["approval-1"].Status != "APPROVED" {
		t.Fatalf("status = %q, want APPROVED", st.Approvals["approval-1"].Status)
	}
}

func TestApprovalRejectRequiresConfirm(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Execute("owner", "approval.request", "approval-1", model.ApprovalRequested{Tier: "ORCHESTRATOR", Action: "task.takeover:task-1", Reason: "test"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterApprovalsView(t, m)
	m = pressKey(t, m, keyText("x"))
	if m.confirm == nil {
		t.Fatal("reject should require confirmation")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("reject failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Approvals["approval-1"].Status != "REJECTED" {
		t.Fatalf("status = %q, want REJECTED", st.Approvals["approval-1"].Status)
	}
}

func TestHumanTierApprovalHidesApproveForAgentPrincipal(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "auto-orch", model.RoleOrchestrator, "*")
	if _, e := s.Execute("owner", "approval.request", "approval-1", model.ApprovalRequested{Tier: "HUMAN", Action: "release.publish", Reason: "test"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "auto-orch")
	if e != nil {
		t.Fatal(e)
	}
	m.state, e = s.State()
	if e != nil {
		t.Fatal(e)
	}
	got := approvalLabels(m.approvalList.Actions("approval-1", m.state, m.actor))
	want := []string{"reject"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
