package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestDocumentCreateNotifyReportsPartialFailure is the regression test for
// UX-07 / RFC 0033: a failed --notify used to be visible only as a stderr
// line (itself suppressed under --quiet), so a caller relying on --json
// output had no field to check and got ok:true regardless of whether a
// requested notification actually went out. The document itself must still
// be created (a notification failure is not a document-creation failure).
func TestDocumentCreateNotifyReportsPartialFailure(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json", "--quiet")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "owner", "--id", "reviewer")
	must("agent", "activate", "--actor", "owner", "--id", "reviewer", "--role", "AGENT", "--scope", "src")

	// One real recipient (succeeds), one nonexistent recipient (fails) --
	// the audit's own reproduction of the bug.
	must("document", "create", "--actor", "owner", "--id", "doc-1", "--title", "Plan",
		"--body", "The plan", "--notify", "reviewer", "--notify", "nonexistent-agent")

	var result struct {
		Notify []struct {
			Principal string `json:"principal"`
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
			Error     string `json:"error"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(result.Notify) != 2 {
		t.Fatalf("want 2 notify results (quiet must not remove them), got %d: %s", len(result.Notify), out.String())
	}
	byPrincipal := map[string]struct {
		Principal string `json:"principal"`
		MessageID string `json:"message_id"`
		Status    string `json:"status"`
		Error     string `json:"error"`
	}{}
	for _, r := range result.Notify {
		byPrincipal[r.Principal] = r
	}
	if got := byPrincipal["reviewer"]; got.Status != "sent" || got.MessageID != "msg-5:doc-1:reviewer" {
		t.Fatalf("reviewer notify result = %+v, want status=sent message_id=msg-5:doc-1:reviewer", got)
	}
	failed := byPrincipal["nonexistent-agent"]
	if failed.Status != "failed" || failed.Error == "" {
		t.Fatalf("nonexistent-agent notify result = %+v, want status=failed with a non-empty error", failed)
	}

	// The document itself must exist despite the partial notify failure.
	must("document", "show", "--actor", "owner", "--id", "doc-1")
}

// TestDocumentCreateWithoutNotifyOmitsField confirms the additive-only
// contract in RFC 0033: a `document create` call that never used --notify
// gets a response with no "notify" field at all, byte-for-byte compatible
// with callers written before this change.
func TestDocumentCreateWithoutNotifyOmitsField(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("document", "create", "--actor", "owner", "--id", "doc-1", "--title", "Plan", "--body", "The plan")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &raw); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if _, present := raw["notify"]; present {
		t.Fatalf("notify field must be absent when --notify wasn't used: %s", out.String())
	}
}

// TestDocumentNotifyRetriesOnlyFailedRecipientWithoutDuplicating is the
// regression test for RFC 0033's retry command: retrying must not
// re-attempt (or duplicate a message for) a principal whose notification
// already went out, and must succeed for the one that previously failed
// once its blocker (the recipient not existing yet) is resolved.
func TestDocumentNotifyRetriesOnlyFailedRecipientWithoutDuplicating(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json", "--quiet")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--actor", "owner", "--id", "reviewer")
	must("agent", "activate", "--actor", "owner", "--id", "reviewer", "--role", "AGENT", "--scope", "src")

	// reviewer succeeds; late-joiner doesn't exist yet, so it fails.
	must("document", "create", "--actor", "owner", "--id", "doc-1", "--title", "Plan",
		"--body", "The plan", "--notify", "reviewer", "--notify", "late-joiner")

	// Now late-joiner is registered and activated -- the earlier blocker is gone.
	must("agent", "register", "--actor", "owner", "--id", "late-joiner")
	must("agent", "activate", "--actor", "owner", "--id", "late-joiner", "--role", "AGENT", "--scope", "src")

	must("document", "notify", "--actor", "owner", "--id", "doc-1", "--notify", "reviewer", "--notify", "late-joiner")

	var result struct {
		Notify []struct {
			Principal string `json:"principal"`
			MessageID string `json:"message_id"`
			Status    string `json:"status"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	byPrincipal := map[string]string{}
	for _, r := range result.Notify {
		byPrincipal[r.Principal] = r.Status
	}
	if byPrincipal["reviewer"] != "already-sent" {
		t.Fatalf("reviewer (already notified) should be already-sent, got %q: %s", byPrincipal["reviewer"], out.String())
	}
	if byPrincipal["late-joiner"] != "sent" {
		t.Fatalf("late-joiner (previously failed, now valid) should be sent, got %q: %s", byPrincipal["late-joiner"], out.String())
	}

	// Confirm no duplicate message was created for reviewer: inbox must
	// show exactly one message from this document for reviewer.
	must("message", "inbox", "--actor", "reviewer")
	var inbox map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &inbox); err != nil {
		t.Fatalf("decode inbox: %v\n%s", err, out.String())
	}
	count := 0
	for id := range inbox {
		if id == "msg-5:doc-1:reviewer" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one msg-5:doc-1:reviewer in reviewer's inbox, found %d: %s", count, out.String())
	}
}

// TestDocumentNotifyMessageIDsDoNotCollideAcrossDashes is the regression
// test for a bug an independent review caught before release: the original
// "msg-" + documentID + "-" + principal ID scheme was ambiguous whenever
// either ID itself contains a dash. Document "a-b" notifying "c" and
// document "a" notifying "b-c" both produced "msg-a-b-c" -- creating the
// second collided with the first's already-sent message and silently
// reported "already-sent" without "b-c" ever actually being notified.
func TestDocumentNotifyMessageIDsDoNotCollideAcrossDashes(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--json", "--quiet")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	for _, id := range []string{"c", "b-c"} {
		must("agent", "register", "--actor", "owner", "--id", id)
		must("agent", "activate", "--actor", "owner", "--id", id, "--role", "AGENT", "--scope", "src")
	}
	// document "a-b" notifying "c" -- under the old scheme, both this and
	// the next call produce "msg-a-b-c".
	must("document", "create", "--actor", "owner", "--id", "a-b", "--title", "First", "--body", "first", "--notify", "c")
	// document "a" notifying "b-c" -- must be its own, unambiguous message.
	must("document", "create", "--actor", "owner", "--id", "a", "--title", "Second", "--body", "second", "--notify", "b-c")

	if strings.Contains(out.String(), "already-sent") {
		t.Fatalf("false success for a never-notified recipient (message ID collision): %s", out.String())
	}
	var result struct {
		Notify []struct {
			Principal string `json:"principal"`
			Status    string `json:"status"`
		} `json:"notify"`
	}
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &result); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(result.Notify) != 1 || result.Notify[0].Principal != "b-c" || result.Notify[0].Status != "sent" {
		t.Fatalf("expected b-c to be actually notified, got: %+v", result.Notify)
	}
}
