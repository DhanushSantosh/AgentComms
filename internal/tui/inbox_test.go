package tui

import (
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func enterInboxView(t *testing.T, m Model) Model {
	t.Helper()
	m = pressKey(t, m, keyText("j"))
	m = pressKey(t, m, keyText("j"))
	m = pressKey(t, m, keyText("j"))
	m = pressKey(t, m, keyEnter())
	if !m.rowFocus {
		t.Fatal("expected row focus after entering Inbox")
	}
	return m
}

func TestAckThenCompleteActionMessage(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	if _, e := s.Execute("owner", "message.post", "msg-1", model.MessagePosted{Kind: "ACTION", To: []string{"builder"}, Subject: "Run tests", Body: "Attach results"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "builder")
	if e != nil {
		t.Fatal(e)
	}
	m = enterInboxView(t, m)
	if id := m.messageList.SelectedID(m.state, m.actor); id != "msg-1" {
		t.Fatalf("selected id = %q, want msg-1", id)
	}
	m = pressKey(t, m, keyText("a"))
	if m.err != nil {
		t.Fatalf("ack failed: %v", m.err)
	}
	m = pressKey(t, m, keyText("p"))
	if m.err != nil {
		t.Fatalf("complete failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range st.Messages["msg-1"].Recipients {
		if r.Principal == "builder" && r.Status != "COMPLETED" {
			t.Fatalf("status = %q, want COMPLETED", r.Status)
		}
	}
}

func TestAckThenResolveBlocker(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")
	if _, e := s.Execute("owner", "message.post", "msg-2", model.MessagePosted{Kind: "BLOCKER", To: []string{"builder"}, Subject: "CI is down"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "builder")
	if e != nil {
		t.Fatal(e)
	}
	m = enterInboxView(t, m)
	m = pressKey(t, m, keyText("a"))
	if m.err != nil {
		t.Fatalf("ack failed: %v", m.err)
	}
	m = pressKey(t, m, keyText("v"))
	if m.err != nil {
		t.Fatalf("resolve failed: %v", m.err)
	}

	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range st.Messages["msg-2"].Recipients {
		if r.Principal == "builder" && r.Status != "RESOLVED" {
			t.Fatalf("status = %q, want RESOLVED", r.Status)
		}
	}
}

// TestOwnerSeesEveryMessageByRealOwnerID is a regression test for a bug
// where messageRowSource compared the viewing actor against the literal
// string "owner" instead of the project's real owner ID. Every other test
// in this package happens to use "owner" as the owner's actual ID (see
// testsupport.StartPersonalProject), which is exactly what let the bug
// hide — this test deliberately uses a different owner ID.
func TestOwnerSeesEveryMessageByRealOwnerID(t *testing.T) {
	state := model.State{
		Messages: map[string]model.Message{
			"msg-1": {Kind: "ACTION", From: "builder", Subject: "narrow", To: []string{"builder"}},
		},
	}
	source := messageRowSource{owner: "Dhanush"}
	if ids := source.filteredIDs(state, "Dhanush"); len(ids) != 1 {
		t.Fatalf("real owner should see every message regardless of To; got %v", ids)
	}
	if ids := source.filteredIDs(state, "owner"); len(ids) != 0 {
		t.Fatalf("an unrelated actor literally named %q should not get blanket visibility; got %v", "owner", ids)
	}
	if ids := source.filteredIDs(state, "someone-else"); len(ids) != 0 {
		t.Fatalf("a non-recipient, non-owner actor should see nothing; got %v", ids)
	}
}

func TestContractPostRequiresConfirm(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterInboxView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "message.post" || len(m.inputs) != 6 {
		t.Fatalf("expected message.post form with 6 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("contract-1")
	m.inputs[1].SetValue("CONTRACT")
	m.inputs[2].SetValue("builder")
	m.inputs[3].SetValue("API contract")
	m.inputs[4].SetValue("Body text")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" {
		t.Fatalf("form should have closed into a confirm step, still open with err=%v", m.err)
	}
	if m.confirm == nil {
		t.Fatal("expected a confirm step before publishing a CONTRACT message")
	}

	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("contract post failed: %v", m.err)
	}
	st, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if st.Messages["contract-1"].Kind != "CONTRACT" {
		t.Fatalf("message not created: %+v", st.Messages["contract-1"])
	}
}
