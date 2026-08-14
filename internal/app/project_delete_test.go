package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/store"
)

// TestProjectDeleteCLIRefusesWithoutInteractiveTerminal is RFC 0020's CLI
// safety property: unlike every other command, project delete has no
// --yes or piped-passphrase escape hatch at all -- it refuses outright
// under --non-interactive, before touching the project or prompting for
// anything, matching agent elevate-key's own reasoning (a passphrase
// prompt is meaningless to a script, and this is the single most
// destructive command in the system).
func TestProjectDeleteCLIRefusesWithoutInteractiveTerminal(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	if e := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); e != nil {
		t.Fatalf("init: %v\n%s", e, stderr.String())
	}
	e := run("project", "delete", "--non-interactive", "--actor", "owner")
	if e == nil {
		t.Fatal("expected project delete to refuse under --non-interactive")
	}
	if !strings.Contains(e.Error(), "interactive terminal") {
		t.Fatalf("expected an interactive-terminal-shaped error, got: %v", e)
	}
	// Nothing about the project may have been touched by a refused attempt.
	if _, statErr := os.Stat(filepath.Join(project, store.Runtime)); statErr != nil {
		t.Fatalf("expected the project runtime to survive a refused delete: %v", statErr)
	}
}
