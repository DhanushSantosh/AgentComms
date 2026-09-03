package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReplaceExecutableFollowsSymlink covers RFC 0030 §3: when the CLI is
// run through the `agc` symlink, `update apply` must replace the real
// `agent-comms` binary, and the by-name symlink must still resolve to the
// new one afterward.
func TestReplaceExecutableFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "agent-comms")
	if err := os.WriteFile(real, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "agc")
	if err := os.Symlink("agent-comms", alias); err != nil {
		t.Fatal(err)
	}

	installed, backup, err := replaceExecutable(alias, []byte("NEW BINARY"))
	if err != nil {
		t.Fatalf("replaceExecutable via symlink: %v", err)
	}
	if installed != real {
		t.Fatalf("installed = %q, want the real target %q", installed, real)
	}
	if backup != real+".previous" {
		t.Fatalf("backup = %q, want %q", backup, real+".previous")
	}

	if got, _ := os.ReadFile(real); string(got) != "NEW BINARY" {
		t.Fatalf("real binary content = %q, want NEW BINARY", got)
	}
	if got, _ := os.ReadFile(backup); string(got) != "OLD BINARY" {
		t.Fatalf("backup content = %q, want OLD BINARY", got)
	}
	// The alias is untouched and now resolves to the new binary.
	fi, err := os.Lstat(alias)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("alias should still be a symlink: %v %v", fi, err)
	}
	if got, _ := os.ReadFile(alias); string(got) != "NEW BINARY" {
		t.Fatalf("reading through the alias = %q, want NEW BINARY", got)
	}
}
