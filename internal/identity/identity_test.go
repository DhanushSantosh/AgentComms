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
