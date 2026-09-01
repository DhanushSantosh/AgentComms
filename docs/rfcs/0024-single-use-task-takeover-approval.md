# RFC 0024: Single-use task-takeover approval

## Status

**Accepted, 2026-09-02.** Reviewed and accepted by the project owner (Dhanush Santosh) before
implementation, per `docs/rfcs/README.md` and `docs/development-workflow.md`'s design-proposal
rule. Follows the single-use approval design established by
[RFC 0023](0023-single-use-orchestrator-grant-approval.md). Implementation follows this
acceptance.

An implementation matching this design was drafted, verified (build, tests, `go vet` all clean),
and briefly committed to `dev` (`eb3aafd`) without going through this review step -- including a
now-corrected false claim in that original draft that the project owner had already authorized
it. That commit was reverted (`f8103f7`) so the fix could go through review properly; this RFC
was then drafted, reviewed, and accepted on its own merits.

## Problem and desired outcome

RFC 0023 closed the approval-reuse gap for orchestrator grants, and its follow-up audit
(recorded in `docs/backlog.md`) checked the five other `hasApproval`-gated actions in
`internal/protocol/transitions.go` for the same structural weakness: `hasApproval` matches an
approval by action string only and never consumes it, so any approval ever granted for a matching
action string permanently pre-authorizes every future occurrence of that action.

That audit found four of the five sites are safe as-is:

- `contract:<messageID>` and `invocation:<invocationID>` /
  `invocation-sensitive:<invocationID>` are keyed by an entity ID that can only be created once
  (duplicate IDs are rejected before the approval check runs), so their approvals cannot be
  replayed against a second event.
- `shared-write:<taskA>:<taskB>` is intentionally reusable for the lifetime of an ongoing
  arrangement between two tasks -- that is the desired behavior, not a gap.

`task.takeover:<taskID>` is the one real gap: a task ID is long-lived and survives ownership
changes, so one approved takeover record for a task can authorize taking over that task an
unbounded number of times, at any point in the future, regardless of who currently owns it.

The desired outcome is that one takeover approval authorizes exactly one takeover, without
touching the other four sites' (correct) existing behavior.

## Proposed design

When `internal/projection.ApplyEvent` applies a validated `task.takeover` event, it changes one
matching approval from `APPROVED` to `CONSUMED`. If multiple independently-approved records exist
for the same action (e.g. two separate approval requests both approved ahead of time), approval
IDs are sorted and the first is consumed -- deterministic, and each independently-approved record
still represents one authorization. `hasApproval` already only matches `Status == "APPROVED"`, so
a `CONSUMED` record stops authorizing further takeovers with no change needed to the check side
itself; the fix is entirely in the projection layer.

This intentionally does not introduce a conventional, ID-scoped approval ID the way RFC 0023 did
for orchestrator grants (see Alternatives) -- takeover approval IDs stay caller-chosen, only their
consumption changes.

The other four audited actions are explicitly left unchanged; this RFC's design does not extend
`hasApproval` consumption to them.

## Alternatives considered

- **Consume every action matched by `hasApproval`, uniformly across all five sites.** Rejected:
  `shared-write:` is intentionally long-lived, and the entity-ID-keyed actions (`contract:`,
  `invocation:`, `invocation-sensitive:`) cannot be reused across events regardless, so uniform
  consumption would change nothing for three sites and break the fourth.
- **Introduce a conventional, ID-scoped approval ID for takeovers, mirroring RFC 0023's
  `OrchestratorGrantApprovalID`.** Rejected as a larger compatibility change than necessary:
  it would require callers to move off freely-chosen approval IDs for takeovers specifically.
  Consuming one deterministically-selected matching record closes the reuse gap while keeping
  today's caller-chosen approval IDs valid.
- **Consume every approval matching the takeover action, not just one.** Rejected: two separately
  requested and approved records reasonably represent two distinct authorizations (e.g. approved
  by two different reviewers in advance); consuming both on a single takeover would silently
  discard an authorization nobody used yet.

## Compatibility and rollout

No schema migration: `Approval.Status` already supports `CONSUMED`, introduced by RFC 0023.
Existing approved takeover records remain valid for exactly one use once this ships. After a
takeover consumes a record, a new `task.takeover:<taskID>` approval must be requested and approved
before the task can be taken over again. No CLI, MCP, or TUI command-shape changes.

## Security and privacy implications

Prevents a single past takeover decision from permanently authorizing every future ownership
change to a long-lived task -- the same class of replay risk RFC 0023 closed for orchestrator
grants, scoped here to task takeovers. No new data collection or disclosure. Deterministic
single-record consumption keeps an auditable one-to-one correspondence between approved records
and the takeover events they authorized.

## Test and rollout plan

- Projection regression: a `task.takeover` event consumes exactly one matching `APPROVED` record
  (deterministically selected when more than one exists) and leaves unrelated approvals -- other
  tasks', or a second independently-approved record for the same task -- untouched.
- Protocol regression: a `CONSUMED` takeover approval does not authorize another takeover.
- `go build`, `go vet`, and the focused `internal/projection` and `internal/protocol` suites,
  plus the project's usual full verification pass before merge.

## Unresolved questions

None.
