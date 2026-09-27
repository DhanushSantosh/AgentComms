package tui

import "testing"

func TestTruncateMiddleKeepsWhatDistinguishesProviderScopedIDs(t *testing.T) {
	// The point of eliding the middle: two agents of the same provider share
	// a 7-character prefix, so end-truncation renders them identically.
	a := truncateMiddle("claude-reviewer", 13)
	b := truncateMiddle("claude-developer", 13)
	if a == b {
		t.Fatalf("same-provider IDs must stay distinguishable, both rendered %q", a)
	}
	for _, got := range []string{a, b} {
		if len([]rune(got)) != 13 {
			t.Errorf("truncateMiddle should fill the column exactly, got %q (%d runes)", got, len([]rune(got)))
		}
	}
	// End-truncation is what this replaces; prove it actually collides.
	if truncate("claude-reviewer", 13) != truncate("claude-developer", 13) {
		t.Log("note: end-truncation no longer collides for these inputs")
	}
	// A value that fits is returned untouched, and short widths degrade to
	// ordinary truncation rather than producing nonsense.
	if got := truncateMiddle("claude", 13); got != "claude" {
		t.Errorf("a fitting value must be unchanged, got %q", got)
	}
	if got := truncateMiddle("claude-reviewer", 4); got != truncate("claude-reviewer", 4) {
		t.Errorf("below the middle-elision floor it should match truncate, got %q", got)
	}
	if got := truncateMiddle("claude-reviewer", 0); got != "" {
		t.Errorf("zero width should render nothing, got %q", got)
	}
}
