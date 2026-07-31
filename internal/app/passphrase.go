package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

// promptPassphrase is the real, TTY-gated implementation wired as
// Service.PassphrasePrompt at CLI startup. It refuses outright -- rather
// than hanging, or silently reading whatever bytes happen to be on stdin --
// unless stdin is a genuine interactive terminal. This is a deliberate
// security property, not an incidental one: it's what makes the elevated
// key's passphrase unreachable to an automated caller (an MCP-connected
// agent, a script, or an agent shelling out via a non-interactive
// subprocess) that only has a pipe for stdin, not a real terminal.
func promptPassphrase(actor string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("signing as %s here requires the elevated-key passphrase, but no interactive terminal is attached to type it into", actor)
	}
	fmt.Fprintf(os.Stderr, "Enter passphrase to sign as %s: ", actor)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// nonInteractivePassphrasePrompt is wired instead of promptPassphrase for
// the mcp and tui commands. Both would otherwise risk racing
// term.ReadPassword against another consumer of the exact same stdin file
// descriptor: an MCP host that allocates a pty for the subprocess (where
// term.IsTerminal would read true, unlike the ordinary piped-stdio case
// promptPassphrase's TTY gate is designed for), or the TUI's own bubbletea
// program, which already owns raw stdin for its key-event loop. Rather than
// rely on TTY detection alone to avoid that race across every possible
// host/terminal combination, both contexts refuse outright, unconditionally,
// with a clear pointer to where this can actually be done safely: the CLI,
// run directly.
func nonInteractivePassphrasePrompt(context string) func(actor string) (string, error) {
	return func(actor string) (string, error) {
		return "", fmt.Errorf(
			"signing as %s here requires the elevated-key passphrase, which %s cannot safely prompt for -- "+
				"run the equivalent command from the CLI directly instead (e.g. `agent-comms approval approve`, `agent-comms agent activate`, or `agent-comms agent delete`)",
			actor, context)
	}
}

// promptNewPassphrase asks for a new elevated-key passphrase twice and
// requires both entries to match, refusing an empty passphrase. Also
// TTY-gated for the same reason as promptPassphrase.
func promptNewPassphrase(actor string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("registering an elevated key for %s requires an interactive terminal to set its passphrase", actor)
	}
	fmt.Fprintf(os.Stderr, "New passphrase for %s's elevated key: ", actor)
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if len(first) == 0 {
		return "", errors.New("passphrase must not be empty")
	}
	fmt.Fprint(os.Stderr, "Confirm passphrase: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passphrases did not match")
	}
	return string(first), nil
}
