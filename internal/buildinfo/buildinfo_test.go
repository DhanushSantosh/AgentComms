package buildinfo

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDevBuildIDIsStableForIdenticalContentAtDifferentPaths is the
// regression test for a real bug: devBuildID used to hash the executable's
// absolute path alongside its size and modtime, so two byte-identical
// binaries built at different paths (e.g. two separate git worktree
// checkouts of the same source, as this project's agents run concurrently
// today) got different build IDs despite being the same build. Content
// hashing must not depend on where the file happens to live.
func TestDevBuildIDIsStableForIdenticalContentAtDifferentPaths(t *testing.T) {
	content := []byte("identical binary content for this test")
	dirA := t.TempDir()
	dirB := t.TempDir()
	pathA := filepath.Join(dirA, "agent-comms")
	pathB := filepath.Join(dirB, "nested", "agent-comms")
	if err := os.MkdirAll(filepath.Dir(pathB), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathA, content, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, content, 0o755); err != nil {
		t.Fatal(err)
	}

	idA := devBuildIDForPath(pathA)
	idB := devBuildIDForPath(pathB)
	if idA != idB {
		t.Fatalf("identical content at different paths produced different build IDs: %q vs %q", idA, idB)
	}
}

// TestDevBuildIDChangesWithContent guards the other direction: a real
// content change must still change the build ID, or a rebuilt binary
// could silently reuse a stale daemon from a previous, different build --
// exactly what devBuildID exists to prevent.
func TestDevBuildIDChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-comms")
	if err := os.WriteFile(path, []byte("build one"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := devBuildIDForPath(path)

	if err := os.WriteFile(path, []byte("build two, different content"), 0o755); err != nil {
		t.Fatal(err)
	}
	after := devBuildIDForPath(path)

	if before == after {
		t.Fatalf("changed content produced the same build ID %q for both", before)
	}
}

// TestDevBuildIDIsStableAcrossModtimeChangesAlone guards the CI-cache-
// restore case directly: touching a file's modtime without touching its
// content (what a cache restore does) must not change the build ID, or
// every cache restore triggers a spurious daemon restart.
func TestDevBuildIDIsStableAcrossModtimeChangesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-comms")
	if err := os.WriteFile(path, []byte("unchanged content"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := devBuildIDForPath(path)

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	after := devBuildIDForPath(path)

	if before != after {
		t.Fatalf("modtime-only change produced a different build ID: %q vs %q", before, after)
	}
}
