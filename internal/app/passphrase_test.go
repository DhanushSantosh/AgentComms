package app

import (
	"strings"
	"testing"
)

// TestNonInteractivePassphrasePromptAlwaysRefuses guards the actual fix: mcp
// and tui must never attempt term.ReadPassword at all, regardless of what
// stdin looks like (a real pty an MCP host allocated, or bubbletea's own
// raw-mode stdin) -- unlike promptPassphrase, this must be unconditional,
// not TTY-gated.
func TestNonInteractivePassphrasePromptAlwaysRefuses(t *testing.T) {
	for _, context := range []string{"an MCP connection", "the TUI"} {
		prompt := nonInteractivePassphrasePrompt(context)
		passphrase, err := prompt("owner")
		if err == nil {
			t.Fatalf("expected %s's prompt to refuse unconditionally, got passphrase %q", context, passphrase)
		}
		if !strings.Contains(err.Error(), context) {
			t.Fatalf("expected the error to name %q, got: %v", context, err)
		}
		if !strings.Contains(err.Error(), "owner") {
			t.Fatalf("expected the error to name the actor, got: %v", err)
		}
	}
}
