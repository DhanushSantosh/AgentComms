package tui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

var inboxTestAnsi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestInboxOrdersNewestMessagesRegardlessOfObligation(t *testing.T) {
	state := model.State{Messages: map[string]model.Message{
		"msg-001": {ID: "msg-001", Kind: "FYI", To: []string{"reviewer"}, CreatedSequence: 1},
		"msg-002": {ID: "msg-002", Kind: "ACTION", To: []string{"reviewer"}, CreatedSequence: 2, Recipients: []model.RecipientState{{Principal: "reviewer", Status: "PENDING"}}},
		"msg-003": {ID: "msg-003", Kind: "FYI", To: []string{"reviewer"}, CreatedSequence: 3},
		"msg-004": {ID: "msg-004", Kind: "ACTION", To: []string{"reviewer"}, CreatedSequence: 4, Recipients: []model.RecipientState{{Principal: "reviewer", Status: "PENDING"}}},
	}}
	ids := (messageRowSource{}).filteredIDs(state, "reviewer")
	want := []string{"msg-004", "msg-003", "msg-002", "msg-001"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("inbox order %v, want %v", ids, want)
	}
}

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
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	if _, e := s.Execute("owner", "message.post", "msg-1", model.MessagePosted{Kind: "ACTION", To: []string{"claude-builder"}, Subject: "Run tests", Body: "Attach results"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "claude-builder")
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
		if r.Principal == "claude-builder" && r.Status != "COMPLETED" {
			t.Fatalf("status = %q, want COMPLETED", r.Status)
		}
	}
}

func TestAckThenResolveBlocker(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	if _, e := s.Execute("owner", "message.post", "msg-2", model.MessagePosted{Kind: "BLOCKER", To: []string{"claude-builder"}, Subject: "CI is down"}); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "claude-builder")
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
		if r.Principal == "claude-builder" && r.Status != "RESOLVED" {
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
			"msg-1": {Kind: "ACTION", From: "claude-builder", Subject: "narrow", To: []string{"claude-builder"}},
		},
	}
	source := messageRowSource{owner: "Alex"}
	if ids := source.filteredIDs(state, "Alex"); len(ids) != 1 {
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
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterInboxView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "message.post" || len(m.inputs) != 10 {
		t.Fatalf("expected message.post form with 10 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("contract-1")
	m.inputs[1].SetValue("CONTRACT")
	m.inputs[2].SetValue("claude-builder")
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

func TestContractFormCanRequestBoundApproval(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-builder", model.Role("MEMBER"), "src")
	m, err := New(s, "claude-builder")
	if err != nil {
		t.Fatal(err)
	}
	m = enterInboxView(t, m)
	m = pressKey(t, m, keyText("n"))
	values := map[int]string{0: "contract-bound", 1: "CONTRACT", 2: "owner", 3: "Reviewed terms", 4: "Exact body", 6: "YES", 7: "approval-contract-bound", 8: "review exact contract", 9: "1h"}
	for index, value := range values {
		m.inputs[index].SetValue(value)
	}
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.err != nil {
		t.Fatalf("request approval from contract form: %v", m.err)
	}
	state, err := s.State()
	if err != nil {
		t.Fatal(err)
	}
	approval := state.Approvals["approval-contract-bound"]
	if approval.Subject == "" || approval.SubjectDigest == "" || approval.ExpiresAt == nil || approval.Action != "contract:contract-bound" {
		t.Fatalf("contract form did not create a reviewable bound approval: %+v", approval)
	}
}

// TestRowCellsNeverWrapOntoAnExtraLine is the regression test for a bug
// affecting every RowList-backed view, not just Inbox: renderHeader and
// renderTableRow (rowlist.go) rendered each already-fixed-width cell
// through a Header/Cell style that added its own Padding(0, 1) on top,
// silently making every rendered row 2 columns wider per column than the
// width its own RowSource.Columns(width) assumed -- 8 columns of overflow
// on a 4-column table like Inbox's, at every terminal size, not just
// narrow ones. lipgloss wrapped that overflow onto a spurious extra
// physical line under the row (usually landing inside the last column's
// text, e.g. "DELIVERED"), and for the selected row -- widened further
// still by Selected's own Padding(0, 1) around the whole already-oversized
// row -- the wrapped overflow was blank padding, showing up as a small,
// otherwise-unexplained stray colored block. Confirmed live on both the
// Inbox and Runtimes views. Renders a real Inbox screen across a wide
// range of terminal widths and asserts each message's FROM and its
// delivery state always land on the same physical line -- proof nothing
// wrapped.
func TestRowCellsNeverWrapOntoAnExtraLine(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-henry", model.Role("MEMBER"), "src")
	registerAgent(t, s, "claude-peter", model.Role("MEMBER"), "src")
	if _, e := s.Execute("claude-henry", "message.post", "msg-1", model.MessagePosted{Kind: "FYI", To: []string{"owner"}, Subject: "Hello from HENRY"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute("claude-peter", "message.post", "msg-2", model.MessagePosted{Kind: "FYI", To: []string{"owner"}, Subject: "Greetings from PETER"}); e != nil {
		t.Fatal(e)
	}

	for _, width := range []int{70, 80, 90, 100, 120, 160} {
		m, e := New(s, "owner")
		if e != nil {
			t.Fatal(e)
		}
		m.width, m.height = width, 30
		m = enterInboxView(t, m)
		lines := strings.Split(inboxTestAnsi.ReplaceAllString(m.View().Content, ""), "\n")
		// "DELIV", not the full "DELIVERED": at a narrow enough width the
		// STATE column legitimately truncates with "…" (clampColumnsToWidth),
		// which is the correct, non-wrapping degradation this test exists
		// to confirm still holds -- only a wrap would split it onto its own
		// line entirely.
		// Senders are matched on a prefix for the same reason "DELIV"
		// stands in for "DELIVERED" above: under RFC 0039 an agent ID
		// carries its provider, and at width 70 the FROM column
		// legitimately truncates it. Truncation is the correct
		// degradation; a wrap is the failure this test exists to catch,
		// and a wrapped row would put the sender and the state on
		// different lines regardless of how much of the name survived.
		for _, want := range []string{"claude-hen", "claude-pet"} {
			found := false
			for _, line := range lines {
				if strings.Contains(line, want) && strings.Contains(line, "DELIV") {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("width %d: expected a single line containing both %q and DELIV(ERED), got:\n%s", width, want, strings.Join(lines, "\n"))
			}
		}
	}
}

// TestInspectorStaysOnScreenWhenTheTableIsFull is the regression test for
// the one place the TUI shows a message's body: the inspector used to be
// appended below a row list that had already been given the pane's whole
// height, so on any view with enough rows to fill the screen it rendered
// past the last line the terminal has. [i] looked like a dead key, and
// the body looked like something the TUI simply did not display --
// confirmed live on a 40-message inbox. The table yields the room now.
func TestInspectorStaysOnScreenWhenTheTableIsFull(t *testing.T) {
	s := newTestService(t)
	const body = "The body-preservation path drops trailing whitespace when a message is re-rendered."
	for i := 0; i < 40; i++ {
		id := "msg-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		if _, e := s.Execute("owner", "message.post", id, model.MessagePosted{
			Kind: "FYI", To: []string{"owner"}, Subject: "Subject " + id, Body: body,
		}); e != nil {
			t.Fatal(e)
		}
	}
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.width, m.height = 120, 30
	m = enterInboxView(t, m)

	before := m.View().Content
	if strings.Contains(before, "INSPECTOR") {
		t.Fatal("the inspector should be closed to begin with")
	}
	m = pressKey(t, m, keyText("i"))
	if !m.inspecting {
		t.Fatal("expected [i] to open the inspector")
	}
	after := m.View().Content
	if !strings.Contains(after, "INSPECTOR") {
		t.Fatal("[i] did nothing visible: the inspector rendered past the bottom of the screen")
	}
	// A fragment, not the whole body: the inspector wraps it across
	// lines, so the full string is never contiguous on screen.
	if !strings.Contains(inboxTestAnsi.ReplaceAllString(after, ""), "body-preservation path") {
		t.Error("the inspector must show the message body")
	}
	// It cost the table rows rather than the terminal its last lines.
	if got := lipgloss.Height(after); got > m.height {
		t.Errorf("the screen renders %d lines into a %d-line terminal", got, m.height)
	}
	if rows := strings.Count(after, "Subject msg-"); rows == 0 {
		t.Error("the table should still show rows alongside the inspector")
	}
}

func TestLongInspectorScrollsWithoutMovingInboxSelection(t *testing.T) {
	s := newTestService(t)
	body := strings.Repeat("first section\n", 30) + "last section marker"
	if _, err := s.Execute("owner", "message.post", "msg-long-inspector", model.MessagePosted{
		Kind: "FYI", To: []string{"owner"}, Subject: "Long body", Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m.width, m.height = 80, 24
	m = enterInboxView(t, m)
	m = pressKey(t, m, keyText("i"))
	selected := m.messageList.SelectedID(m.state, m.actor)
	if !strings.Contains(m.View().Content, "↕") {
		t.Fatal("long inspector lacks a visible scroll indicator")
	}
	for i := 0; i < 30; i++ {
		m = pressKey(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown, Mod: tea.ModShift}))
	}
	if !strings.Contains(m.View().Content, "last section marker") {
		t.Fatal("last inspector line could not be reached")
	}
	if got := m.messageList.SelectedID(m.state, m.actor); got != selected {
		t.Fatalf("scrolling inspector moved inbox selection from %q to %q", selected, got)
	}
	if got := lipgloss.Height(m.View().Content); got > m.height {
		t.Fatalf("inspector pushed screen to %d lines (height %d)", got, m.height)
	}
}
