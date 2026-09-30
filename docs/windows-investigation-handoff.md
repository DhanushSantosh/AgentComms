# Windows investigation handoff

Snapshot: 2026-09-30, on `dev` at `5409de5` **before** this documentation
commit. This is an investigation plan, not a claim that every Windows defect
is fixed. Recheck the branch, CI, and backlog on the Windows checkout before
acting. The main release has no planned `v0.9.0` intermediate release.

## Start here on Windows

1. Open a native PowerShell session in the AgentComms checkout. Preserve any
   local changes; do not reset, clean, or copy the Linux actor's credentials.
   Check `git status -sb`, `git branch --show-current`, `git rev-parse HEAD`,
   `git ls-remote origin refs/heads/dev`, `go version`, and `go env GOOS GOARCH
   CGO_ENABLED`. The target is the latest `dev` commit and the Go version in
   [CI](../.github/workflows/ci.yml) (currently 1.26.6). If local `dev` is
   behind, fetch and fast-forward only when the worktree is clean.
2. Check the current `dev` CI run and its Windows job with `gh run list
   --workflow ci --branch dev --limit 10` and `gh run view <run-id>`. At this
   snapshot, [run 36709399766](https://github.com/DhanushSantosh/AgentComms/actions/runs/36709399766)
   passed on `5409de5`; earlier intermittent failures remain relevant. A
   single green run does not establish that a flake is gone.
3. Establish a native red-capable feedback loop. Run the focused tests below
   once first. If a failure appears, save the test name, command, SHA, Go and
   Windows versions, elapsed time, complete redacted output, and any daemon
   log. Then repeat the *same* command/commit (`-count=10` if practical) to
   measure frequency before changing code. If local runs stay green, use a
   failing CI run as the reproduction and its artifacts as the evidence.
4. Diagnose the failing boundary, make the smallest causal fix, and add a
   regression test that would fail without it. Re-run the focused loop, then
   `go test ./...` and `go vet ./...` on Windows. A race run is useful only
   if the Windows toolchain/CGO configuration supports it; CI currently runs
   `go test -race ./...` on non-Windows jobs only. Require a first-attempt
   green Windows CI job plus the other required CI jobs before release
   promotion. Do not hide the issue with a skip or another unexplained
   timeout increase.

### Focused PowerShell commands

Run these from the repository root, separately, recording exit codes:

```powershell
go test ./internal/app -run '^TestEnsureDaemonReplacesIncompatibleDaemon(AfterSlowFixtureStart)?$' -count=1 -v -timeout 10m
go test ./internal/app -run '^(TestTaskLockConflictsOnlyForTheSameWorktree|TestDocumentNotifyRecognizesLegacySchemeMessage|TestInboxUnreadReflectsPerRecipientObligation)$' -count=1 -v -timeout 10m
go test ./internal/service -run '^TestInvocationDeliveryFailureDoesNotTerminateObligation$' -count=1 -v -timeout 10m
go test ./...
go vet ./...
```

For a red CI run, inspect its failed logs, then download the **Windows**
daemon-log artifact if present:

```powershell
gh run view <run-id> --log-failed
gh run download <run-id> -n daemon-logs-windows-latest -D .\agc-ci-diagnostics
```

The artifact is uploaded only on a failed job and only if a nonempty
`daemon.log` was found. An absent artifact or empty log is itself a fact to
record, not proof of a particular root cause. Keep the scratch directory out
of the commit. Redact tokens, keys, paths containing private user data, and
message contents before sharing logs.

## What this project does

AgentComms is a Go CLI/TUI/MCP coordination layer for humans and coding
agents. It maintains signed, append-only project events, scoped work leases,
messages, invocations, approvals, and receipts. In the default personal mode,
one per-project daemon is the writer to SQLite; clients use a Unix socket on
Linux/macOS and a named pipe on Windows. Team mode uses a shared PostgreSQL
authority and a local cache. The transport/runtime lifecycle is separate
from message eligibility: zero online runtimes does **not** mean agents
cannot communicate. See [architecture](architecture.md),
[agent onboarding](agent-onboarding.md), and the [backlog](backlog.md).

The current release tag is `v0.8.0`; `dev` is the integration branch. The
main-release promotion and verification rules are in
[releasing](releasing.md) and [release verification](release-verification.md).
Do not tag or merge a release merely because one Windows run is green.

Since `v0.8.0`, `dev` has accumulated identity/doctor behavior fixes, TUI
layout and runtime-versus-messaging clarity work, CLI reference updates,
release-backlog reconciliation, and Windows-focused test/CI instrumentation.
The exact change list is `git log --oneline v0.8.0..dev`; do not treat this
summary as a frozen release note. The [current priority queue](backlog.md#current-priority-queue-2026-09-30)
separates conditional Windows release confidence from product polish and
deliberately deferred designs. The Windows work below is the immediate
investigation; the other queue items do not silently become Windows bugs.

## Windows issue ledger

| Symptom and evidence | State at handoff | First inspection point |
| --- | --- | --- |
| `TestEnsureDaemonReplacesIncompatibleDaemonAfterSlowFixtureStart` timed out after about 40 seconds and 398 probes. The last Windows error was opening `\\.\pipe\agent-comms-...`: file not found; `daemon.log` was empty/unreadable. [Failing run 36545280545](https://github.com/DhanushSantosh/AgentComms/actions/runs/36545280545). | **Open if it recurs.** No pipe was observed, but the evidence does not yet prove why it was not bound. Do not call this merely a slow healthy daemon or increase 40 seconds blindly. | `internal/app/app_test.go` (`TestMain`, fake launch, replacement test), `internal/app/cmd_misc.go` (`ensureDaemon` and log tail), `internal/app/app.go` (budgets), `internal/daemon` and Windows IPC implementation. |
| Three app tests failed tempdir cleanup because `personal-authority.db` was still open on Windows. [Failing run 36556251703](https://github.com/DhanushSantosh/AgentComms/actions/runs/36556251703). | **Mitigated, monitor.** `fa6a7e9` made test cleanup wait for daemon exit with a Windows-scaled 60-second budget; later Windows CI passed. Reopen on the same signature. | `internal/app/app_test.go` (`cleanupProjectDaemon`, `testDaemonShutdownBudget`). |
| `TestInvocationDeliveryFailureDoesNotTerminateObligation` once raced a real background retry against an explicit delivery attempt on a loaded Windows runner. The same SHA passed on rerun. | **Conditional open.** Separate test timing from a product delivery-state race if it recurs. | `internal/service/invocation_test.go:230` and the delivery coordinator. |
| Process-takeover ancestry false positive and partial session-discovery JSON during concurrent writes. | **Mitigated 2026-09-24, monitor.** Creation-time ancestry and staged-write regressions landed; use the detailed backlog entries if either signature returns. | [Test / CI infrastructure](backlog.md#test--ci-infrastructure). |
| Filesystem-heavy work was drastically slower on `windows-latest` than local Linux in measured tests (for example, concurrent reconcile 9.32 s versus 0.18 s). | **Performance constraint, not a diagnosis of every failure.** Budget work proportional to its units; diagnose no-pipe failures separately. | [Windows CI details](backlog.md#test--ci-infrastructure), `internal/projectlifecycle` and reconcile tests. |

The CI test matrix runs `go test ./...` and `go vet ./...` on Ubuntu,
Windows, and macOS. On failure it captures nonempty daemon logs as
`daemon-logs-windows-latest`. Cross-build, security, PostgreSQL integration,
docs, and landing jobs are separate gates. See [CI](../.github/workflows/ci.yml).

## Completion criteria and team handoff

- For a reproduced issue: demonstrate a failing pre-fix case and a passing
  post-fix case on native Windows, including repeated runs sufficient to
  characterize intermittency. Keep the regression and record the causal
  diagnosis in the backlog; distinguish an environmental runner limitation
  from a product defect.
- For mitigated issues: verify their named tests and monitor CI. Do not label
  them permanently resolved solely because the latest run is green.
- Before the main release: run the full supported Windows suite, inspect
  first-attempt CI results across the matrix and release gates, exercise a
  basic local `agc` init/status/message flow on Windows, and check
  [release verification](release-verification.md). New failures are triaged
  by severity and evidence, not folded into a blanket “Windows flake.”
- Coordinate through this project's `agc` instance: check `agc status`,
  `agc attention`, and `agc message inbox`; create/claim scoped work before code edits
  and post progress plus exact CI links. If Windows is a separate checkout,
  establish its own authorized actor/profile rather than copying key material.
  A runtime registration is not required to exchange project messages.
