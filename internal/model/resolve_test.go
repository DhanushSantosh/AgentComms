package model

import "testing"

func agents() map[string]Agent {
	return map[string]Agent{
		"claude-main":     {ID: "claude-main", DisplayName: "Atlas", Status: "ACTIVE"},
		"codex-main":      {ID: "codex-main", DisplayName: "Beacon", Status: "ACTIVE"},
		"claude-reviewer": {ID: "claude-reviewer", DisplayName: "", Status: "ACTIVE"},
		"dhanush":         {ID: "dhanush", DisplayName: "Dhanush", Status: "ACTIVE"},
	}
}

func TestResolvePrincipalPrefersActorIDOverDisplayName(t *testing.T) {
	for reference, want := range map[string]string{
		"claude-main":     "claude-main",
		"claude-reviewer": "claude-reviewer",
		"Atlas":           "claude-main",
		"atlas":           "claude-main",
		"  Beacon  ":      "codex-main",
		"dhanush":         "dhanush",
	} {
		got, err := ResolvePrincipal(agents(), reference)
		if err != nil || got != want {
			t.Errorf("ResolvePrincipal(%q) = (%q, %v), want %q", reference, got, err, want)
		}
	}
}

// A display name must never shadow someone else's actor ID: the ID is
// unique by construction, the display name is not.
func TestActorIDWinsWhenADisplayNameCollidesWithIt(t *testing.T) {
	set := agents()
	set["codex-main"] = Agent{ID: "codex-main", DisplayName: "claude-main", Status: "ACTIVE"}
	got, err := ResolvePrincipal(set, "claude-main")
	if err != nil || got != "claude-main" {
		t.Fatalf("the real actor ID must win, got (%q, %v)", got, err)
	}
}

// Guessing between two principals would attribute a signed event to one the
// caller never chose, so ambiguity is an error that names the candidates.
func TestAmbiguousDisplayNameIsRefusedAndNamesTheCandidates(t *testing.T) {
	set := agents()
	set["codex-main"] = Agent{ID: "codex-main", DisplayName: "Atlas", Status: "ACTIVE"}
	_, err := ResolvePrincipal(set, "Atlas")
	if err == nil {
		t.Fatal("two principals sharing a display name must not resolve")
	}
	for _, want := range []string{"claude-main", "codex-main", "actor ID"} {
		if !contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestResolvePrincipalRejectsUnknownAndEmpty(t *testing.T) {
	if _, err := ResolvePrincipal(agents(), "nobody"); err == nil {
		t.Error("an unknown reference must not resolve")
	}
	if _, err := ResolvePrincipal(agents(), "   "); err == nil {
		t.Error("a blank reference must not resolve")
	}
}
