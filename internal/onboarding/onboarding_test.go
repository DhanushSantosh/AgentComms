package onboarding

import (
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

var forbiddenLiterals = []string{"AXIOM", "DAMON", "PRISM", "builder", "task-001"}

func assertNoForbiddenLiterals(t *testing.T, rendered string) {
	t.Helper()
	for _, forbidden := range forbiddenLiterals {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("rendered onboarding text contains forbidden literal %q:\n%s", forbidden, rendered)
		}
	}
}

func assertSingleDisclaimer(t *testing.T, rendered string) {
	t.Helper()
	if count := strings.Count(rendered, "Never copy an example name out of documentation"); count != 1 {
		t.Errorf("expected exactly one placeholder disclaimer, found %d:\n%s", count, rendered)
	}
}

func TestStaticRenderIsWellFormedAndPlaceholderClean(t *testing.T) {
	rendered, err := Render(StaticData(""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "<no value>") {
		t.Fatalf("static render left an unresolved template action:\n%s", rendered)
	}
	if !strings.Contains(rendered, "agent-comms profile current --json") {
		t.Fatalf("static render did not tell the reader how to discover their live state:\n%s", rendered)
	}
	assertSingleDisclaimer(t, rendered)
	assertNoForbiddenLiterals(t, rendered)
}

func TestSourceBranchesProduceDistinctText(t *testing.T) {
	tests := []struct {
		name       string
		resolution identity.ActorResolution
		registered bool
		active     bool
		role       string
		want       string
	}{
		{
			name:       "unregistered owner fallback",
			resolution: identity.ActorResolution{Actor: "owner", Source: identity.ActorSourceProjectOwner},
			want:       "you are currently resolving as the\nproject owner",
		},
		{
			name:       "registered but not activated",
			resolution: identity.ActorResolution{Actor: "reviewer", Source: identity.ActorSourceHostBinding, HostLabel: "claude"},
			registered: true,
			want:       "You are registered as `reviewer` but not yet activated",
		},
		{
			name:       "active agent",
			resolution: identity.ActorResolution{Actor: "reviewer", Source: identity.ActorSourceHostBinding, HostLabel: "claude"},
			registered: true,
			active:     true,
			role:       "AGENT",
			want:       "You are an active agent: `reviewer`, role `AGENT`",
		},
	}
	seen := map[string]bool{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rendered, err := Render(FromActorResolution(test.resolution, "agent-comms", test.registered, test.active, test.role))
			if err != nil {
				t.Fatal(err)
			}
			normalizedRendered := strings.ReplaceAll(rendered, "\r\n", "\n")
			if strings.Contains(normalizedRendered, "<no value>") {
				t.Fatalf("live render left an unresolved template action:\n%s", rendered)
			}
			if !strings.Contains(normalizedRendered, test.want) {
				t.Fatalf("expected rendered text to contain %q, got:\n%s", test.want, rendered)
			}
			assertSingleDisclaimer(t, rendered)
			assertNoForbiddenLiterals(t, rendered)
			if seen[rendered] {
				t.Fatalf("branch %q produced identical text to a previous branch", test.name)
			}
			seen[rendered] = true
		})
	}
}

// TestActiveOwnerFallbackDoesNotClaimHostBinding guards a real bug found
// while live-testing: identity.ActorResolution.HostLabel is set whenever a
// caller passed AGENT_COMMS_HOST_LABEL, regardless of which Source the
// resolution actually landed on — including the project_owner fallback, the
// project's very first, brand-new connection. The template must not say
// "resolved via host binding" just because HostLabel happens to be
// non-empty; that phrase is only true when Source is actually
// "host_binding".
func TestActiveOwnerFallbackDoesNotClaimHostBinding(t *testing.T) {
	resolution := identity.ActorResolution{Actor: "owner", Source: identity.ActorSourceProjectOwner, HostLabel: "claude"}
	rendered, err := Render(FromActorResolution(resolution, "agent-comms", true, true, "OWNER"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, "resolved via host binding") {
		t.Fatalf("owner-fallback resolution falsely claimed host binding:\n%s", rendered)
	}
}

func TestLookupAgentState(t *testing.T) {
	state := model.State{Agents: map[string]model.Agent{
		"reviewer": {ID: "reviewer", Status: "ACTIVE", Role: model.Role("AGENT")},
		"pending":  {ID: "pending", Status: "REGISTERED"},
	}}
	if registered, active, role := LookupAgentState(state, "reviewer"); !registered || !active || role != "AGENT" {
		t.Fatalf("expected active reviewer, got registered=%v active=%v role=%q", registered, active, role)
	}
	if registered, active, _ := LookupAgentState(state, "pending"); !registered || active {
		t.Fatalf("expected registered-but-inactive, got registered=%v active=%v", registered, active)
	}
	if registered, active, role := LookupAgentState(state, "nobody"); registered || active || role != "" {
		t.Fatalf("expected unregistered zero-value, got registered=%v active=%v role=%q", registered, active, role)
	}
}
