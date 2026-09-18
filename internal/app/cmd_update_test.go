package app

import (
	"bytes"
	"context"
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
