package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskCreateMissingBranchFailsFastWithFlagName is the regression test
// for UX-09's reproduced bug: `task create` omitting --branch used to skip
// past the CLI entirely (title/branch/resource weren't marked required)
// and reach backend validation, which reported a single combined "title,
// repository, branch, and resources are required" message regardless of
// which field was actually missing. --branch (along with --title and
// --resource) is now a required flag, so this fails immediately with
// cobra's own message naming the flag -- before any event is ever
// attempted.
func TestTaskCreateMissingBranchFailsFastWithFlagName(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		args = append(args, "--project", project, "--actor", "owner", "--json")
		return Run(args, &out, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal"); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	err := run("task", "create", "--id", "task-1", "--title", "A title", "--resource", "src/x")
	if err == nil {
		t.Fatal("expected task create without --branch to fail")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Fatalf("error = %q, want it to name the missing --branch flag", err.Error())
	}
}
