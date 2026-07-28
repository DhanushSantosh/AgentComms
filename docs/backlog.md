# Backlog

Deferred, real work items surfaced during development but intentionally not
built yet — each was a deliberate decision to defer, not an oversight. When
one is picked up, remove it from here and note the landing commit.

## Security / governance

- **Approval self-approval is not prevented.** A human (or any elevated
  actor) can both request and approve the same approval record today —
  nothing requires the approver to differ from the requester, for any
  approval, not just orchestrator grants. The two-step orchestrator-grant
  gate (`internal/protocol/transitions.go`'s `hasHumanApproval`) still
  defends against what it was built for (an unattended agent completing the
  whole escalation alone), since self-approval still requires a human to
  consciously type both steps. But it's a real, separate hardening if
  stronger multi-party control is wanted: require `approval.Approver !=
  approval.Requester`, or specifically that the *activating* actor differ
  from whoever requested the approval it's relying on.

- **`agent.rename` display-name impersonation is an accepted, low-severity
  risk, not fixed.** An orchestrator (including an AGENT-principal one) can
  rename another principal's cosmetic `DisplayName` to impersonate a
  trusted human in TUI/CLI listings. IDs stay authoritative underneath —
  this is a social-engineering vector, not a privilege bypass — so it was
  deliberately left as ordinary owner-or-orchestrator-gated rather than
  restricted further, to avoid breaking legitimate managerial renaming.
  Revisit if it's ever actually exploited.

## Elevated key / TUI

- **No TUI passphrase-entry UI.** Once a HUMAN principal registers an
  elevated key (`agent elevate-key`), completing an orchestrator grant or
  HUMAN-tier approval from the TUI now refuses cleanly with a "run this
  from the CLI instead" error (fixed — see `internal/app/passphrase.go`'s
  `nonInteractivePassphrasePrompt`), rather than risking a hang racing
  bubbletea's own raw-mode stdin reader. But that's a safety fix, not the
  real UX: building an actual masked-password bubbletea form so this can be
  completed in-TUI is still open. Same shape as the RFC 0011 TUI
  confirmation-flow gap below, and could reasonably be solved together.

## Test / CI infrastructure

- **Postgres-backend authorization logic doesn't run in CI.**
  `internal/authority`'s integration tests (`postgres_integration_test.go`,
  `elevated_key_integration_test.go`) all skip without a live
  `AGENT_COMMS_TEST_POSTGRES_URL`, and no CI workflow sets one — this is
  exactly how a real bug (`agent.rename` silently broken in Postgres mode,
  fixed 2026-07-28) shipped and stayed hidden. A lightweight,
  non-DB registry-completeness test now exists
  (`internal/authority/decode_payload_test.go`) and would catch that
  specific class of bug, but it doesn't cover behavioral correctness (e.g.
  the elevated-key SQL queries in `scopedElevationState`) the way a real
  database run would. Wire an ephemeral Postgres service into CI (e.g.
  `services: postgres:` in a GitHub Actions workflow) so these tests
  actually execute automatically.

- **`internal/protocol`'s `ValidateTransition` is mostly untested
  in-package.** `transitions_test.go` covers the elevated-key/orchestrator
  work directly; the other ~800 lines (task lifecycle, invocation
  lifecycle, message routing, resource-overlap checks) have only indirect
  coverage through `internal/service`/`internal/app`/`internal/mcp`/`internal/tui`
  integration tests. Not urgent — the indirect coverage is real — but a gap
  worth closing with direct unit tests for the highest-value paths.

## Possibly-a-bug, not yet root-caused

- **`doctor`'s `REVOKED_AGENT_HAS_OPEN_WORK` warning appears to fire even
  when all of the revoked agent's invocations are already terminal**
  (`CANCELLED`/`COMPLETED`), observed live against `DummyTestProject`
  2026-07-28. Low priority (cosmetic, not a data-integrity issue —
  `verify`/`integrity` were unaffected both times), but the check's actual
  logic (wherever it counts "open" invocations for a revoked agent) hasn't
  been read yet to confirm whether it's checking the wrong field or a
  genuinely stale condition.

## Cross-reference

- [RFC 0012](rfcs/0012-agent-identity-deletion-and-key-fingerprinting.md) —
  implemented: revoked principals can be deliberately deleted by a HUMAN
  through the elevated CLI path, while every new event permanently attests
  the verified actor-key fingerprint so a reused ID's occupants remain
  distinguishable.
- [RFC 0011's "Known gaps"](rfcs/0011-managed-project-lifecycle-and-upgrades.md#known-gaps) —
  the TUI's one-confirmation upgrade-approval UX isn't built; a
  confirmation-required plan just blocks the TUI from launching instead.
