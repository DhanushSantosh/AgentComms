# Release UX audit: reproduction evidence

Captured 2026-09-05 against a fresh CLI build from commit `93d0c35`. This file preserves selected observed outputs and fixture setup, not a fabricated transcript of every command. Long signed JSON receipts are summarized explicitly to avoid noise. See [audit and plan](2026-09-05-release-ux-audit.md) for impact, source evidence, and limitations.

## Isolation and verification

The binary was built with:

```sh
go build -o /tmp/agentcomms-ux-audit-wxb4L0/agent-comms ./cmd/agent-comms
```

All fixture commands used that binary with `--project /tmp/agentcomms-ux-audit-wxb4L0/project`. Their subprocess environments set `AGENT_COMMS_CONFIG_DIR=/tmp/agentcomms-ux-audit-wxb4L0/config` and `AGENT_COMMS_CREDENTIAL_DIR=/tmp/agentcomms-ux-audit-wxb4L0/credentials`, so the real project/identity store was not used. Credentials and the entire fixture are temporary and are not included in this report. New reproductions should create a new temporary directory instead of reusing these IDs in a real project.

```text
go test ./internal/tui ./internal/cliui ./internal/onboarding ./internal/app
ok  .../internal/tui         58.684s
ok  .../internal/cliui       (cached)
ok  .../internal/onboarding  (cached)
ok  .../internal/app         174.955s
```

## E1: no project and bootstrap conflict (UX-01, UX-03)

Before initialization, `status` exited 2:

```text
error [VALIDATION]: lstat /tmp/agentcomms-ux-audit-wxb4L0/project/.agent-comms: no such file or directory
hint: Run the command with --help to review required flags and accepted values.
```

With a pre-created empty `.agents/`, `init --owner audit-owner --yes --non-interactive` exited 2:

```text
error [VALIDATION]: .agents already exists; remove or rename it before initializing Agent Comms
hint: Run the command with --help to review required flags and accepted values.
```

Removing only that empty fixture directory allowed the same init to succeed. Fresh status then showed one agent, zero runtimes/tasks/invocations/approvals, and verified integrity, but no active actor or setup checklist.

## E2: registration does not mean readiness (UX-03)

```text
$ agent register --id audit-agent --principal-type AGENT --non-interactive
Agent registered

Agent          audit-agent
Registered by  audit-owner
Profile        ac-e6499536-17fb-49ec-b15b-b234326c4e90:audit-agent
Project        /tmp/agentcomms-ux-audit-wxb4L0/project
Sequence       3
```

The following `profile current` still reported `audit-owner` via `session_profile`. A task create explicitly using `--actor audit-agent` before activation exited 3:

```text
error [AUTHORIZATION]: active principal required
hint: Check the active actor, profile, role, and granted scopes.
```

Fixture owner activation as Contributor with scope `*` made the agent usable. No elevated role was granted in the fixture.

## E3: hidden message content (UX-04)

Posted a message to audit-agent with subject `Please review the release readiness plan` and body `This is the full message body.` Both default inbox and redirected `--output plain` showed:

```text
ID                       KIND  FROM         STATUS
msg-1788594370361795997  FYI   audit-owner  DELIVERED
```

`--details` displayed the missing subject and body in a nested Details section. `message show --id missing` failed with `unknown flag: --id`; `message --help` lists no `show` command. This is a discoverability limitation, not a claim that `show` is documented as supported.

## E4: unstable limits and recipient-unread mismatch (UX-05)

Added eight FYI fixtures `audit-fyi-0` through `audit-fyi-7`. Repeated `message inbox --actor audit-agent --limit 1 --json` twelve times without changing state. Selected ID frequencies:

```text
msg-1788594370361795997: 1
audit-fyi-5: 1
audit-fyi-0: 4
audit-fyi-2: 1
audit-fyi-7: 2
audit-fyi-3: 1
audit-fyi-1: 1
audit-fyi-6: 1
```

Then posted ACTION `audit-two-recipients` to audit-agent and audit-owner, and acknowledged it as audit-agent. `message inbox --actor audit-agent --unread --json` still included:

```json
{
  "id": "audit-two-recipients",
  "status": "OPEN",
  "recipients": [
    {"principal": "audit-agent", "status": "ACCEPTED"},
    {"principal": "audit-owner", "status": "PENDING"}
  ]
}
```

Only relevant fields are shown; the actual record also contained subject, body, timestamp, kind, and sender/recipient IDs.

## E5: a generated bound approval's default view (UX-06)

Requested approval through the actual contract workflow:

```sh
agent-comms message post --actor audit-agent --kind CONTRACT \
  --id audit-contract --to audit-owner \
  --subject 'Review exact operation' --body 'Only publish these reviewed terms' \
  --request-approval --approval-id bound-contract --non-interactive
```

`approval show --id bound-contract --actor audit-agent` displayed:

```text
Approval bound-contract

Tier       ORCHESTRATOR
Status     PENDING
Requester  audit-agent
Action     contract:audit-contract
Reason
```

With `--details`, the same record additionally showed affected principal audit-owner, expiry `2026-09-06T07:47:13.385307371Z`, the SHA-256 subject digest, and canonical JSON including actor, type `message.post`, id, kind, recipients, subject, and exact body. The bound approval was not approved or used in this probe.

## E6: partial notification success hidden in quiet mode (UX-07)

```sh
agent-comms document create --actor audit-agent --id audit-doc \
  --title 'Review me' --body 'Meaningful contents' \
  --decision --notify nonexistent-agent --json
```

Observed exit 0; stdout was a valid `ok:true` `document.create` envelope containing the committed document event, without structured notification failure or warnings. Stderr contained:

```text
warning: --notify nonexistent-agent: active message recipient nonexistent-agent is required
```

Repeating with a distinct document ID `audit-doc-quiet` and `--quiet` again returned exit 0 and `ok:true`; stderr was empty. Both documents appeared in `document list`. The issue is lost partial-outcome information, not failure to create the document.

## E7: no consumer and inconsistent attention (UX-08)

An invocation from audit-owner to activated audit-agent with no runtime returned:

```text
Invocation requested

Invocation  inv-1788594371209530976
Target      audit-agent
Priority    NORMAL
Consumer
Delivery    PENDING_CONSUMER
Runtime

Next: Inspect the invocation to review delivery evidence and lifecycle state.
```

At that time, `attention` reported Waiting invocations 0, Failed deliveries 0, and Degraded runtimes 0. It did show the separate pending approval. The live TUI overview displayed the invocation as pending delivery to audit-agent, so the surfaces differ. A queued request is not automatically a failure; the proposal adds an evidence-based readiness explanation and an age threshold for intervention.

## E8: task prerequisites and scope-only claim (UX-09, UX-10)

`task create --id audit-task --title 'First task' --resource 'src/**'` as the active audit-agent failed with:

```text
error [VALIDATION]: title, repository, branch, and resources are required
hint: Run the command with --help to review required flags and accepted values.
```

Adding `--branch dev` succeeded. Claiming it without `--worktree` succeeded and printed an empty Worktree field, plus lease expiry. `task show` displayed title/status/owner/branch and empty Worktree but omitted resources and lease expiry from its primary summary.

## E9: live terminal palette (UX-11)

Opened the freshly built TUI in a Linux PTY, with the isolated project and audit-agent identity. Typed `/new`. It displayed six matches, starting with `new task`, then `new agent`, `new message`, `new invocation`, `new runtime`, and `new document`. Sent Down then Enter. The TUI opened `EDIT / Create task`, not the second result. The implementation confirms Enter always applies the first match and has no arrow-selection index.

The task form was not submitted. Escape returned to the task view; Ctrl+C closed the TUI. This is terminal interaction evidence, not a screenshot-based color/contrast assessment.

## E10: POSIX checksum fallback fixture (UX-02)

This tests the exact pipeline/control-flow shape without downloading or executing any release asset:

```sh
sha256sum() { return 127; }
shasum() { printf 'expected-digest  fixture\n'; }
checksum_of() {
  sha256sum fixture 2>/dev/null | awk '{print $1}' ||
    shasum -a 256 fixture | awk '{print $1}'
}
value=$(checksum_of)
printf 'checksum=<%s> exit=%s\n' "$value" "$?"
```

Observed with `sh`:

```text
checksum=<> exit=0
```

The missing-command failure is masked by the successful `awk` pipeline stage. Native macOS installation was not run.

## Remaining evidence boundaries

UX-12 through UX-16 rely primarily on source/contract review as indicated individually in the audit. Claims about screen readers, Windows/macOS rendering, live remote recovery, and large-state latency require the proposed release tests. Existing browser tests were read, not rerun. Existing unit/integration tests passing establishes a useful baseline, not a clean bill of UX health.
