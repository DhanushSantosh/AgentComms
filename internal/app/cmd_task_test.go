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

// TestTaskClaimDistinguishesScopeLeaseFromWorktreeLock is the regression
// test for UX-10: `task claim` without --worktree used to succeed with an
// empty Worktree field in its receipt -- indistinguishable from "the lock
// was acquired but happens to be blank" -- despite the command's own help
// text implying it always acquires a working-directory lock.
func TestTaskClaimDistinguishesScopeLeaseFromWorktreeLock(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")
	must("task", "create", "--id", "task-1", "--title", "Scope only", "--branch", "main", "--resource", "src/x", "--json")

	if err := run("task", "claim", "--id", "task-1"); err != nil {
		t.Fatalf("task claim: %v\n%s", err, stderr.String())
	}
	plain := out.String()
	if !strings.Contains(plain, "scope lease") {
		t.Fatalf("claim receipt should name the scope lease explicitly, got:\n%s", plain)
	}
	if !strings.Contains(plain, "not requested") {
		t.Fatalf("claim receipt should say the worktree lock was not requested, not leave it blank:\n%s", plain)
	}

	if err := run("task", "show", "--id", "task-1"); err != nil {
		t.Fatalf("task show: %v\n%s", err, stderr.String())
	}
	plain = out.String()
	for _, want := range []string{"not requested", "Protected resources", "src/x", "Lease expires"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("default task show is missing %q:\n%s", want, plain)
		}
	}
}

// TestTaskListEmptyStateNamesTheNextAction is the regression test for
// UX-15: `task list` on a fresh project used to print the same generic
// "(no rows)" a permission-limited or filtered-to-zero view would, naming
// no next step.
func TestTaskListEmptyStateNamesTheNextAction(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json"); err != nil {
		t.Fatalf("init: %v\n%s", err, stderr.String())
	}
	out.Reset()
	if err := run("task", "list"); err != nil {
		t.Fatalf("task list: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(out.String(), "No tasks yet") || !strings.Contains(out.String(), "task create") {
		t.Fatalf("expected an empty state naming the next action, got: %q", out.String())
	}
}

// TestTaskClaimWithWorktreeShowsThePath is a sanity check alongside the
// above: a real worktree claim must still show the actual path, not the
// "not requested" placeholder.
func TestTaskClaimWithWorktreeShowsThePath(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")
	must("task", "create", "--id", "task-2", "--title", "Worktree claim", "--branch", "main", "--resource", "src/y", "--json")

	worktreePath := filepath.Join(project, "checkout")
	if err := run("task", "claim", "--id", "task-2", "--worktree", worktreePath); err != nil {
		t.Fatalf("task claim: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(out.String(), worktreePath) {
		t.Fatalf("claim receipt should show the actual worktree path, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "not requested") {
		t.Fatalf("a real worktree claim must not say 'not requested':\n%s", out.String())
	}
}
