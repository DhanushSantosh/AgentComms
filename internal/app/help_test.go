package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestProjectIndependentCommandsRunWithoutAProject guards RFC 0027
// section 12: binary- and user-scoped commands must succeed from a
// directory with no initialized project, and projectOptional commands
// must degrade instead of erroring.
func TestProjectIndependentCommandsRunWithoutAProject(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	bare := t.TempDir()
	for _, args := range [][]string{
		{"config", "--project", bare, "--json"},
		{"config", "theme", "dark", "--json"},
		{"profile", "current", "--project", bare, "--json"},
		{"doctor", "--project", bare, "--json"},
		{"agent-instructions", "--project", bare, "--json"},
	} {
		var out, errBuf bytes.Buffer
		if err := Run(args, &out, &errBuf); err != nil {
			t.Fatalf("%v should run without an initialized project: %v\n%s", args, err, errBuf.String())
		}
	}
	// A projectRequired command still fails clearly.
	var out, errBuf bytes.Buffer
	if err := Run([]string{"status", "--project", bare, "--json"}, &out, &errBuf); err == nil {
		t.Fatal("status should still require an initialized project")
	}
}

// TestEveryCommandHasShortHelp guards RFC 0027 section 1: no non-hidden
// command may ship with an empty one-line description. `agent-comms <cmd>
// --help` is the documented way an agent learns a command; a blank line
// there is a dead end.
func TestEveryCommandHasShortHelp(t *testing.T) {
	c := &cli{}
	root := c.root()
	var missing []string
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden {
			return
		}
		if cmd != root && strings.TrimSpace(cmd.Short) == "" {
			missing = append(missing, cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
	if len(missing) > 0 {
		t.Fatalf("%d command(s) have no Short description:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}
