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
	t.Setenv("HOME", home)
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

	sessionID, ok := discoverClaudeSessionID(pid)
	if !ok || sessionID != "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475" {
		t.Fatalf("expected the session ID to be discovered, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverClaudeSessionIDTimesOutWhenFileNeverAppears(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	original := sessionDiscoveryTimeout
	sessionDiscoveryTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sessionDiscoveryTimeout = original })

	sessionID, ok := discoverClaudeSessionID(999999)
	if ok || sessionID != "" {
		t.Fatalf("expected no discovery, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverClaudeSessionIDReportsNotOkForMalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionsDir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionsDir, "42.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	sessionID, ok := discoverClaudeSessionID(42)
	if ok || sessionID != "" {
		t.Fatalf("expected no discovery for malformed JSON, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverSessionIDUnknownAdapterReportsNotOk(t *testing.T) {
	sessionID, ok := discoverSessionID("agy", 1)
	if ok || sessionID != "" {
		t.Fatalf("expected agy (no known discovery mechanism yet) to report not-ok, got ok=%v id=%q", ok, sessionID)
	}
	sessionID, ok = discoverSessionID("opencode", 1)
	if ok || sessionID != "" {
		t.Fatalf("expected opencode (no known discovery mechanism yet) to report not-ok, got ok=%v id=%q", ok, sessionID)
	}
}

func TestDiscoverSessionIDDispatchesClaudeToTheClaudeDiscoverer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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

	sessionID, ok := discoverSessionID("claude", 7)
	if !ok || sessionID != "dispatched-session" {
		t.Fatalf("expected dispatch to discoverClaudeSessionID, got ok=%v id=%q", ok, sessionID)
	}
}
