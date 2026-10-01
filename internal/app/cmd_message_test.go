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
	must("agent", "register", "--actor", "owner", "--id", "claude-recipient-a")
	must("agent", "activate", "--actor", "owner", "--id", "claude-recipient-a", "--role", "AGENT", "--scope", "src")
	must("agent", "register", "--actor", "owner", "--id", "claude-recipient-b")
	must("agent", "activate", "--actor", "owner", "--id", "claude-recipient-b", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "two-recipients", "--kind", "ACTION",
		"--to", "claude-recipient-a", "--to", "claude-recipient-b", "--subject", "Please review", "--body", "Details")

	// recipient-a acknowledges its own copy; recipient-b never acts.
	must("message", "ack", "--actor", "claude-recipient-a", "--id", "two-recipients")

	must("message", "inbox", "--actor", "claude-recipient-a", "--unread")
	var recipientAInbox map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &recipientAInbox); err != nil {
		t.Fatal(err)
	}
	if _, stillUnread := recipientAInbox["two-recipients"]; stillUnread {
		t.Fatalf("recipient-a (already acknowledged) still shown as unread purely because recipient-b hasn't acted: %s", out.String())
	}

	must("message", "inbox", "--actor", "claude-recipient-b", "--unread")
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
	must("agent", "register", "--actor", "owner", "--id", "claude-recipient")
	must("agent", "activate", "--actor", "owner", "--id", "claude-recipient", "--role", "AGENT", "--scope", "src")
	for _, id := range []string{"msg-a", "msg-b", "msg-c", "msg-d", "msg-e", "msg-f", "msg-g", "msg-h"} {
		must("message", "post", "--actor", "owner", "--id", id, "--kind", "FYI", "--to", "claude-recipient", "--subject", id)
	}

	first := ""
	for i := 0; i < 12; i++ {
		must("message", "inbox", "--actor", "claude-recipient", "--limit", "1")
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
	must("agent", "register", "--actor", "owner", "--id", "claude-recipient")
	must("agent", "activate", "--actor", "owner", "--id", "claude-recipient", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "readable", "--kind", "FYI",
		"--to", "claude-recipient", "--subject", "A subject worth reading", "--body", "The full body text")

	must("message", "show", "--actor", "claude-recipient", "--id", "readable")
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

	if err := run("message", "show", "--actor", "claude-recipient", "--id", "does-not-exist"); err == nil {
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
	must("agent", "register", "--actor", "owner", "--id", "claude-reviewer")
	must("agent", "activate", "--actor", "owner", "--id", "claude-reviewer", "--role", "AGENT", "--scope", "src")
	must("message", "post", "--actor", "owner", "--id", "bound-contract", "--kind", "CONTRACT",
		"--to", "claude-reviewer", "--subject", "Review exact operation", "--body", "Only publish these reviewed terms",
		"--request-approval", "--approval-id", "bound-contract-approval", "--non-interactive")

	// Plain human output is the actual bar the audit found too low --
	// --json always carried the full struct regardless of this bug.
	if err := Run([]string{"approval", "show", "--project", project, "--actor", "claude-reviewer",
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
	must("agent", "register", "--actor", "owner", "--id", "claude-recipient", "--json")
	must("agent", "activate", "--actor", "owner", "--id", "claude-recipient", "--role", "AGENT", "--scope", "src", "--json")

	// Nothing addressed to this actor at all.
	if err := run("message", "inbox", "--actor", "claude-recipient"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing addressed to you yet") {
		t.Fatalf("expected the genuinely-empty message, got: %q", out.String())
	}

	// A real message exists, but --from matches nothing.
	must("message", "post", "--actor", "owner", "--id", "msg-1", "--kind", "FYI",
		"--to", "claude-recipient", "--subject", "Hi", "--json")
	if err := run("message", "inbox", "--actor", "claude-recipient", "--from", "nobody"); err != nil {
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

// RFC 0039 section 4: a principal may be named by its display name, and
// the signed event must still record the canonical actor ID.
func TestMessagePostAcceptsADisplayNameAndRecordsTheActorID(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	// --actor is explicit throughout: once this project has more than one
	// locally-registered identity, the machine-wide active-profile default
	// is ambiguous and the CLI rightly refuses to sign. Leaving it implicit
	// made this pass locally and fail in CI, where the profile state differs.
	if err := run("agent", "register", "--actor", "owner", "--provider", "claude", "--display-name", "Atlas"); err != nil {
		t.Fatalf("register: %v (%s)", err, stderr.String())
	}
	if err := run("agent", "activate", "--actor", "owner", "--id", "claude", "--role", "Engineer", "--scope", "*"); err != nil {
		t.Fatalf("activate: %v (%s)", err, stderr.String())
	}

	// Addressed by display name...
	if err := run("message", "post", "--actor", "owner", "--to", "Atlas",
		"--kind", "FYI", "--subject", "hello", "--body", "by display name"); err != nil {
		t.Fatalf("post by display name: %v (%s)", err, stderr.String())
	}
	// ...but the event records the actor ID, not "Atlas".
	posted := stdout.String()
	if strings.Contains(posted, `"Atlas"`) {
		t.Errorf("the signed event must not record a display name:\n%s", posted)
	}
	if !strings.Contains(posted, "claude") {
		t.Errorf("the event should record the canonical actor ID:\n%s", posted)
	}
	// The recipient sees it in their inbox under their own identity.
	if err := run("message", "inbox", "--actor", "claude"); err != nil {
		t.Fatalf("inbox: %v (%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hello") {
		t.Errorf("recipient should have the message:\n%s", stdout.String())
	}
	// An unknown reference is refused rather than silently delivered.
	if err := run("message", "post", "--actor", "owner", "--to", "Nobody",
		"--kind", "FYI", "--subject", "x", "--body", "y"); err == nil {
		t.Error("an unresolvable recipient must be refused")
	}
}

// A negative --expires-in once fell through the "> 0" branch and left
// ExpiresAt nil, which RFC 0037 reads as "never expires" -- so asking for a
// window that had already closed produced a permanent approval. The
// protocol guard could not catch it, because the CLI never sent a past
// timestamp for it to reject.
func TestApprovalRequestRefusesANegativeExpiry(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := run("approval", "request", "--actor", "owner", "--id", "past",
		"--action", "demo:x", "--tier", "ORCHESTRATOR", "--reason", "r", "--expires-in", "-1h"); err == nil {
		t.Fatal("a negative --expires-in must be refused, not silently become no-expiry")
	}
	// Zero still means "not specified", and a positive window still works.
	if err := run("approval", "request", "--actor", "owner", "--id", "future",
		"--action", "demo:y", "--tier", "ORCHESTRATOR", "--reason", "r", "--expires-in", "1h"); err != nil {
		t.Fatalf("a positive window must still be accepted: %v (%s)", err, stderr.String())
	}
	if err := run("approval", "request", "--actor", "owner", "--id", "none",
		"--action", "demo:z", "--tier", "ORCHESTRATOR", "--reason", "r"); err != nil {
		t.Fatalf("omitting --expires-in must still be accepted: %v (%s)", err, stderr.String())
	}
}

// RFC 0039 section 4 names four places a principal can be referenced:
// --to, --actor, task ownership and invocation targets. The first
// implementation only did message recipients, which codex-main caught by
// grepping for the resolver rather than trusting the RFC.
func TestDisplayNamesResolveForTaskAndInvocationTargetsToo(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := run("agent", "register", "--actor", "owner", "--provider", "claude", "--display-name", "Atlas"); err != nil {
		t.Fatalf("register: %v (%s)", err, stderr.String())
	}
	if err := run("agent", "activate", "--actor", "owner", "--id", "claude", "--role", "Engineer", "--scope", "*"); err != nil {
		t.Fatalf("activate: %v (%s)", err, stderr.String())
	}
	if err := run("task", "create", "--actor", "owner", "--id", "t1", "--title", "T", "--branch", "main", "--resource", "src"); err != nil {
		t.Fatalf("task create: %v (%s)", err, stderr.String())
	}

	if err := run("task", "offer", "--actor", "owner", "--id", "t1", "--to", "Atlas"); err != nil {
		t.Errorf("task offer should accept a display name: %v (%s)", err, stderr.String())
	}
	if err := run("invocation", "request", "--actor", "owner", "--to", "Atlas", "--instruction", "do it"); err != nil {
		t.Errorf("invocation request should accept a display name: %v (%s)", err, stderr.String())
	}
	// The receipt must show the identity actually recorded, not the label
	// the caller typed.
	if strings.Contains(stdout.String(), `"target":"Atlas"`) {
		t.Errorf("the invocation must record the actor ID, not the display name:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"target":"claude"`) {
		t.Errorf("expected the resolved target in the event:\n%s", stdout.String())
	}
	// An unresolvable reference is refused at each site.
	if err := run("invocation", "request", "--actor", "owner", "--to", "Nobody", "--instruction", "x"); err == nil {
		t.Error("an unresolvable invocation target must be refused")
	}
}

// The three principal-reference sites codex-main's review of 1b7aa4c found
// still unresolved, plus the regression the previous fix introduced.
func TestRemainingPrincipalReferenceSitesResolveDisplayNames(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	if err := run("agent", "register", "--actor", "owner", "--provider", "claude", "--display-name", "Atlas"); err != nil {
		t.Fatalf("register: %v (%s)", err, stderr.String())
	}
	if err := run("agent", "activate", "--actor", "owner", "--id", "claude", "--role", "Engineer", "--scope", "*"); err != nil {
		t.Fatalf("activate: %v (%s)", err, stderr.String())
	}

	// --actor by display name: the command must run as claude, not fail.
	if err := run("task", "create", "--actor", "Atlas", "--id", "t1", "--title", "T", "--branch", "main", "--resource", "src"); err != nil {
		t.Errorf("--actor should accept a display name: %v (%s)", err, stderr.String())
	}
	if strings.Contains(stdout.String(), `"actor":"Atlas"`) {
		t.Errorf("the event must record the actor ID, not the display name:\n%s", stdout.String())
	}

	// handoff by display name, and --accept must ignore --to entirely
	// rather than resolving a flag it discards.
	if err := run("task", "claim", "--actor", "owner", "--id", "t1"); err != nil {
		t.Fatalf("claim: %v (%s)", err, stderr.String())
	}
	if err := run("task", "handoff", "--actor", "owner", "--id", "t1", "--to", "Atlas", "--summary", "over to you"); err != nil {
		t.Errorf("handoff should accept a display name: %v (%s)", err, stderr.String())
	}
	if err := run("task", "handoff", "--actor", "Atlas", "--id", "t1", "--accept", "--to", "no-such-principal", "--summary", "mine"); err != nil {
		t.Errorf("--accept ignores --to, so a bad name there must not reject it: %v (%s)", err, stderr.String())
	}

	// invocation list --to by display name must find the invocation whose
	// stored Target is the canonical ID, not silently return nothing.
	if err := run("invocation", "request", "--actor", "owner", "--to", "Atlas", "--instruction", "do it"); err != nil {
		t.Fatalf("invocation request: %v (%s)", err, stderr.String())
	}
	if err := run("invocation", "list", "--to", "Atlas"); err != nil {
		t.Fatalf("invocation list: %v (%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "inv-") {
		t.Errorf("filtering by display name must match the canonical target:\n%s", stdout.String())
	}
}

// TestExplicitActorNamesTheCandidatesWhenTheDisplayNameIsAmbiguous is
// the regression test for what codex-main caught reviewing 1b7aa4c:
// app.go resolved --actor through model.ResolvePrincipal but discarded
// the error, so naming a display name two principals share fell through
// to the generic unknown-actor path. RFC 0039 section 4 promises the
// candidates instead -- and this is the one source where the user typed
// the reference themselves and can act on that list.
func TestExplicitActorNamesTheCandidatesWhenTheDisplayNameIsAmbiguous(t *testing.T) {
	project := t.TempDir()
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	cleanupProjectDaemon(t, project)
	var stdout, stderr bytes.Buffer
	run := func(args ...string) error {
		stdout.Reset()
		stderr.Reset()
		return Run(append(args, "--project", project, "--json"), &stdout, &stderr)
	}
	if err := run("init", "--non-interactive", "--owner", "owner"); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"claude", "codex"} {
		if err := run("agent", "register", "--actor", "owner", "--provider", provider, "--display-name", "Atlas"); err != nil {
			t.Fatalf("register %s: %v (%s)", provider, err, stderr.String())
		}
		if err := run("agent", "activate", "--actor", "owner", "--id", provider, "--role", "Engineer", "--scope", "*"); err != nil {
			t.Fatalf("activate %s: %v (%s)", provider, err, stderr.String())
		}
	}

	err := run("status", "--actor", "Atlas")
	if err == nil {
		t.Fatal("a display name shared by two principals must not silently resolve to either")
	}
	message := err.Error() + stdout.String() + stderr.String()
	for _, want := range []string{"claude", "codex", "actor ID"} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal should name %q so the reader can pick: %s", want, message)
		}
	}

	// An unambiguous display name still resolves, and a reference that
	// matches nobody still reaches the ordinary unknown-actor path rather
	// than being turned into a resolution error.
	if err := run("agent", "rename", "--actor", "owner", "--id", "codex", "--display-name", "Beacon"); err != nil {
		t.Fatalf("rename: %v (%s)", err, stderr.String())
	}
	if err := run("status", "--actor", "Beacon"); err != nil {
		t.Fatalf("an unambiguous display name must still resolve: %v (%s)", err, stderr.String())
	}
	// A reference matching nobody is still left to the paths that already
	// report unknown actors -- the ones that sign. status is a read and
	// never gets that far, which is exactly why resolution failure is not
	// turned into an error here for the not-found case.
	err = run("message", "post", "--actor", "nobody-at-all", "--to", "Atlas",
		"--kind", "FYI", "--subject", "x", "--body", "y")
	if err == nil {
		t.Error("signing as an unregistered actor must be refused")
	} else if strings.Contains(err.Error(), "display name of") {
		t.Errorf("an unknown actor is not an ambiguous one: %v", err)
	}
}
