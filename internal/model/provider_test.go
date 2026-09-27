package model

import "testing"

func TestProviderOfAcceptsTheGrammarAndRejectsEverythingElse(t *testing.T) {
	valid := map[string]string{
		"claude":         "claude",
		"codex":          "codex",
		"opencode":       "opencode",
		"claude-main":    "claude",
		"codex-reviewer": "codex",
		"opencode-2":     "opencode",
		"claude-a-b-c":   "claude",
	}
	for id, wantProvider := range valid {
		got, ok := ProviderOf(id)
		if !ok || got != wantProvider {
			t.Errorf("ProviderOf(%q) = (%q, %v), want (%q, true)", id, got, ok, wantProvider)
		}
	}
	// A bare role name is the mistake this exists to catch; the rest are
	// shapes that would read as a different identity to someone scanning a
	// table, or that are not a provider at all.
	for _, id := range []string{
		"", "reviewer", "builder", "Claude", "Claude-Main", "claude_main",
		"claude-", "claude-Main", "-claude", "gpt-4", "claudex",
	} {
		if provider, ok := ProviderOf(id); ok {
			t.Errorf("ProviderOf(%q) accepted as provider %q, want rejected", id, provider)
		}
	}
}

func TestValidateAgentActorIDExplainsTheFixRatherThanTheRule(t *testing.T) {
	if err := ValidateAgentActorID("claude-main"); err != nil {
		t.Fatalf("a conforming ID must pass: %v", err)
	}
	err := ValidateAgentActorID("reviewer")
	if err == nil {
		t.Fatal("a bare role name must be rejected")
	}
	// The common mistake is a role name; the message should show the exact
	// replacement, not just restate the grammar.
	if got := err.Error(); !contains(got, "claude-reviewer") {
		t.Errorf("error should suggest the fixed ID, got: %s", got)
	}
	// Still rejected -- the grammar is lower case -- but the suggestion
	// must be the normalized ID, not "claude-claude-main".
	upper := ValidateAgentActorID("Claude-Main")
	if upper == nil {
		t.Fatal("Claude-Main is not lower case and must be rejected")
	}
	if !contains(upper.Error(), `try "claude-main"`) {
		t.Errorf("should suggest the normalized form, got: %v", upper)
	}
}

// A suggestion the caller cannot use is worse than none: it sends them to a
// second identical failure. Every suggestion this error makes must itself
// pass validation.
func TestSuggestedReplacementsAreThemselvesValid(t *testing.T) {
	for _, bad := range []string{
		"reviewer", "Reviewer", "builder", "  spaced  ", "UPPER", "agent-00",
		"foo_bar", "x..y", "BAD ID", "claude-", "claude-BAD", "claude_main", "-lead", "",
	} {
		err := ValidateAgentActorID(bad)
		if err == nil {
			continue // legitimately valid; nothing to suggest
		}
		suggestion, ok := suggestedActorID(bad)
		if !ok {
			// No suggestion offered -- the message must then not pretend to
			// have one, which the "use <provider>-<suffix>" wording handles.
			continue
		}
		if vErr := ValidateAgentActorID(suggestion); vErr != nil {
			t.Errorf("input %q was told to try %q, which is itself invalid: %v", bad, suggestion, vErr)
		}
	}
}

// Specifically the cases that used to produce unusable advice.
func TestPreviouslyMisleadingSuggestionsAreGone(t *testing.T) {
	for _, bad := range []string{"foo_bar", "x..y", "BAD ID", "claude-BAD"} {
		err := ValidateAgentActorID(bad)
		if err == nil {
			t.Fatalf("%q should be rejected", bad)
		}
		if contains(err.Error(), "try \"") {
			suggestion, _ := suggestedActorID(bad)
			if vErr := ValidateAgentActorID(suggestion); vErr != nil {
				t.Errorf("%q still offers the invalid suggestion %q", bad, suggestion)
			}
		}
	}
}

func TestDefaultAgentActorIDNumbersOnlyAfterTheBareNameIsTaken(t *testing.T) {
	existing := map[string]bool{}
	taken := func(id string) bool { return existing[id] }

	if got := DefaultAgentActorID("claude", taken); got != "claude" {
		t.Fatalf("first agent should be the bare provider name, got %q", got)
	}
	existing["claude"] = true
	if got := DefaultAgentActorID("claude", taken); got != "claude-2" {
		t.Fatalf("second should be claude-2, got %q", got)
	}
	existing["claude-2"] = true
	if got := DefaultAgentActorID("claude", taken); got != "claude-3" {
		t.Fatalf("third should be claude-3, got %q", got)
	}
	// A differently-named agent of the same provider must not push the
	// counter along -- numbering fills gaps rather than tracking a total.
	existing["claude-reviewer"] = true
	delete(existing, "claude-2")
	if got := DefaultAgentActorID("claude", taken); got != "claude-2" {
		t.Fatalf("numbering should reuse the freed slot, got %q", got)
	}
}

func TestRegisterProviderExtendsTheAcceptedSet(t *testing.T) {
	t.Cleanup(func() { delete(extraProviders, "housecat") })
	if _, ok := ProviderOf("housecat-main"); ok {
		t.Fatal("an unregistered provider must not be accepted")
	}
	RegisterProvider("  HouseCat  ")
	if !IsKnownProvider("housecat") {
		t.Fatal("RegisterProvider should lower-case and trim")
	}
	if provider, ok := ProviderOf("housecat-main"); !ok || provider != "housecat" {
		t.Fatalf("declarative provider should be accepted, got (%q, %v)", provider, ok)
	}
	RegisterProvider("   ")
	for _, name := range KnownProviders() {
		if name == "" {
			t.Fatal("a blank provider must never enter the set")
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 ||
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}
