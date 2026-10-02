package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeRelease(tag string) githubRelease {
	return githubRelease{Tag: tag}
}

func TestUpdatePromptsAndInstallsOnYes(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	// c.out/c.err are set to the SAME buffers passed to root.SetOut/SetErr
	// below, matching how Run() (internal/app/app.go) wires them in
	// production: c := &cli{out: stdout, err: stderr, ...}; root.SetOut(stdout).
	// emitDocument/emitUpdateApply write through c.out directly (never
	// cmd.OutOrStdout()), so a separate, unshared buffer for c.out would
	// leave the cobra-level stdout this test inspects empty even though
	// the command ran and rendered its document correctly.
	var stdout, stderr bytes.Buffer
	c := &cli{
		out: &stdout, err: &stderr, timeout: time.Second,
		in: strings.NewReader("y\n"),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return map[string]any{"version": "0.7.1", "installed": "/fake/path", "previous": "/fake/path", "verified": true}, nil
		},
	}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if !installed {
		t.Fatal("expected the release to be installed after answering y")
	}
	if !strings.Contains(stdout.String(), "0.7.1") {
		t.Fatalf("expected stdout to mention the new version, got: %s", stdout.String())
	}
}

func TestUpdatePromptsAndSkipsInstallOnNo(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	var stdout, stderr bytes.Buffer
	c := &cli{
		out: &stdout, err: &stderr, timeout: time.Second,
		in: strings.NewReader("n\n"),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return nil, nil
		},
	}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if installed {
		t.Fatal("expected no install after answering n")
	}
}

func TestUpdateNonInteractiveInstallsWithoutPrompting(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	Version = "0.7.0"
	t.Cleanup(func() { Version = "0.7.0" })

	// c.nonInteractive is NOT set directly in the struct literal here: the
	// root command's own --non-interactive flag is bound with
	// f.BoolVar(&c.nonInteractive, "non-interactive", false, ...)
	// (internal/app/app.go), and BoolVar resets the bound variable to its
	// given default (false) the moment c.root() registers it -- so a
	// pre-set `nonInteractive: true` here would be silently overwritten
	// back to false before RunE ever runs. Passing the real flag in
	// SetArgs is the only way this field ends up true for this test.
	installed := false
	var stdout, stderr bytes.Buffer
	c := &cli{
		out: &stdout, err: &stderr, timeout: time.Second,
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return map[string]any{"version": "0.7.1", "installed": "/fake/path", "previous": "/fake/path", "verified": true}, nil
		},
	}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade", "--non-interactive"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if !installed {
		t.Fatal("expected --non-interactive to install without a prompt")
	}
}

func TestUpdateReportsAlreadyCurrentWithoutPrompting(t *testing.T) {
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(t.TempDir(), "credentials"))
	Version = "0.7.1"
	t.Cleanup(func() { Version = "0.7.0" })

	installed := false
	var stdout, stderr bytes.Buffer
	c := &cli{
		out: &stdout, err: &stderr, timeout: time.Second,
		in: strings.NewReader(""),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v0.7.1"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			installed = true
			return nil, nil
		},
	}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"update", "--skip-project-upgrade"})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}
	if installed {
		t.Fatal("expected no install when already current")
	}
	if !strings.Contains(stdout.String(), "up to date") {
		t.Fatalf("expected stdout to say up to date, got: %s", stdout.String())
	}
}

// `update` replaces this executable, so reconciling projects in
// PersistentPreRunE would do it with the binary on its way out. A project
// whose recorded minimum toolkit is already the incoming version is refused
// by the outgoing one, and the resulting "skipped lifecycle inspection ...
// requires toolkit X, running Y" warnings flush after the update succeeded
// -- reading as though reconciliation failed when it had not been attempted.
// Reported live upgrading 0.7.1 -> 0.8.0.
//
// update reconciles properly by re-execing the freshly installed binary, so
// the pre-run pass must not happen at all.
func TestUpdateDoesNotReconcileProjectsWithTheOutgoingBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(home, "config"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(home, "credentials"))
	Version = "0.7.1"
	t.Cleanup(func() { Version = "0.7.1" })

	// A project that only the incoming version may touch: exactly the
	// shape the outgoing binary refuses to inspect.
	project := filepath.Join(home, "future-project")
	if err := os.MkdirAll(filepath.Join(project, ".agent-comms"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".agentcomms"),
		[]byte("# Agent Comms managed bootstrap\nruntime = .agent-comms\nmode = personal\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := `{"schema_version":"2.2.0","toolkit_version":"9.9.9","minimum_toolkit_version":"9.9.9",` +
		`"project_format_version":1,"managed_files_version":2,"runtime_mode":"personal",` +
		`"project_id":"ac-future","owner":"owner"}`
	if err := os.WriteFile(filepath.Join(project, ".agent-comms", "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	// reconcileUserInstallation only sees projects recorded in identity
	// profiles, so without this the test would pass whether or not the fix
	// is present -- verified by removing the fix and watching it stay green.
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	userConfig := fmt.Sprintf(`{"profiles":{"ac-future:owner":{"name":"ac-future:owner","project_id":"ac-future","actor":"owner","project_root":%q}}}`, project)
	if err := os.WriteFile(filepath.Join(home, "config", "config.json"), []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	handoffCalled := false
	c := &cli{
		out: &stdout, err: &stderr, timeout: time.Second,
		in: strings.NewReader("y\n"),
		fetchReleaseFn: func(ctx context.Context, channel, version string) (githubRelease, error) {
			return fakeRelease("v9.9.9"), nil
		},
		installReleaseFn: func(ctx context.Context, r githubRelease) (map[string]any, error) {
			return map[string]any{"version": "9.9.9", "installed": "/fake/new-binary", "previous": "/fake/old", "verified": true}, nil
		},
		handoffRunner: func(ctx context.Context, executable string, args []string, in io.Reader, out, errOut io.Writer) error {
			// The reconcile that does happen must use the NEW binary.
			handoffCalled = true
			if executable != "/fake/new-binary" {
				t.Errorf("the reconcile must use the installed binary, got %q", executable)
			}
			return nil
		},
	}
	root := c.root()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	// --project, not the working directory: tests run from internal/app,
	// which is not an initialized project, so without it update took its
	// "current directory is not an initialized project" branch, never
	// reached the handoff, and the new-binary check below never ran.
	root.SetArgs([]string{"update", "--yes", "--current-project-only", "--project", project})
	if err := root.Execute(); err != nil {
		t.Fatalf("update: %v\nstderr: %s", err, stderr.String())
	}

	combined := stdout.String() + stderr.String()
	if strings.Contains(combined, "skipped lifecycle inspection") {
		t.Errorf("update must not reconcile with the outgoing binary:\n%s", combined)
	}
	if strings.Contains(combined, "requires toolkit") {
		t.Errorf("no toolkit-version warning should survive an update:\n%s", combined)
	}
	// The executable check above lives inside the handoff runner, so it
	// only proves anything if the runner actually ran. Without this, an
	// update that skipped the post-install reconcile entirely would pass
	// -- the one outcome this test exists to rule out.
	if !handoffCalled {
		t.Error("update never handed the project reconcile to the installed binary")
	}
}
