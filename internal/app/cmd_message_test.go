package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestInboxUnreadReflectsPerRecipientObligation is the regression test for
// UX-05's second half: --unread used to check the message's aggregate
// Status, not this recipient's own per-recipient obligation, so a
// two-recipient ACTION stayed "OPEN" (aggregate) until *every* recipient
// responded -- a recipient who had already acknowledged their own copy
// kept seeing it in --unread purely because someone else hadn't acted yet.
func TestInboxUnreadReflectsPerRecipientObligation(t *testing.T) {
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
	must("agent", "register", "--actor", "owner", "--id", "recipient-a")
	must("agent", "activate", "--actor", "owner", "--id", "recipient-a", "--role", "AGENT", "--scope", "src")
	must("agent", "register", "--actor", "owner", "--id", "recipient-b")
	must("agent", "activate", "--actor", "owner", "--id", "recipient-b", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "two-recipients", "--kind", "ACTION",
		"--to", "recipient-a", "--to", "recipient-b", "--subject", "Please review", "--body", "Details")

	// recipient-a acknowledges its own copy; recipient-b never acts.
	must("message", "ack", "--actor", "recipient-a", "--id", "two-recipients")

	must("message", "inbox", "--actor", "recipient-a", "--unread")
	var recipientAInbox map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &recipientAInbox); err != nil {
		t.Fatal(err)
	}
	if _, stillUnread := recipientAInbox["two-recipients"]; stillUnread {
		t.Fatalf("recipient-a (already acknowledged) still shown as unread purely because recipient-b hasn't acted: %s", out.String())
	}

	must("message", "inbox", "--actor", "recipient-b", "--unread")
	var recipientBInbox map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &recipientBInbox); err != nil {
		t.Fatal(err)
	}
	if _, stillUnread := recipientBInbox["two-recipients"]; !stillUnread {
		t.Fatalf("recipient-b (never acted) should still see the message as unread: %s", out.String())
	}
}

// TestInboxLimitIsDeterministic is the regression test for UX-05's other
// half: --limit used to trim a Go map (randomized iteration order) before
// sorting, so repeated calls against an unchanged inbox returned different
// IDs. Fixed by sorting first, then slicing the already-sorted ID list.
func TestInboxLimitIsDeterministic(t *testing.T) {
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
	must("agent", "register", "--actor", "owner", "--id", "recipient")
	must("agent", "activate", "--actor", "owner", "--id", "recipient", "--role", "AGENT", "--scope", "src")
	for _, id := range []string{"msg-a", "msg-b", "msg-c", "msg-d", "msg-e", "msg-f", "msg-g", "msg-h"} {
		must("message", "post", "--actor", "owner", "--id", id, "--kind", "FYI", "--to", "recipient", "--subject", id)
	}

	first := ""
	for i := 0; i < 12; i++ {
		must("message", "inbox", "--actor", "recipient", "--limit", "1")
		var got map[string]any
		if err := json.Unmarshal(extractResult(t, out.Bytes()), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("call %d: --limit 1 returned %d entries, want 1: %s", i, len(got), out.String())
		}
		var only string
		for id := range got {
			only = id
		}
		if first == "" {
			first = only
		} else if only != first {
			t.Fatalf("call %d: --limit 1 returned %q, want the same %q every time against an unchanged inbox", i, only, first)
		}
	}
}

// TestMessageShow is the regression test for UX-04 / RFC 0032: there was
// no way to read one message's subject and body directly by ID.
func TestMessageShow(t *testing.T) {
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
	must("agent", "register", "--actor", "owner", "--id", "recipient")
	must("agent", "activate", "--actor", "owner", "--id", "recipient", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "readable", "--kind", "FYI",
		"--to", "recipient", "--subject", "A subject worth reading", "--body", "The full body text")

	must("message", "show", "--actor", "recipient", "--id", "readable")
	var shown struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		From    string `json:"from"`
	}
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.Subject != "A subject worth reading" || shown.Body != "The full body text" || shown.From != "owner" {
		t.Fatalf("message show returned %+v, want the full subject/body/sender", shown)
	}

	if err := run("message", "show", "--actor", "recipient", "--id", "does-not-exist"); err == nil {
		t.Fatal("expected message show for an unknown ID to fail")
	}
}

// TestApprovalShowDefaultsIncludeReviewedOperationAndExpiry is the
// regression test for UX-06: the default `approval show` view had tier,
// status, requester, action, and reason, but not the reviewed operation's
// subject, its expiry, or affected principals -- all present, but only
// under --details, even though a reviewer following the natural "show
// then approve" workflow is exactly who needs to see them without an
// extra flag.
func TestApprovalShowDefaultsIncludeReviewedOperationAndExpiry(t *testing.T) {
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
	must("agent", "register", "--actor", "owner", "--id", "reviewer")
	must("agent", "activate", "--actor", "owner", "--id", "reviewer", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "bound-contract", "--kind", "CONTRACT",
		"--to", "reviewer", "--subject", "Review exact operation", "--body", "Only publish these reviewed terms",
		"--request-approval", "--approval-id", "bound-contract-approval", "--non-interactive")

	// Plain human output is the actual bar the audit found too low --
	// --json always carried the full struct regardless of this bug.
	if err := Run([]string{"approval", "show", "--project", project, "--actor", "reviewer",
		"--id", "bound-contract-approval", "--output", "plain"}, &out, &stderr); err != nil {
		t.Fatalf("approval show (plain): %v\n%s", err, stderr.String())
	}
	plain := out.String()
	for _, want := range []string{"Subject", "Review exact operation", "Expires"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("default approval show is missing %q:\n%s", want, plain)
		}
	}
}

// TestInboxEmptyStateDistinguishesNothingFromNoMatches is the regression
// test for UX-15: an empty inbox and a filter (--unread/--from) matching
// nothing real both used to print the identical "(no rows)", giving no
// signal about which case it was or what to do about it.
func TestInboxEmptyStateDistinguishesNothingFromNoMatches(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project)
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")
	must("agent", "register", "--actor", "owner", "--id", "recipient", "--json")
	must("agent", "activate", "--actor", "owner", "--id", "recipient", "--role", "AGENT", "--scope", "src", "--json")

	// Nothing addressed to this actor at all.
	if err := run("message", "inbox", "--actor", "recipient"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing addressed to you yet") {
		t.Fatalf("expected the genuinely-empty message, got: %q", out.String())
	}

	// A real message exists, but --from matches nothing.
	must("message", "post", "--actor", "owner", "--id", "msg-1", "--kind", "FYI",
		"--to", "recipient", "--subject", "Hi", "--json")
	if err := run("message", "inbox", "--actor", "recipient", "--from", "nobody"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No messages match this filter") {
		t.Fatalf("expected the filtered-empty message, got: %q", out.String())
	}
	if strings.Contains(out.String(), "Nothing addressed to you yet") {
		t.Fatal("filtered-empty must not read as genuinely empty")
	}
}

// extractResult pulls the "result" field out of a --json envelope.
func extractResult(t *testing.T, envelope []byte) []byte {
	t.Helper()
	var wrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(envelope, &wrapper); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, envelope)
	}
	return wrapper.Result
}
