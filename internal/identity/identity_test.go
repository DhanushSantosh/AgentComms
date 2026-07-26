package identity

import "testing"

func TestFindProfileByProjectAndHost(t *testing.T) {
	profiles := map[string]Profile{
		"p1:AXIOM": {Name: "p1:AXIOM", ProjectID: "p1", Actor: "AXIOM", HostLabel: "claude"},
		"p1:DAMON": {Name: "p1:DAMON", ProjectID: "p1", Actor: "DAMON", HostLabel: "codex"},
		"p2:HENRY": {Name: "p2:HENRY", ProjectID: "p2", Actor: "HENRY", HostLabel: "claude"},
	}
	actor, ok := FindProfileByProjectAndHost(profiles, "p1", "claude")
	if !ok || actor != "AXIOM" {
		t.Fatalf("expected AXIOM, got %q ok=%v", actor, ok)
	}
	actor, ok = FindProfileByProjectAndHost(profiles, "p1", "codex")
	if !ok || actor != "DAMON" {
		t.Fatalf("expected DAMON, got %q ok=%v", actor, ok)
	}
	if _, ok = FindProfileByProjectAndHost(profiles, "p1", "opencode"); ok {
		t.Fatal("expected no match for an unregistered host in a known project")
	}
	if _, ok = FindProfileByProjectAndHost(profiles, "p3", "claude"); ok {
		t.Fatal("expected no match for an unknown project")
	}
}

// TestFindProfileByProjectAndHostAmbiguous guards the deliberate design
// choice not to guess: if a host somehow registered two agents in the same
// project (two profiles sharing project+host), resolution must decline
// rather than picking one arbitrarily, so callers fall back to existing
// resolution behavior instead of silently binding to the wrong identity.
func TestFindProfileByProjectAndHostAmbiguous(t *testing.T) {
	profiles := map[string]Profile{
		"p1:AXIOM": {Name: "p1:AXIOM", ProjectID: "p1", Actor: "AXIOM", HostLabel: "claude"},
		"p1:PRISM": {Name: "p1:PRISM", ProjectID: "p1", Actor: "PRISM", HostLabel: "claude"},
	}
	if _, ok := FindProfileByProjectAndHost(profiles, "p1", "claude"); ok {
		t.Fatal("expected ambiguous multi-match to return ok=false")
	}
}

func TestResolveActorPrecedenceAndProjectIsolation(t *testing.T) {
	config := UserConfig{
		ActiveProfile: "other:WRONG",
		Profiles: map[string]Profile{
			"project:AXIOM": {Name: "project:AXIOM", ProjectID: "project", Actor: "AXIOM", HostLabel: "claude"},
			"other:WRONG":   {Name: "other:WRONG", ProjectID: "other", Actor: "WRONG"},
		},
	}
	tests := []struct {
		name    string
		request ActorResolutionRequest
		actor   string
		source  string
	}{
		{
			name: "explicit actor overrides every indirect source",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ExplicitActor: "DAMON",
				ExplicitProfile: "project:AXIOM", EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "DAMON", source: ActorSourceFlag,
		},
		{
			name: "explicit profile overrides environment",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "project:AXIOM",
				EnvironmentActor: "ENV", HostLabel: "claude", UserConfig: config,
			},
			actor: "AXIOM", source: ActorSourceProfileFlag,
		},
		{
			name: "environment overrides host binding",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", EnvironmentActor: "ENV",
				HostLabel: "claude", UserConfig: config,
			},
			actor: "ENV", source: ActorSourceEnvironment,
		},
		{
			name: "host binding resolves within project",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", HostLabel: "claude", UserConfig: config,
			},
			actor: "AXIOM", source: ActorSourceHostBinding,
		},
		{
			name: "cross-project active profile never leaks",
			request: ActorResolutionRequest{
				ProjectID: "project", ProjectOwner: "owner", UserConfig: config,
			},
			actor: "owner", source: ActorSourceProjectOwner,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolution, err := ResolveActor(test.request)
			if err != nil {
				t.Fatal(err)
			}
			if resolution.Actor != test.actor || resolution.Source != test.source {
				t.Fatalf("unexpected resolution: %+v", resolution)
			}
		})
	}
}

func TestResolveActorRejectsAmbiguousHostBinding(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", HostLabel: "claude",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"project:AXIOM": {Name: "project:AXIOM", ProjectID: "project", Actor: "AXIOM", HostLabel: "claude"},
			"project:PRISM": {Name: "project:PRISM", ProjectID: "project", Actor: "PRISM", HostLabel: "claude"},
		}},
	})
	if err == nil {
		t.Fatal("expected ambiguous host binding to fail")
	}
}

func TestResolveActorRejectsProfileFromAnotherProject(t *testing.T) {
	_, err := ResolveActor(ActorResolutionRequest{
		ProjectID: "project", ProjectOwner: "owner", ExplicitProfile: "other:AXIOM",
		UserConfig: UserConfig{Profiles: map[string]Profile{
			"other:AXIOM": {Name: "other:AXIOM", ProjectID: "other", Actor: "AXIOM"},
		}},
	})
	if err == nil {
		t.Fatal("expected cross-project profile selection to fail")
	}
}
