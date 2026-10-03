package model

import (
	"errors"
	"testing"
)

func agents() map[string]Agent {
	return map[string]Agent{
		"claude-main":     {ID: "claude-main", DisplayName: "Atlas", Status: "ACTIVE"},
		"codex-main":      {ID: "codex-main", DisplayName: "Beacon", Status: "ACTIVE"},
		"claude-reviewer": {ID: "claude-reviewer", DisplayName: "", Status: "ACTIVE"},
		"alex":            {ID: "alex", DisplayName: "Alex", Status: "ACTIVE"},
	}
}

func TestResolvePrincipalPrefersActorIDOverDisplayName(t *testing.T) {
	for reference, want := range map[string]string{
		"claude-main":     "claude-main",
		"claude-reviewer": "claude-reviewer",
		"Atlas":           "claude-main",
		"atlas":           "claude-main",
		"  Beacon  ":      "codex-main",
		"alex":            "alex",
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
	// Typed, so a caller can tell this apart from "nobody by that name"
	// and act on the candidates -- a caller that can only see an
	// undifferentiated error cannot keep RFC 0039's promise, and one of
	// them (the CLI's own --actor resolution) was silently dropping it.
	var ambiguous *AmbiguousPrincipalError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("ambiguity must be distinguishable by type, got %T", err)
	}
	if ambiguous.Reference != "Atlas" {
		t.Errorf("the error should carry the reference, got %q", ambiguous.Reference)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("the error should carry every candidate, got %v", ambiguous.Candidates)
	}
}

func TestResolvePrincipalRejectsUnknownAndEmpty(t *testing.T) {
	_, err := ResolvePrincipal(agents(), "nobody")
	if err == nil {
		t.Error("an unknown reference must not resolve")
	}
	// Not ambiguity: callers treat the two differently, and a reference
	// matching nobody is usually best left to whatever already reports
	// unknown actors.
	var ambiguous *AmbiguousPrincipalError
	if errors.As(err, &ambiguous) {
		t.Errorf("an unknown reference is not an ambiguous one: %v", err)
	}
	if _, err := ResolvePrincipal(agents(), "   "); err == nil {
		t.Error("a blank reference must not resolve")
	}
}
