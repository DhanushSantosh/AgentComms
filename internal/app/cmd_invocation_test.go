package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInvocationRequestExplainsPendingConsumer is the regression test for
// UX-08's receipt half: an invocation with no runtime observed used to
// leave Consumer and Runtime blank in the plain-text receipt and offer
// only a generic "inspect the invocation" hint -- indistinguishable from a
// real failure. Consumer must show the actually-resolved mode (even when
// --consumer was never passed) and Runtime/hint must name this specific,
// ordinary "queued; no consumer observed yet" case.
func TestInvocationRequestExplainsPendingConsumer(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")
	must("agent", "register", "--id", "builder", "--json")
	must("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--json")

	// No runtime for "builder" is online, and --consumer is deliberately
	// omitted so the receipt must resolve it from the target's policy
	// default rather than showing it blank.
	if err := run("invocation", "request", "--id", "inv-queued", "--to", "builder", "--instruction", "say hi"); err != nil {
		t.Fatalf("invocation request: %v\n%s", err, stderr.String())
	}
	plain := out.String()
	if strings.Contains(plain, "Consumer:  \n") || strings.Contains(plain, "Consumer: \n") {
		t.Fatalf("Consumer field must not be blank:\n%s", plain)
	}
	if !strings.Contains(plain, "EITHER") {
		t.Fatalf("Consumer field should resolve to the policy default EITHER, got:\n%s", plain)
	}
	if !strings.Contains(plain, "none observed yet") {
		t.Fatalf("Runtime field should explain no runtime has claimed it yet, got:\n%s", plain)
	}
	if !strings.Contains(plain, "invocation next") && !strings.Contains(plain, "invocation listen") {
		t.Fatalf("hint should name a concrete next step (invocation next/listen), got:\n%s", plain)
	}
}

// TestInvocationListEmptyStateDistinguishesNothingFromNoMatches is the
// regression test for UX-15: a project with zero invocations and a
// --status/--to filter matching nothing both used to print the identical
// "(no rows)".
func TestInvocationListEmptyStateDistinguishesNothingFromNoMatches(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")

	if err := run("invocation", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No invocations yet") {
		t.Fatalf("expected the genuinely-empty message, got: %q", out.String())
	}

	must("agent", "register", "--id", "builder", "--json")
	must("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--json")
	must("invocation", "request", "--id", "inv-1", "--to", "builder", "--instruction", "say hi", "--json")

	if err := run("invocation", "list", "--status", "COMPLETED"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No invocations match this filter") {
		t.Fatalf("expected the filtered-empty message, got: %q", out.String())
	}
	if strings.Contains(out.String(), "No invocations yet") {
		t.Fatal("filtered-empty must not read as genuinely empty")
	}
}

// TestInvocationRequestOutcomeHintsNameOnlyRealFlags is the regression
// test for a bug an independent review caught before release:
// invocationRequestOutcomeHint's PENDING_CONSUMER hint suggested
// `invocation listen --id <id>` (that command has no --id flag at all --
// it isn't scoped to one invocation) and its AMBIGUOUS hint suggested
// `invocation policy set --to <target>` (that flag is --agent, not --to).
// This checks both the hint text directly and, live, that every command
// name/flag combination the hints mention for every outcome actually
// exists on the real cobra command tree, so a future flag rename can't
// silently reintroduce the same class of bug.
func TestInvocationRequestOutcomeHintsNameOnlyRealFlags(t *testing.T) {
	for _, outcome := range []string{"PENDING_CONSUMER", "UNAVAILABLE", "AMBIGUOUS", "SUCCEEDED"} {
		_, hint := invocationRequestOutcomeHint(outcome, "", "inv-1", "builder")
		if strings.Contains(hint, "listen --id") {
			t.Fatalf("%s hint suggests unsupported `listen --id`: %s", outcome, hint)
		}
		if strings.Contains(hint, "policy set --to") {
			t.Fatalf("%s hint suggests unsupported `policy set --to`: %s", outcome, hint)
		}
	}
	_, pendingHint := invocationRequestOutcomeHint("PENDING_CONSUMER", "", "inv-1", "builder")
	if !strings.Contains(pendingHint, "invocation listen --runtime") {
		t.Fatalf("PENDING_CONSUMER hint should suggest the real `listen --runtime` flag: %s", pendingHint)
	}
	_, ambiguousHint := invocationRequestOutcomeHint("AMBIGUOUS", "", "inv-1", "builder")
	if !strings.Contains(ambiguousHint, "policy set --agent") {
		t.Fatalf("AMBIGUOUS hint should suggest the real `policy set --agent` flag: %s", ambiguousHint)
	}

	// Live cross-check: every flag named above must actually exist.
	root := (&cli{}).invocationCmd()
	listen, _, err := root.Find([]string{"listen"})
	if err != nil {
		t.Fatal(err)
	}
	if listen.Flags().Lookup("runtime") == nil {
		t.Fatal("invocation listen has no --runtime flag")
	}
	if listen.Flags().Lookup("id") != nil {
		t.Fatal("invocation listen unexpectedly has an --id flag -- the hint's own reasoning for omitting it is now stale")
	}
	policySet, _, err := (&cli{}).invocationPolicyCmd().Find([]string{"set"})
	if err != nil {
		t.Fatal(err)
	}
	if policySet.Flags().Lookup("agent") == nil {
		t.Fatal("invocation policy set has no --agent flag")
	}
	if policySet.Flags().Lookup("to") != nil {
		t.Fatal("invocation policy set unexpectedly has a --to flag -- the hint's own reasoning for using --agent is now stale")
	}
}

// TestInvocationInspectShowsInstructionByDefault is the regression test for
// UX-08's inspect half: instruction (what the invocation actually asked
// for) was only visible under --details/--json even though it's the first
// thing anyone inspecting one invocation by ID wants to see.
func TestInvocationInspectShowsInstructionByDefault(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal", "--json")
	must("agent", "register", "--id", "builder", "--json")
	must("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src", "--json")
	must("invocation", "request", "--id", "inv-inspect", "--to", "builder", "--instruction", "review the release notes carefully", "--json")

	if err := run("invocation", "inspect", "--id", "inv-inspect"); err != nil {
		t.Fatalf("invocation inspect: %v\n%s", err, stderr.String())
	}
	plain := out.String()
	if !strings.Contains(plain, "review the release notes carefully") {
		t.Fatalf("default inspect view is missing the instruction:\n%s", plain)
	}
	if !strings.Contains(plain, "not yet claimed") {
		t.Fatalf("default inspect view should explain an unclaimed runtime, got:\n%s", plain)
	}
}

// TestAttentionSurfacesPendingConsumerInvocationAfterGrace is the
// regression test for UX-08's attention half: `attention` counted
// Status=="WAITING" invocations only, missing entirely the earlier
// "requested, no consumer has ever claimed it" (Status=="PENDING") case
// that a no-runtime `invocation request` produces -- the audit's own
// reproduction. A grace period keeps an invocation that was *just*
// requested from generating a false alarm identical to one nobody has
// picked up in a long time.
func TestAttentionSurfacesPendingConsumerInvocationAfterGrace(t *testing.T) {
	project := t.TempDir()
	cleanupProjectDaemon(t, project)
	t.Setenv("AGENT_COMMS_CONFIG_DIR", filepath.Join(project, "user"))
	t.Setenv("AGENT_COMMS_CREDENTIAL_DIR", filepath.Join(project, "credentials"))
	var out, stderr bytes.Buffer
	run := func(args ...string) error {
		t.Helper()
		out.Reset()
		stderr.Reset()
		args = append(args, "--project", project, "--actor", "owner", "--json")
		return Run(args, &out, &stderr)
	}
	must := func(args ...string) {
		t.Helper()
		if err := run(args...); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, stderr.String())
		}
	}
	must("init", "--non-interactive", "--owner", "owner", "--mode", "personal")
	must("agent", "register", "--id", "builder")
	must("agent", "activate", "--id", "builder", "--role", "AGENT", "--scope", "src")
	must("invocation", "request", "--id", "inv-pending", "--to", "builder", "--instruction", "say hi")

	// Within the grace period: must not yet appear.
	old := attentionPendingConsumerGrace
	attentionPendingConsumerGrace = time.Hour
	t.Cleanup(func() { attentionPendingConsumerGrace = old })
	must("attention")
	var withinGrace map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &withinGrace); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	pending := withinGrace["pending_consumer_invocations"].(map[string]any)
	if len(pending) != 0 {
		t.Fatalf("within the grace period, a just-requested invocation must not be flagged: %s", out.String())
	}

	// Past the grace period: must appear.
	attentionPendingConsumerGrace = 0
	must("attention")
	var pastGrace map[string]any
	if err := json.Unmarshal(extractResult(t, out.Bytes()), &pastGrace); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	pending = pastGrace["pending_consumer_invocations"].(map[string]any)
	if _, ok := pending["inv-pending"]; !ok {
		t.Fatalf("past the grace period, the no-runtime invocation must be flagged: %s", out.String())
	}
}
