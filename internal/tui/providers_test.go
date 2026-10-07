package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

func openProvidersView(t *testing.T, m Model) Model {
	t.Helper()
	m.openView("Providers")
	m.focusCurrentView()
	if !m.rowFocus {
		t.Fatal("expected row focus in Providers")
	}
	return m
}

func TestProvidersViewRegistersRetiresAndReactivates(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = openProvidersView(t, m)
	if got := strings.Join(providerRowSource{}.IDs(m.state, m.actor, false), ","); got != "claude,codex,opencode" {
		t.Fatalf("rows = %s, want the built-ins", got)
	}
	if actions := (providerRowSource{}).Actions("codex", m.state, m.actor); len(actions) != 0 {
		t.Fatal("built-in providers must offer no actions")
	}

	m = pressKey(t, m, keyText("n"))
	if m.form != "provider.register" || len(m.inputs) != 3 {
		t.Fatalf("expected the register-provider form, got form=%q inputs=%d err=%v", m.form, len(m.inputs), m.err)
	}
	m.inputs[0].SetValue("gemini")
	m.inputs[1].SetValue("Google Gemini CLI")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.form != "" || m.err != nil {
		t.Fatalf("register form: form=%q err=%v", m.form, m.err)
	}
	if got := m.state.Providers["gemini"]; got.Status != model.ProviderStatusActive || got.DisplayName != "Google Gemini CLI" {
		t.Fatalf("gemini = %+v", got)
	}
	if id := m.providerList.SelectedID(m.state, m.actor); id != "gemini" {
		t.Fatalf("selected = %q, want the new provider", id)
	}

	// Retire: the form requires a reason.
	m = pressKey(t, m, keyText("x"))
	if m.form != "provider.retire" {
		t.Fatalf("expected the retire form, got %q (err %v)", m.form, m.err)
	}
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.state.Providers["gemini"].Status != model.ProviderStatusActive {
		t.Fatal("retiring without a reason must not happen")
	}
	m.inputs[0].SetValue("trial ended")
	m = pressKey(t, m, keyEnter())
	if m.form != "" || m.state.Providers["gemini"].Status != model.ProviderStatusRetired {
		t.Fatalf("retire: form=%q err=%v provider=%+v", m.form, m.err, m.state.Providers["gemini"])
	}

	// Reactivate through a confirm.
	m = pressKey(t, m, keyText("a"))
	if m.confirm == nil {
		t.Fatal("reactivate must ask for confirmation")
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil || m.state.Providers["gemini"].Status != model.ProviderStatusActive {
		t.Fatalf("reactivate: err=%v provider=%+v", m.err, m.state.Providers["gemini"])
	}
}

func TestProvidersViewIsReadOnlyWithoutStanding(t *testing.T) {
	s := newTestService(t)
	registerAgent(t, s, "claude-member", model.Role("MEMBER"), "src")
	if _, err := s.Execute("owner", "provider.register", "gemini", model.ProviderRegistered{}); err != nil {
		t.Fatal(err)
	}
	m, err := New(s, "claude-member")
	if err != nil {
		t.Fatal(err)
	}
	m = openProvidersView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "" || m.err == nil || !strings.Contains(m.err.Error(), providerReadOnly) {
		t.Fatalf("an actor without standing must be told why, got form=%q err=%v", m.form, m.err)
	}
	if actions := (providerRowSource{}).Actions("gemini", m.state, m.actor); len(actions) != 0 {
		t.Fatal("an actor without standing must see no row actions")
	}
}

func TestAgentRegisterFormOffersToRegisterAMissingProvider(t *testing.T) {
	s := newTestService(t)
	m, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	m = enterAgentsView(t, m)
	m = pressKey(t, m, keyText("n"))
	if m.form != "agent.register" || len(m.inputs) != 4 {
		t.Fatalf("expected the register form with a provider field, got form=%q inputs=%d", m.form, len(m.inputs))
	}
	m.inputs[2].SetValue("AGENT")
	m.inputs[3].SetValue("aider")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.confirm == nil || !strings.Contains(m.confirm.prompt, `Provider "aider" is not registered`) {
		t.Fatalf("expected the register-provider confirmation, got confirm=%v err=%v", m.confirm, m.err)
	}
	m = pressKey(t, m, keyText("y"))
	if m.err != nil {
		t.Fatalf("confirm: %v", m.err)
	}
	if m.state.Providers["aider"].Status != model.ProviderStatusActive {
		t.Fatal("aider was not registered")
	}
	if m.state.Agents["aider"].Status != "PENDING" {
		t.Fatalf("the agent ID must default to the provider name, agents=%v", m.state.Agents)
	}

	// A registered provider needs no confirmation; the ID moves to -2.
	m = pressKey(t, m, keyText("n"))
	m.inputs[2].SetValue("AGENT")
	m.inputs[3].SetValue("aider")
	m.formFocus = len(m.inputs) - 1
	m = pressKey(t, m, keyEnter())
	if m.confirm != nil || m.err != nil || m.state.Agents["aider-2"].Status != "PENDING" {
		t.Fatalf("second aider agent: confirm=%v err=%v", m.confirm, m.err)
	}
}

func TestAgentInspectorShowsTheProvider(t *testing.T) {
	st := model.EmptyState()
	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusRetired}
	for id, want := range map[string]string{"claude-main": "claude (built-in)", "gemini-main": "gemini (retired)", "reviewer": "—"} {
		if got := agentProviderLabel(st, model.Agent{ID: id, PrincipalType: model.PrincipalAgent}); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
}

func TestProvidersViewStaysInBoundsWithRegisteredProviders(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Execute("owner", "provider.register", "geminiwithalongername", model.ProviderRegistered{
		DisplayName: strings.Repeat("Display ", 8),
	}); err != nil {
		t.Fatal(err)
	}
	base, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{40, 12}, {79, 24}, {80, 24}, {160, 48}} {
		m := base
		m.openView("Providers")
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(Model)
		content := m.View().Content
		if got := lipgloss.Height(content); got > size[1] {
			t.Fatalf("%dx%d renders %d rows", size[0], size[1], got)
		}
		for _, line := range strings.Split(content, "\n") {
			if got := lipgloss.Width(line); got > size[0] {
				t.Fatalf("%dx%d renders %d columns", size[0], size[1], got)
			}
		}
	}
}
