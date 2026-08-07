package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverClaudeSessionIDFindsFileWrittenAfterAShortDelay(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	pid := 123456
	record := struct {
		PID       int    `json:"pid"`
		SessionID string `json:"sessionId"`
	}{PID: pid, SessionID: "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475"}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate the real-world lag between the child process starting and
	// it getting around to writing its own session file, so this exercises
	// the poll loop rather than a lucky first read.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(sessionsDir, "123456.json"), raw, 0o600)
	}()

	sessionID, ok := discoverClaudeSessionID(home, pid)
	if !ok || sessionID != "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475" {
		t.Fatalf("expected the session ID to be discovered, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverClaudeSessionIDTimesOutWhenFileNeverAppears(t *testing.T) {
	home := t.TempDir()

	original := sessionDiscoveryTimeout
	sessionDiscoveryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sessionDiscoveryTimeout = original })

	sessionID, ok := discoverClaudeSessionID(home, 999999)
	if ok || sessionID != "" {
		t.Fatalf("expected no discovery, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverClaudeSessionIDReportsNotOkForMalformedJSON(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "42.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	sessionID, ok := discoverClaudeSessionID(home, 42)
	if ok || sessionID != "" {
		t.Fatalf("expected no discovery for malformed JSON, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverSessionIDUnknownAdapterReportsNotOk(t *testing.T) {
	sessionID, ok := discoverSessionID("codex", 1, t.TempDir())
	if ok || sessionID != "" {
		t.Fatalf("expected codex (not wrapped by interactive-serve, no discoverer registered) to report not-ok, got ok=%v id=%q", ok, sessionID)
	}
}

// TestDiscoverSessionIDDispatchesAgyWithoutOptInReportsNotOk exercises the
// dispatcher's agy branch specifically (discoverAgySessionID itself is
// covered directly in sessiondiscovery_agy_test.go): without the opt-in,
// this must return immediately, not poll out the full timeout.
func TestDiscoverSessionIDDispatchesAgyWithoutOptInReportsNotOk(t *testing.T) {
	t.Setenv("AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV", "")
	sessionID, ok := discoverSessionID("agy", 1, t.TempDir())
	if ok || sessionID != "" {
		t.Fatalf("expected no discovery without the agy opt-in set, got ok=%v id=%q", ok, sessionID)
	}
}

// TestDiscoverSessionIDDispatchesOpencodeGracefullyWhenCLIUnavailable
// forces exec.LookPath("opencode") to fail regardless of what's actually
// installed on the machine running this test (CI has no reason to have the
// opencode CLI on PATH), so the behavior asserted here -- graceful
// not-found, no panic or hang -- doesn't depend on the host environment.
func TestDiscoverSessionIDDispatchesOpencodeGracefullyWhenCLIUnavailable(t *testing.T) {
	original := sessionDiscoveryTimeout
	sessionDiscoveryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sessionDiscoveryTimeout = original })
	t.Setenv("PATH", t.TempDir())

	sessionID, ok := discoverSessionID("opencode", 1, t.TempDir())
	if ok || sessionID != "" {
		t.Fatalf("expected not-found when the opencode CLI is unavailable, got ok=%v id=%q", ok, sessionID)
	}
}

// TestParseOpencodeSessionListJSONParsesARealCapturedSample uses the exact
// raw output PETER (running opencode live) captured 2026-08-07 from
// `opencode session list --format json --max-count 1`, run from this
// project's own root -- a real sample, not a guessed shape.
func TestParseOpencodeSessionListJSONParsesARealCapturedSample(t *testing.T) {
	raw := []byte(`[{"id":"ses_032d59696ffepgBGk73AiMF00F","title":"Register agent as PETER with agent-comms","updated":1786110988269,"created":1785853536617,"projectId":"79a8a333323ffb5e54a60c4293dadad014b4363c","directory":"/home/dhanush/Projects/AgentComms"}]`)
	id, ok := parseOpencodeSessionListJSON(raw)
	if !ok || id != "ses_032d59696ffepgBGk73AiMF00F" {
		t.Fatalf("got ok=%v id=%q", ok, id)
	}
}

func TestParseOpencodeSessionListJSONReportsNotOkForEmptyArray(t *testing.T) {
	id, ok := parseOpencodeSessionListJSON([]byte(`[]`))
	if ok || id != "" {
		t.Fatalf("expected not-ok for an empty session list, got ok=%v id=%q", ok, id)
	}
}

func TestParseOpencodeSessionListJSONReportsNotOkForMalformedJSON(t *testing.T) {
	id, ok := parseOpencodeSessionListJSON([]byte(`not json`))
	if ok || id != "" {
		t.Fatalf("expected not-ok for malformed JSON, got ok=%v id=%q", ok, id)
	}
}

// TestDiscoverSessionIDDispatchesClaudeToTheClaudeDiscoverer exercises the
// dispatcher, which (unlike discoverClaudeSessionID itself) does resolve
// os.UserHomeDir() internally -- so both HOME (Unix, macOS) and USERPROFILE
// (Windows; os.UserHomeDir() ignores HOME there) are set, rather than
// relying on whichever one the OS running the test actually reads. Setting
// the one the current OS ignores is harmless.
func TestDiscoverSessionIDDispatchesClaudeToTheClaudeDiscoverer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sessionsDir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(struct {
		SessionID string `json:"sessionId"`
	}{SessionID: "dispatched-session"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "7.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	sessionID, ok := discoverSessionID("claude", 7, t.TempDir())
	if !ok || sessionID != "dispatched-session" {
		t.Fatalf("expected dispatch to discoverClaudeSessionID, got ok=%v id=%q", ok, sessionID)
	}
}
