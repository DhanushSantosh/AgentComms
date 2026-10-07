package protocol

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/model"
)

// providerState has an owner, an active orchestrator of each principal
// type, an active developer agent, and a grandfathered pre-RFC 0039 agent.
func providerState() model.State {
	st := model.EmptyState()
	for id, agent := range map[string]model.Agent{
		"owner":            {Status: "ACTIVE", Role: model.RoleOwner, PrincipalType: model.PrincipalHuman},
		"lead":             {Status: "ACTIVE", Role: model.RoleOrchestrator, PrincipalType: model.PrincipalHuman},
		"claude-orchestra": {Status: "ACTIVE", Role: model.RoleOrchestrator, PrincipalType: model.PrincipalAgent},
		"codex-developer":  {Status: "ACTIVE", Role: "DEVELOPER", PrincipalType: model.PrincipalAgent},
		"reviewer":         {Status: "ACTIVE", Role: "DEVELOPER", PrincipalType: model.PrincipalAgent},
		"claude-suspended": {Status: "SUSPENDED", Role: model.RoleOrchestrator, PrincipalType: model.PrincipalAgent},
	} {
		agent.ID = id
		st.Agents[id] = agent
	}
	return st
}

var providerNow = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

func TestProviderRegisterIsForTheOwnerAndActiveOrchestrators(t *testing.T) {
	for actor, allow := range map[string]bool{
		"owner": true, "lead": true, "claude-orchestra": true,
		"codex-developer": false, "claude-suspended": false, "missing": false,
	} {
		_, err := ValidateTransition(providerState(), actor, "provider.register", "gemini", model.ProviderRegistered{}, providerNow)
		if (err == nil) != allow {
			t.Errorf("%s: allow=%v, error=%v", actor, allow, err)
		}
	}
}

func TestProviderRegisterNameRules(t *testing.T) {
	for name, want := range map[string]string{
		"gemini":      "",
		"g2":          "",
		"claude-code": "no hyphens",
		"Gemini":      "lower-case",
		"g":           "2-24",
		"9lives":      "starting with a letter",
		"owner":       "reserved",
		"codex":       "built-in",
		"reviewer":    `collides with existing principal "reviewer"`,
		"lead":        `collides with existing principal "lead"`,
		"claude":      "built-in",
	} {
		_, err := ValidateTransition(providerState(), "owner", "provider.register", name, model.ProviderRegistered{}, providerNow)
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: unexpected error %v", name, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: error %v, want it to mention %q", name, err, want)
		}
	}
}

func TestProviderRegisterRejectsAPrefixCollision(t *testing.T) {
	st := providerState()
	st.Agents["aider-legacy"] = model.Agent{ID: "aider-legacy", Status: "ACTIVE", PrincipalType: model.PrincipalAgent}
	if _, err := ValidateTransition(st, "owner", "provider.register", "aider", model.ProviderRegistered{}, providerNow); err == nil ||
		!strings.Contains(err.Error(), "aider-legacy") {
		t.Fatalf("a name that would reinterpret aider-legacy must be refused, got %v", err)
	}
}

func TestProviderRegisterBoundsItsText(t *testing.T) {
	for _, payload := range []model.ProviderRegistered{
		{DisplayName: strings.Repeat("x", model.MaxProviderDisplayName+1)},
		{Description: strings.Repeat("x", model.MaxProviderDescription+1)},
		{DisplayName: "bad\nname"},
	} {
		if _, err := ValidateTransition(providerState(), "owner", "provider.register", "gemini", payload, providerNow); err == nil {
			t.Errorf("payload %+v must be rejected", payload)
		}
	}
	ok := model.ProviderRegistered{DisplayName: strings.Repeat("x", model.MaxProviderDisplayName), Description: "Google Gemini CLI"}
	if _, err := ValidateTransition(providerState(), "owner", "provider.register", "gemini", ok, providerNow); err != nil {
		t.Fatalf("bounded text must pass: %v", err)
	}
}

func TestProviderRegisterReactivatesOnlyARetiredProvider(t *testing.T) {
	st := providerState()
	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusActive}
	if _, err := ValidateTransition(st, "owner", "provider.register", "gemini", model.ProviderRegistered{}, providerNow); err == nil {
		t.Fatal("registering an ACTIVE provider again must be refused")
	}
	// Its own agents match the prefix; reactivation must not count them as
	// collisions.
	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusRetired}
	st.Agents["gemini-main"] = model.Agent{ID: "gemini-main", Status: "ACTIVE", PrincipalType: model.PrincipalAgent}
	if _, err := ValidateTransition(st, "owner", "provider.register", "gemini", model.ProviderRegistered{}, providerNow); err != nil {
		t.Fatalf("reactivating a RETIRED provider must pass: %v", err)
	}
}

func TestProviderRetire(t *testing.T) {
	st := providerState()
	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusActive}
	st.Providers["aider"] = model.Provider{Name: "aider", Status: model.ProviderStatusRetired}
	for _, tc := range []struct {
		actor, name, reason, want string
	}{
		{"owner", "gemini", "trial ended", ""},
		{"claude-orchestra", "gemini", "trial ended", ""},
		{"codex-developer", "gemini", "trial ended", "owner or orchestrator role required"},
		{"owner", "gemini", "  ", "reason is required"},
		{"owner", "codex", "x", "cannot be retired"},
		{"owner", "kilo", "x", "not registered"},
		{"owner", "aider", "x", "already retired"},
		{"owner", "gemini", strings.Repeat("x", model.MaxProviderRetireReason+1), "at most"},
	} {
		_, err := ValidateTransition(st, tc.actor, "provider.retire", tc.name, model.ProviderRetired{Reason: tc.reason}, providerNow)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%+v: unexpected error %v", tc, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%+v: error %v, want it to mention %q", tc, err, tc.want)
		}
	}
}

func registerAgent(st model.State, id string) error {
	_, err := ValidateTransition(st, "owner", "agent.register", id, model.AgentRegistered{
		PublicKey: "pk", PrincipalType: model.PrincipalAgent,
	}, providerNow)
	return err
}

func TestAgentRegisterUsesTheProjectsProviders(t *testing.T) {
	st := providerState()
	err := registerAgent(st, "gemini-main")
	if !errors.Is(err, model.ErrProviderNotRegistered) || !strings.Contains(err.Error(), "provider add gemini") {
		t.Fatalf("an unregistered provider must say how to register it, got %v", err)
	}
	// A bare role name keeps RFC 0039's suggestion rather than being read
	// as a provider.
	if err = registerAgent(st, "builder"); err == nil || !strings.Contains(err.Error(), `try "claude-builder"`) {
		t.Fatalf("bare names must keep the RFC 0039 suggestion, got %v", err)
	}

	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusActive}
	for _, id := range []string{"gemini", "gemini-main", "gemini-code-reviewer", "claude-main"} {
		if err = registerAgent(st, id); err != nil {
			t.Errorf("%s must be accepted once gemini is registered: %v", id, err)
		}
	}

	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusRetired}
	if err = registerAgent(st, "gemini-2"); !errors.Is(err, model.ErrProviderNotRegistered) || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("a retired provider must refuse new agents and say so, got %v", err)
	}
}

func TestAgentsOfARetiredProviderKeepActing(t *testing.T) {
	st := providerState()
	st.Providers["gemini"] = model.Provider{Name: "gemini", Status: model.ProviderStatusRetired}
	st.Agents["gemini-main"] = model.Agent{ID: "gemini-main", Status: "ACTIVE", Role: "DEVELOPER", PrincipalType: model.PrincipalAgent}
	if _, err := ValidateTransition(st, "gemini-main", "message.post", "msg-1", model.MessagePosted{
		Kind: "FYI", To: []string{"owner"}, Subject: "still here",
	}, providerNow); err != nil {
		t.Fatalf("retiring a provider must not stop its existing agents: %v", err)
	}
}
