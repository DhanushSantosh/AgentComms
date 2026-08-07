package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseAgyConversationIDFromEnvironFindsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environ")
	raw := "PATH=/usr/bin\x00ANTIGRAVITY_CONVERSATION_ID=5885f88c-6cdf-4343-ad9d-693e66d41852\x00HOME=/home/hulk\x00"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	id, ok := parseAgyConversationIDFromEnviron(path)
	if !ok || id != "5885f88c-6cdf-4343-ad9d-693e66d41852" {
		t.Fatalf("got ok=%v id=%q", ok, id)
	}
}

func TestParseAgyConversationIDFromEnvironMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "environ")
	raw := "PATH=/usr/bin\x00HOME=/home/hulk\x00"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	id, ok := parseAgyConversationIDFromEnviron(path)
	if ok || id != "" {
		t.Fatalf("expected not-found, got ok=%v id=%q", ok, id)
	}
}

func TestParseAgyConversationIDFromEnvironMissingFile(t *testing.T) {
	id, ok := parseAgyConversationIDFromEnviron(filepath.Join(t.TempDir(), "does-not-exist"))
	if ok || id != "" {
		t.Fatalf("expected not-found for a missing file, got ok=%v id=%q", ok, id)
	}
}

func TestDiscoverAgySessionIDRequiresOptIn(t *testing.T) {
	t.Setenv("AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV", "")
	// A pid that could never resolve to a real environ, to confirm this
	// returns false because of the missing opt-in specifically, not
	// because of a bogus pid.
	id, ok := discoverAgySessionID(1)
	if ok || id != "" {
		t.Fatalf("expected no discovery without the opt-in set, got ok=%v id=%q", ok, id)
	}
}

// TestDiscoverAgySessionIDReadsARealProcess spawns a real short-lived child
// with ANTIGRAVITY_CONVERSATION_ID in its own environment and confirms
// discoverAgySessionID finds it via the child's actual, real
// /proc/<pid>/environ -- not just the fixture-file parsing unit tests above.
// Linux-only (procfs), matching production's own graceful "not found"
// behavior on any other OS -- skipped here rather than asserted false, since
// this test's own child-spawning setup assumes procfs is how discovery
// would work at all.
func TestDiscoverAgySessionIDReadsARealProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/environ is Linux-only")
	}
	t.Setenv("AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV", "1")

	cmd := exec.Command("sleep", "5")
	cmd.Env = append(os.Environ(), "ANTIGRAVITY_CONVERSATION_ID=real-proc-session-id")
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn a real test process: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	original := sessionDiscoveryTimeout
	sessionDiscoveryTimeout = 3 * time.Second
	t.Cleanup(func() { sessionDiscoveryTimeout = original })

	id, ok := discoverAgySessionID(cmd.Process.Pid)
	if !ok || id != "real-proc-session-id" {
		t.Fatalf("got ok=%v id=%q", ok, id)
	}
}
