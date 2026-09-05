package releaseverify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// extractShellFunction pulls a named function's exact literal text out of a
// shell script, so this test always exercises install.sh's real, current
// checksum_of -- never a copy that could silently drift from the source it
// is supposed to be testing. Assumes the conventional "name(){" opening and
// a lone "}" closing line this project's own install.sh uses.
// shellQuote wraps a value in single quotes for safe interpolation into a
// generated POSIX sh script -- needed here because t.TempDir() embeds the
// subtest name verbatim, and this package's own subtest names contain
// parentheses and spaces that would otherwise break the script.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func extractShellFunction(t *testing.T, scriptPath, name string) string {
	t.Helper()
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	open := name + "(){"
	start := -1
	for i, line := range lines {
		if line == open {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("function %s not found in %s (looked for literal %q)", name, scriptPath, open)
	}
	for i := start; i < len(lines); i++ {
		if lines[i] == "}" {
			return strings.Join(lines[start:i+1], "\n")
		}
	}
	t.Fatalf("closing brace for %s not found in %s", name, scriptPath)
	return ""
}

// TestChecksumOfFallback is the regression test for UX-02: checksum_of used
// to be `sha256sum ... | awk ... || shasum ...`, and a shell pipeline's exit
// status is its *last* command's, not sha256sum's -- so a missing or
// failing sha256sum still left awk exiting 0 on empty input, and the
// fallback to shasum never ran at all. Exercises the real, current function
// text from install.sh (via extractShellFunction) against every tool-
// availability combination the audit's acceptance criteria named, using a
// minimal, fully-controlled PATH rather than the host's real one.
func TestChecksumOfFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh targets POSIX sh")
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	installer := filepath.Join(repoRoot, "install.sh")
	fn := extractShellFunction(t, installer, "checksum_of")

	// The real digest for this exact content, so a successful path can be
	// checked against a known value, not just "some 64 hex chars". Computed
	// here rather than hardcoded so it can never be a typo'd/stale literal.
	const fixtureContent = "release asset contents\n"
	fixtureDigestBytes := sha256.Sum256([]byte(fixtureContent))
	fixtureDigest := hex.EncodeToString(fixtureDigestBytes[:])

	newFakeTool := func(dir, name, script string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	run := func(t *testing.T, path, tmpDir string) (stdout, stderr string, err error) {
		t.Helper()
		script := "TMP=" + shellQuote(tmpDir) + "\n" + fn + "\nchecksum_of fixture\n"
		cmd := exec.Command("sh", "-c", script)
		cmd.Env = []string{"PATH=" + path}
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err = cmd.Run()
		return out.String(), errBuf.String(), err
	}

	newFixtureDir := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "fixture"), []byte(fixtureContent), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	// A minimal real toolchain every scenario needs regardless of which
	// checksum tool it is testing: sh builtins cover most of it, but
	// `command`, `printf`, `awk`, `grep` must resolve from PATH. Symlink
	// the real ones in rather than depending on the host's own PATH order.
	baseTools := func(t *testing.T, dir string) {
		t.Helper()
		for _, tool := range []string{"awk", "grep"} {
			real, err := exec.LookPath(tool)
			if err != nil {
				t.Skipf("%s not available on this host to build the test PATH", tool)
			}
			if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("sha256sum available", func(t *testing.T) {
		real, err := exec.LookPath("sha256sum")
		if err != nil {
			t.Skip("sha256sum not available on this host")
		}
		binDir := t.TempDir()
		baseTools(t, binDir)
		if err := os.Symlink(real, filepath.Join(binDir, "sha256sum")); err != nil {
			t.Fatal(err)
		}
		fixtureDir := newFixtureDir(t)
		out, errOut, err := run(t, binDir, fixtureDir)
		if err != nil {
			t.Fatalf("checksum_of failed with sha256sum available: %v, stderr=%s", err, errOut)
		}
		if strings.TrimSpace(out) != fixtureDigest {
			t.Fatalf("digest = %q, want %q", strings.TrimSpace(out), fixtureDigest)
		}
	})

	t.Run("only shasum available (the exact reported bug)", func(t *testing.T) {
		real, err := exec.LookPath("shasum")
		if err != nil {
			t.Skip("shasum not available on this host")
		}
		binDir := t.TempDir()
		baseTools(t, binDir)
		if err := os.Symlink(real, filepath.Join(binDir, "shasum")); err != nil {
			t.Fatal(err)
		}
		fixtureDir := newFixtureDir(t)
		out, errOut, err := run(t, binDir, fixtureDir)
		if err != nil {
			t.Fatalf("checksum_of did not fall back to shasum when sha256sum is absent: %v, stderr=%s", err, errOut)
		}
		if strings.TrimSpace(out) != fixtureDigest {
			t.Fatalf("digest via shasum fallback = %q, want %q", strings.TrimSpace(out), fixtureDigest)
		}
	})

	t.Run("neither tool available", func(t *testing.T) {
		binDir := t.TempDir()
		baseTools(t, binDir)
		fixtureDir := newFixtureDir(t)
		out, errOut, err := run(t, binDir, fixtureDir)
		if err == nil {
			t.Fatalf("expected checksum_of to fail with neither tool available, got output %q", out)
		}
		if out != "" {
			t.Fatalf("expected no stdout digest on failure, got %q", out)
		}
		if !strings.Contains(errOut, "neither sha256sum nor shasum is available") {
			t.Fatalf("expected an actionable prerequisite error, got stderr=%q", errOut)
		}
	})

	t.Run("sha256sum present but fails on this file", func(t *testing.T) {
		binDir := t.TempDir()
		baseTools(t, binDir)
		newFakeTool(binDir, "sha256sum", "echo 'sha256sum: permission denied' >&2\nexit 1\n")
		fixtureDir := newFixtureDir(t)
		out, errOut, err := run(t, binDir, fixtureDir)
		if err == nil {
			t.Fatalf("expected checksum_of to propagate a real sha256sum failure, got output %q", out)
		}
		if out != "" {
			t.Fatalf("expected no stdout digest on a real tool failure, got %q", out)
		}
		if !strings.Contains(errOut, "sha256sum failed to hash") {
			t.Fatalf("expected the failure to be attributed to sha256sum, got stderr=%q", errOut)
		}
	})

	t.Run("tool present but produces a malformed digest", func(t *testing.T) {
		binDir := t.TempDir()
		baseTools(t, binDir)
		newFakeTool(binDir, "sha256sum", "echo 'not-a-real-digest fixture'\n")
		fixtureDir := newFixtureDir(t)
		out, errOut, err := run(t, binDir, fixtureDir)
		if err == nil {
			t.Fatalf("expected checksum_of to reject a malformed digest, got output %q", out)
		}
		if out != "" {
			t.Fatalf("expected no stdout digest for a malformed shape, got %q", out)
		}
		if !strings.Contains(errOut, "unexpected SHA-256 digest shape") {
			t.Fatalf("expected a digest-shape error, got stderr=%q", errOut)
		}
	})
}
