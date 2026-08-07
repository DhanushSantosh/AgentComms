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
	clearProviderEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "3c48b78d-184f-4c66-ae96-4d7294075d36")
	sessionID, adapter := Capture()
	if sessionID != "3c48b78d-184f-4c66-ae96-4d7294075d36" || adapter != "claude" {
		t.Fatalf("unexpected capture: session=%q adapter=%q", sessionID, adapter)
	}
}

func TestCaptureReadsCodexThreadEnv(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("CODEX_THREAD_ID", "019e5408-3ef4-7db3-b584-03ad8f399199")
	sessionID, adapter := Capture()
	if sessionID != "019e5408-3ef4-7db3-b584-03ad8f399199" || adapter != "codex" {
		t.Fatalf("unexpected capture: session=%q adapter=%q", sessionID, adapter)
	}
}

// TestCaptureReadsAntigravityConversationEnv covers agy the same way
// TestCaptureReadsClaudeSessionEnv and TestCaptureReadsCodexThreadEnv
// already do for claude/codex -- this had no coverage at all when the agy
// env var check was added, and the variable it originally checked
// (ANTIGRAVITY_SESSION_ID, with an AGY_SESSION_ID fallback) turned out not
// to be real: `strings`-ing the installed agy binary found neither name
// anywhere, but did find the literal JS
// `conversationId: process.env.ANTIGRAVITY_CONVERSATION_ID` in a bundled
// sidecar script. Unlike the claude/codex vars, this one is not published by
// Google anywhere, so Capture only acts on it with the explicit opt-in set
// (see Capture's doc comment for the ToS reasoning) -- this test sets it.
func TestCaptureReadsAntigravityConversationEnv(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(agyUndocumentedEnvOptIn, "1")
	t.Setenv("ANTIGRAVITY_CONVERSATION_ID", "b4b6b7f0-6b8b-4b8b-8b8b-6b8b4b8b6b8b")
	sessionID, adapter := Capture()
	if sessionID != "b4b6b7f0-6b8b-4b8b-8b8b-6b8b4b8b6b8b" || adapter != "agy" {
		t.Fatalf("unexpected capture: session=%q adapter=%q", sessionID, adapter)
	}
}

// TestCaptureIgnoresAntigravityConversationEnvWithoutOptIn is the opt-in
// gate's actual guarantee: ANTIGRAVITY_CONVERSATION_ID alone, without
// agyUndocumentedEnvOptIn also set, must never be acted on -- this is what
// keeps agy session capture from happening silently by default the way
// TestCaptureReadsAntigravityConversationEnv confirms it does happen once
// an operator has consciously opted in.
func TestCaptureIgnoresAntigravityConversationEnvWithoutOptIn(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("ANTIGRAVITY_CONVERSATION_ID", "b4b6b7f0-6b8b-4b8b-8b8b-6b8b4b8b6b8b")
	sessionID, adapter := Capture()
	if sessionID != "" || adapter != "" {
		t.Fatalf("expected no capture without opt-in, got session=%q adapter=%q", sessionID, adapter)
	}
}

func TestCapturePrefersAgyOverClaudeAndCodexWhenBothPresent(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(agyUndocumentedEnvOptIn, "1")
	t.Setenv("ANTIGRAVITY_CONVERSATION_ID", "b4b6b7f0-6b8b-4b8b-8b8b-6b8b4b8b6b8b")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "3c48b78d-184f-4c66-ae96-4d7294075d36")
	t.Setenv("CODEX_THREAD_ID", "019e5408-3ef4-7db3-b584-03ad8f399199")
	sessionID, adapter := Capture()
	if sessionID != "b4b6b7f0-6b8b-4b8b-8b8b-6b8b4b8b6b8b" || adapter != "agy" {
		t.Fatalf("expected agy to win when all three are set, got session=%q adapter=%q", sessionID, adapter)
	}
}

func TestCapturePrefersClaudeWhenBothPresent(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "3c48b78d-184f-4c66-ae96-4d7294075d36")
	t.Setenv("CODEX_THREAD_ID", "019e5408-3ef4-7db3-b584-03ad8f399199")
	sessionID, adapter := Capture()
	if sessionID != "3c48b78d-184f-4c66-ae96-4d7294075d36" || adapter != "claude" {
		t.Fatalf("expected claude to win when both are set, got session=%q adapter=%q", sessionID, adapter)
	}
}

func TestCaptureReportsNothingWithoutProviderEnv(t *testing.T) {
	clearProviderEnv(t)
	sessionID, adapter := Capture()
	if sessionID != "" || adapter != "" {
		t.Fatalf("expected no capture, got session=%q adapter=%q", sessionID, adapter)
	}
}

// clearProviderEnv resets every provider env var Capture checks, via
// t.Setenv (auto-restored after the test), so one test's provider var
// never leaks into another run in the same process -- and so each test
// only asserts on the vars it explicitly sets, not on whatever happened to
// already be in the ambient environment.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"ANTIGRAVITY_CONVERSATION_ID", agyUndocumentedEnvOptIn,
		"CLAUDE_CODE_SESSION_ID", "CODEX_THREAD_ID",
	} {
		t.Setenv(name, "")
	}
}
