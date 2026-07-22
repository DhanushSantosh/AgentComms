package opencodeclient

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestManualSmokeEnsureServer is a throwaway, human-run verification
// against the real opencode binary, not part of the normal suite: it
// confirms EnsureServer actually spawns a real, healthy, detached
// `opencode serve` instance, and that a second call reuses it rather than
// spawning a duplicate. Skipped unless AGENTCOMMS_ACP_SMOKE=1.
func TestManualSmokeEnsureServer(t *testing.T) {
	if os.Getenv("AGENTCOMMS_ACP_SMOKE") != "1" {
		t.Skip("set AGENTCOMMS_ACP_SMOKE=1 to run this against the real opencode binary")
	}
	projectRoot := t.TempDir()
	workDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	first, err := EnsureServer(ctx, projectRoot, workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first EnsureServer call: %s", first)
	if err := New(first).Health(ctx); err != nil {
		t.Fatalf("expected the spawned server to be healthy: %v", err)
	}

	second, err := EnsureServer(ctx, projectRoot, workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("second EnsureServer call: %s", second)
	if second != first {
		t.Fatalf("expected the second call to reuse the same server, got %q vs %q", second, first)
	}

	session, err := New(first).CreateSession(ctx, workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created real session via live server: %s", session.ID)
}
