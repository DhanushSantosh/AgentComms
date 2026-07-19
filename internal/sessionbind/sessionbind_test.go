package sessionbind

import (
	"testing"
)

func TestSaveAndLoadRoundTrips(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, "axiom-runtime-1", "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475", "claude"); err != nil {
		t.Fatal(err)
	}
	binding, ok, err := Load(root, "axiom-runtime-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a saved binding")
	}
	if binding.SessionID != "e22cbdad-7233-4d6d-8ecc-0c4bffd8c475" || binding.Adapter != "claude" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	if binding.CapturedAt.IsZero() {
		t.Fatal("expected captured_at to be set")
	}
}

func TestLoadMissingBindingReportsNotFound(t *testing.T) {
	root := t.TempDir()
	_, ok, err := Load(root, "unknown-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no binding for an unregistered runtime")
	}
}

func TestSavePreservesOtherRuntimeBindings(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, "axiom-runtime-1", "session-a", "claude"); err != nil {
		t.Fatal(err)
	}
	if err := Save(root, "damon-runtime-1", "session-b", "codex"); err != nil {
		t.Fatal(err)
	}
	first, ok, err := Load(root, "axiom-runtime-1")
	if err != nil || !ok || first.SessionID != "session-a" {
		t.Fatalf("first binding lost: ok=%v err=%v binding=%+v", ok, err, first)
	}
	second, ok, err := Load(root, "damon-runtime-1")
	if err != nil || !ok || second.SessionID != "session-b" {
		t.Fatalf("second binding lost: ok=%v err=%v binding=%+v", ok, err, second)
	}
}

func TestCaptureReadsClaudeSessionEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "3c48b78d-184f-4c66-ae96-4d7294075d36")
	sessionID, adapter := Capture()
	if sessionID != "3c48b78d-184f-4c66-ae96-4d7294075d36" || adapter != "claude" {
		t.Fatalf("unexpected capture: session=%q adapter=%q", sessionID, adapter)
	}
}

func TestCaptureReportsNothingWithoutProviderEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	sessionID, adapter := Capture()
	if sessionID != "" || adapter != "" {
		t.Fatalf("expected no capture, got session=%q adapter=%q", sessionID, adapter)
	}
}
