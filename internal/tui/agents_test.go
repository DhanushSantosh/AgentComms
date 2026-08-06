package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/protocol"
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

// TestMouseWheelScrollsRowSelection proves wheel events move the row
// cursor the same way LineUp/LineDown keys already do -- table.Model only
// ever reacts to tea.KeyPressMsg internally, so updateRowList has to
// translate tea.MouseWheelMsg itself (rowlist.go).
func TestMouseWheelScrollsRowSelection(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "alpha", model.RoleAgent, "src")
	registerAgent(t, s, "beta", model.RoleAgent, "src")
	registerAgent(t, s, "gamma", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	first := m.agentList.SelectedID(m.state, m.actor)

	m = pressMsg(t, m, wheelDown())
	second := m.agentList.SelectedID(m.state, m.actor)
	if second == first {
		t.Fatalf("expected wheel-down to move selection past %q, got %q", first, second)
	}

	m = pressMsg(t, m, wheelUp())
	back := m.agentList.SelectedID(m.state, m.actor)
	if back != first {
		t.Fatalf("expected wheel-up to return to %q, got %q", first, back)
	}
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// TestMouseClickSelectsRow proves a click at the exact screen position
// rowAtY computes for a given row actually selects that row through the
// real Update() dispatch -- not just that rowAtY's math is internally
// consistent with itself.
func TestMouseClickSelectsRow(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "alpha", model.RoleAgent, "src")
	registerAgent(t, s, "beta", model.RoleAgent, "src")
	registerAgent(t, s, "gamma", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)

	p := colors(m.highContrast)
	wantRow := 2
	wantID := m.agentList.source.RowID(wantRow, m.state, m.actor, m.agentList.mine)
	var targetY int
	found := false
	for y := 0; y < m.height; y++ {
		if row, ok := m.rowAtY(p, y); ok && row == wantRow {
			targetY, found = y, true
			break
		}
	}
	if !found {
		t.Fatal("could not find a screen row resolving to the target row via rowAtY")
	}

	m = pressMsg(t, m, click(m.sidebarWidth()+5, targetY))
	if got := m.agentList.SelectedID(m.state, m.actor); got != wantID {
		t.Fatalf("clicking at the computed row position selected %q, want %q", got, wantID)
	}
}

// TestMouseClickSelectsRowAfterScrolling is the case that actually
// exercises the fix: with more rows than fit on screen and the list
// scrolled down first, a click must resolve to the correct ABSOLUTE row,
// not one relative to the top of the (now scrolled) visible window. This
// only works because updateRowList drives the cursor exclusively through
// SetCursor, never MoveUp/MoveDown, keeping bubbles/table's internal
// YOffset pinned at 0 -- see rowAtY's comment for why that invariant is
// load-bearing here.
func TestMouseClickSelectsRowAfterScrolling(t *testing.T) {
	s := newTestService(t)
	for i := 0; i < 15; i++ {
		registerAgent(t, s, fmt.Sprintf("agent-%02d", i), model.RoleAgent, "src")
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.height = 20 // small enough that all 15 rows can't fit at once
	m = enterAgentsView(t, m)

	for i := 0; i < 8; i++ {
		m = pressMsg(t, m, wheelDown())
	}
	scrolledCursor := m.agentList.SelectedID(m.state, m.actor)
	if scrolledCursor == "" {
		t.Fatal("expected a selection after scrolling")
	}

	p := colors(m.highContrast)
	wantRow := m.agentList.Cursor() // click exactly the current cursor's own row
	var targetY int
	found := false
	for y := 0; y < m.height; y++ {
		if row, ok := m.rowAtY(p, y); ok && row == wantRow {
			targetY, found = y, true
			break
		}
	}
	if !found {
		t.Fatal("could not find a screen row resolving to the cursor's row after scrolling")
	}
	// Move off the target first so the click has to actually do the work.
	m = pressMsg(t, m, wheelUp())
	if m.agentList.SelectedID(m.state, m.actor) == scrolledCursor {
		t.Fatal("expected wheel-up to move off the target row")
	}

	m = pressMsg(t, m, click(m.sidebarWidth()+5, targetY))
	if got := m.agentList.SelectedID(m.state, m.actor); got != scrolledCursor {
		t.Fatalf("click after scrolling selected %q, want %q (row %d)", got, scrolledCursor, wantRow)
	}
}

// TestRowListCursorAlwaysStaysVisibleWhileScrolling guards the property a
// first cut of this feature risked losing: bubbles/table's MoveUp/MoveDown
// keep the cursor on screen via internal YOffset adjustments this package
// can't reproduce from outside, so RowList now owns cursor/topRow directly
// (rowlist.go) instead. This walks the cursor the length of a 30-row list
// one key at a time and asserts it's within [topRow, topRow+height) after
// every single step, not just at the end.
func TestRowListCursorAlwaysStaysVisibleWhileScrolling(t *testing.T) {
	s := newTestService(t)
	for i := 0; i < 30; i++ {
		registerAgent(t, s, fmt.Sprintf("agent-%02d", i), model.RoleAgent, "src")
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.height = 20
	m = enterAgentsView(t, m)

	assertVisible := func(step string) {
		t.Helper()
		list := m.agentList
		if list.Cursor() < list.topRow || list.Cursor() >= list.topRow+list.height {
			t.Fatalf("%s: cursor %d outside visible window [%d, %d)", step, list.Cursor(), list.topRow, list.topRow+list.height)
		}
	}
	assertVisible("initial")
	for i := 0; i < 35; i++ {
		m = pressKey(t, m, keyText("j"))
		assertVisible(fmt.Sprintf("down step %d", i))
	}
	for i := 0; i < 35; i++ {
		m = pressKey(t, m, keyText("k"))
		assertVisible(fmt.Sprintf("up step %d", i))
	}
	if got := m.agentList.Cursor(); got != 0 {
		t.Fatalf("expected cursor back at 0 after walking all the way up, got %d", got)
	}
}

// TestDoubleClickRowTriggersFirstAction proves a double-click on a row
// runs its first available action (activate, for a PENDING agent) -- and
// that a single click, or two clicks far enough apart, does not.
func TestDoubleClickRowTriggersFirstAction(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Register("candidate", "candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)

	p := colors(m.highContrast)
	var targetX, targetY int
	found := false
	for y := 0; y < m.height && !found; y++ {
		for x := m.sidebarWidth(); x < m.width; x++ {
			if row, ok := m.rowAtY(p, y); ok && row == 0 {
				targetX, targetY, found = x, y, true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find the row's clickable position")
	}

	m = pressMsg(t, m, click(targetX, targetY))
	if m.form != "" {
		t.Fatalf("a single click should not open a form, got %q", m.form)
	}

	m = pressMsg(t, m, click(targetX, targetY))
	if m.form != "agent.activate" {
		t.Fatalf("expected double-click to open the activate form (candidate's first action), got form=%q", m.form)
	}
}

// TestDoubleClickRequiresTheSameCellWithinTheWindow guards against
// over-triggering: two clicks on DIFFERENT rows, or the same row far
// enough apart in time, must not be treated as a double-click.
func TestDoubleClickRequiresTheSameCellWithinTheWindow(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Register("candidate", "candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	if m.isDoubleClick(5, 5, time.Now()) {
		t.Fatal("a first-ever click must never be a double-click")
	}
	if m.isDoubleClick(6, 5, time.Now()) {
		t.Fatal("a click on a different cell must not count as a double-click")
	}
	if m.isDoubleClick(6, 5, time.Now().Add(doubleClickWindow+time.Millisecond)) {
		t.Fatal("a click on the same cell past the window must not count as a double-click")
	}
	if !m.isDoubleClick(6, 5, time.Now()) {
		t.Fatal("a click on the same cell within the window should count as a double-click")
	}
}

// TestHubTabClickSwitchesView proves clicking a tab in the current hub's
// tab bar switches to and focuses that view -- the gap reported live right
// after the sidebar-click fix ("not the other tabs").
func TestHubTabClickSwitchesView(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	// Command hub's views are Overview, My work, Blockers, Approvals --
	// start on Overview (the hub's default) and click the "Approvals" tab.
	p := colors(m.highContrast)
	var targetX, targetY int
	found := false
	for y := 0; y < m.height && !found; y++ {
		for x := 0; x < m.width; x++ {
			if view, ok := m.hubTabAt(p, x, y); ok && view == "Approvals" {
				targetX, targetY, found = x, y, true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find the Approvals tab's clickable position")
	}
	m = pressMsg(t, m, click(targetX, targetY))
	if views[m.view] != "Approvals" {
		t.Fatalf("expected clicking the Approvals tab to open it, got %q", views[m.view])
	}
	if !m.rowFocus {
		t.Fatal("expected clicking a tab to enter row focus")
	}
}

// TestSidebarClickOpensAndFocusesHub proves clicking a hub name in the
// sidebar both switches to its first view and enters row-focus, matching
// arrow-navigation-then-Enter's combined effect.
// hubClickPosition finds hubIndex's clickable (x, y) in the CURRENT
// render -- recomputed fresh each call, not cached, since the active
// hub's extra "current view" sub-label line shifts every later hub's
// position by two lines once it becomes active.
func hubClickPosition(t *testing.T, m Model, hubIndex int) (x, y int) {
	t.Helper()
	p := colors(m.highContrast)
	for y := 0; y < m.height; y++ {
		for x := 0; x < m.sidebarWidth(); x++ {
			if hub, ok := m.sidebarHubAt(p, x, y); ok && hub == hubIndex {
				return x, y
			}
		}
	}
	t.Fatalf("could not find hub %d's clickable position", hubIndex)
	return 0, 0
}

// TestSidebarClickSwitchesHubsRepeatedly is the regression test for the
// exact bug reported live: the first sidebar click's own openView+
// focusCurrentView sets rowFocus, and sidebar clicks used to be handled
// only in the plain-navigation-mode switch -- reachable, before this fix,
// only when rowFocus (and form/confirm/settingsFocus) were all unset. That
// made the first click load-bearing and every click after it dead: they'd
// route to updateRowList instead, which has no notion of a sidebar hub and
// silently swallowed them. Clicking three different hubs in sequence here
// must actually switch each time, not get stuck on the first one clicked.
func TestSidebarClickSwitchesHubsRepeatedly(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	hubs := []string{"Command", "Team", "Relay"}
	for _, name := range hubs {
		index := -1
		for i, hub := range navigationHubs {
			if hub.Name == name {
				index = i
			}
		}
		if index < 0 {
			t.Fatalf("expected a %s hub in navigationHubs", name)
		}
		x, y := hubClickPosition(t, m, index)
		m = pressMsg(t, m, click(x, y))
		wantView := navigationHubs[index].Views[0]
		if views[m.view] != wantView {
			t.Fatalf("clicking %s: expected view %q, got %q", name, wantView, views[m.view])
		}
		if !m.rowFocus && wantView != "Overview" {
			t.Fatalf("clicking %s: expected row focus on %q", name, wantView)
		}
	}
}

func TestSidebarClickOpensAndFocusesHub(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "builder", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	p := colors(m.highContrast)
	teamHub := -1
	for i, hub := range navigationHubs {
		if hub.Name == "Team" {
			teamHub = i
		}
	}
	if teamHub < 0 {
		t.Fatal("expected a Team hub in navigationHubs")
	}
	var targetX, targetY int
	found := false
	for y := 0; y < m.height && !found; y++ {
		for x := 0; x < m.sidebarWidth(); x++ {
			if hub, ok := m.sidebarHubAt(p, x, y); ok && hub == teamHub {
				targetX, targetY, found = x, y, true
				break
			}
		}
	}
	if !found {
		t.Fatal("could not find the Team hub's clickable position")
	}

	m = pressMsg(t, m, click(targetX, targetY))
	if views[m.view] != "Agents" {
		t.Fatalf("expected clicking the Team hub to open Agents (its first view), got %q", views[m.view])
	}
	if !m.rowFocus {
		t.Fatal("expected clicking a sidebar hub to enter row focus")
	}
}

// TestFormFieldClickFocusesField proves clicking a field's own line moves
// formFocus to it, mirroring what Tab already does.
func TestFormFieldClickFocusesField(t *testing.T) {
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
	if m.formFocus != 0 {
		t.Fatalf("expected initial focus on field 0, got %d", m.formFocus)
	}

	p := colors(m.highContrast)
	wantField := 2
	var targetY int
	found := false
	for y := 0; y < m.height; y++ {
		if field, ok := m.formFieldAtY(p, y); ok && field == wantField {
			targetY, found = y, true
			break
		}
	}
	if !found {
		t.Fatal("could not find field 2's clickable position")
	}

	m = pressMsg(t, m, click(m.sidebarWidth()+5, targetY))
	if m.formFocus != wantField {
		t.Fatalf("expected clicking field %d's line to focus it, got formFocus=%d", wantField, m.formFocus)
	}
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
	if m.form != "agent.activate" || len(m.inputs) != 4 {
		t.Fatalf("expected agent.activate form with 4 fields, got form=%q inputs=%d", m.form, len(m.inputs))
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

// TestActivateOrchestratorChainsApprovalWhenNoneExists is the sibling of
// TestActivateOrchestratorThroughMaskedPassphraseField for the case where
// nobody has requested or approved the grant yet: activating with role
// ORCHESTRATOR must offer a chained confirm (not fail outright, not
// silently succeed), and accepting it must produce all three real signed
// events -- approval.request, approval.approve, agent.activate -- using
// only the passphrase already typed into the activate form.
func TestActivateOrchestratorChainsApprovalWhenNoneExists(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Register("candidate", "candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ElevateKey("owner", "correct passphrase"); e != nil {
		t.Fatal(e)
	}

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	m = pressKey(t, m, keyText("a"))
	if m.form != "agent.activate" || len(m.inputs) != 4 {
		t.Fatalf("expected agent.activate form with 4 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[0].SetValue("ORCHESTRATOR")
	m.inputs[2].SetValue("src")
	m.inputs[3].SetValue("correct passphrase")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.confirm == nil {
		t.Fatal("expected a chained-approval confirm when no approval exists yet")
	}
	if !m.confirm.chainOrchestratorApproval {
		t.Fatal("expected the confirm to be marked for the orchestrator-approval chain")
	}
	if m.form != "" {
		t.Fatalf("activate form should have closed in favor of the confirm, got %q", m.form)
	}

	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("expected the chain to succeed: %v", m.err)
	}

	state, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to be ORCHESTRATOR, got %+v", state.Agents["candidate"])
	}
	approval, ok := state.Approvals["candidate-orchestrator-approval"]
	if !ok || approval.Status != "APPROVED" || approval.Tier != "HUMAN" {
		t.Fatalf("expected an APPROVED HUMAN-tier approval record, got %+v (found=%v)", approval, ok)
	}
}

// TestActivateOrchestratorThroughMaskedPassphraseField is the TUI-level
// end-to-end proof for the masked-passphrase form field: once owner has a
// registered elevated key, granting ORCHESTRATOR must be completable
// entirely within the TUI's own activate form -- a wrong passphrase fails
// clearly without closing the form, and the correct one signs successfully
// -- with s.PassphrasePrompt deliberately nil throughout the actual grant so
// nothing but the form field itself can be supplying it.
func TestActivateOrchestratorThroughMaskedPassphraseField(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Register("candidate", "candidate", model.PrincipalAgent); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ElevateKey("owner", "correct passphrase"); e != nil {
		t.Fatal(e)
	}

	// Pre-approve the orchestrator grant (a separate, deliberate human step
	// in real usage) using the elevated key directly, so the TUI portion of
	// this test only has to exercise the activate-with-passphrase path.
	approvalID := "candidate-orchestrator-approval"
	s.PassphrasePrompt = func(string) (string, error) { return "correct passphrase", nil }
	if _, e := s.Execute("owner", "approval.request", approvalID, model.ApprovalRequested{
		Tier: "HUMAN", Action: protocol.OrchestratorGrantApprovalAction("candidate"), Reason: "test fixture",
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute("owner", "approval.approve", approvalID, model.ApprovalResponse{}); e != nil {
		t.Fatal(e)
	}
	s.PassphrasePrompt = nil

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)
	if id := m.agentList.SelectedID(m.state, m.actor); id != "candidate" {
		t.Fatalf("selected id = %q, want candidate", id)
	}
	m = pressKey(t, m, keyText("a"))
	if m.form != "agent.activate" || len(m.inputs) != 4 {
		t.Fatalf("expected agent.activate form with 4 fields, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	if mode := m.inputs[3].EchoMode; mode != textinput.EchoPassword {
		t.Fatalf("expected the passphrase field to be masked, got echo mode %v", mode)
	}
	m.inputs[0].SetValue("ORCHESTRATOR")
	m.inputs[2].SetValue("src")
	m.inputs[3].SetValue("wrong passphrase")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.err == nil {
		t.Fatal("expected the orchestrator grant to fail with an incorrect passphrase")
	}
	if m.form != "agent.activate" {
		t.Fatalf("form should stay open after a failed passphrase, got %q", m.form)
	}

	m.inputs[3].SetValue("correct passphrase")
	m = pressKey(t, m, keyEnter())
	if m.err != nil {
		t.Fatalf("expected the orchestrator grant to succeed with the correct passphrase: %v", m.err)
	}
	if m.form != "" {
		t.Fatalf("activate form should have closed, got %q", m.form)
	}

	state, e := s.State()
	if e != nil {
		t.Fatal(e)
	}
	if state.Agents["candidate"].Role != model.RoleOrchestrator {
		t.Fatalf("expected candidate to be ORCHESTRATOR, got %+v", state.Agents["candidate"])
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

// TestBodyPrefixMatchesActualRender is the regression test for a real,
// pre-existing bug found while chasing an inaccurate settings click:
// commandRail and renderHubTabs were sized to the body pane's OUTER width,
// but render inside its Padding(1, 2), whose usable inner width is 4
// columns narrower -- wide enough to make the tabs bar's border-bottom
// line wrap onto an extra line, silently shifting every row below it (the
// header, and everything in the actual view) down by one. Renders a real
// screen and asserts bodyPrefixHeight's line count agrees with where the
// header text is actually found, rather than trusting the two formulas to
// stay in sync by inspection alone.
func TestBodyPrefixMatchesActualRender(t *testing.T) {
	s := newTestService(t)
	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m.width, m.height = 140, 38
	for index, name := range views {
		if name == "Project settings" {
			m.view, m.cursor = index, index
		}
	}
	m.focusCurrentView()

	full := m.View().Content
	lines := strings.Split(full, "\n")
	p := colors(m.highContrast)
	predicted := m.bodyPrefixHeight(p)
	if predicted <= 0 || predicted >= len(lines) {
		t.Fatalf("bodyPrefixHeight returned an out-of-range %d", predicted)
	}
	// The header text ("Project settings", bold) is the last thing
	// bodyPrefixHeight accounts for, two lines above where it says content
	// begins (predicted-1 is the blank line "\n\n" leaves between them) --
	// it must appear there and nowhere else nearby, or the two have
	// drifted again.
	headerLine := predicted - 2
	if !strings.Contains(lines[headerLine], "Project settings") {
		t.Fatalf("expected the header at predicted line %d, got %q (surrounding: %q / %q)",
			headerLine, lines[headerLine], lines[max(0, headerLine-1)], lines[min(len(lines)-1, headerLine+1)])
	}
}

// TestRowTableTopYMatchesActualRender guards rowTableTopY (mouse.go)
// against ever drifting from where the Agents table's own header row
// actually renders -- it once did, when bodyContent still prepended a
// per-view control bar above the row list and the formula miscounted the
// blank line between them. Renders a real Agents screen and asserts
// rowTableTopY's prediction agrees with the table header's real position,
// rather than trusting the formula by inspection alone.
func TestRowTableTopYMatchesActualRender(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "alpha", model.RoleAgent, "src")

	m, e := New(s, "owner")
	if e != nil {
		t.Fatal(e)
	}
	m = enterAgentsView(t, m)

	full := m.View().Content
	lines := strings.Split(full, "\n")
	p := colors(m.highContrast)
	predicted := m.rowTableTopY(p)
	if predicted <= 0 || predicted >= len(lines) {
		t.Fatalf("rowTableTopY returned an out-of-range %d", predicted)
	}
	if !strings.Contains(lines[predicted], "STATE") || !strings.Contains(lines[predicted], "PRINCIPAL") {
		t.Fatalf("expected the table header at predicted line %d, got %q (surrounding: %q / %q)",
			predicted, lines[predicted], lines[max(0, predicted-1)], lines[min(len(lines)-1, predicted+1)])
	}
	if row, ok := m.rowAtY(p, predicted+1); !ok || row != 0 {
		t.Fatalf("expected the line right after the header (y=%d) to resolve to row 0, got row=%d ok=%v", predicted+1, row, ok)
	}
}
