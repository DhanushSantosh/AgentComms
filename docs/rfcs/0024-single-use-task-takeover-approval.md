# RFC 0024: Single-use task-takeover approval

## Status and owner

**Implemented on `dev`, 2026-09-01.** Owner: Dhanush Santosh. The project
owner authorized the approval-reuse audit and its evidence-backed fix. This
lightweight RFC records the authorization decision required by
`docs/development-workflow.md`; it follows the broader single-use approval
design in [RFC 0023](0023-single-use-orchestrator-grant-approval.md).

## Problem and desired outcome

Five authorization paths use `hasApproval(action)`. Contract publication and
invocation request actions contain the ID of an entity that can only be
created once. Shared-write approval intentionally represents an ongoing
arrangement between two tasks. A task ID, however, survives ownership changes,
so one approved `task.takeover:<taskID>` record could authorize every later
takeover of that task.

The desired outcome is that one takeover approval authorizes one takeover,
without shortening shared-write arrangements or changing approval paths that
are already intrinsically limited to one create event.

## Proposed design

When `internal/projection.ApplyEvent` applies a validated `task.takeover`
event, it changes one matching approval from `APPROVED` to `CONSUMED`. If
multiple independently approved records have the same action, approval IDs are
sorted and the first approved record is consumed. This makes replay
deterministic while preserving one authorization per independently approved
record. `hasApproval` already rejects `CONSUMED`, so a later takeover requires
another approved record.

The remaining audited actions are unchanged:

- `shared-write:<taskA>:<taskB>` remains reusable for the task-pair
  arrangement;
- `contract:<messageID>` cannot authorize a second publication because
  duplicate message IDs are rejected;
- `invocation:<invocationID>` and
  `invocation-sensitive:<invocationID>` cannot authorize a second request
  because duplicate invocation IDs are rejected.

The resolved audit and these lifecycle findings are cross-referenced in
`docs/backlog.md`. The implementation is in `internal/projection/apply.go`.

## Alternatives considered

- Consume every action matched by `hasApproval`. Rejected because shared-write
  is intentionally long-lived and the other entity-ID actions cannot be reused
  across events.
- Introduce a conventional, ID-scoped approval ID for takeovers. Rejected as a
  larger compatibility change than necessary; consuming one deterministically
  selected matching record closes the reuse gap while retaining existing
  caller-chosen approval IDs.
- Consume every matching takeover approval. Rejected because two separately
  approved records reasonably represent two authorizations.

## Compatibility and rollout

No schema migration is required: `Approval.Status` already supports the
`CONSUMED` value introduced by RFC 0023. Existing approved takeover records
remain valid once. After use, callers must request and approve a new record,
under any otherwise-valid unique approval ID, before another takeover of the
same task. No CLI or MCP command shape changes.

## Security and privacy implications

The change prevents an old takeover decision from permanently authorizing
future ownership changes to a long-lived task. It adds no data collection or
new disclosure. Deterministic single-record consumption also preserves an
auditable correspondence between approved records and takeover events.

## Test and rollout plan

- Projection regression: a takeover consumes exactly one matching approved
  record, selected deterministically, and leaves unrelated approvals intact.
- Protocol regression: a consumed takeover approval cannot authorize another
  takeover.
- Run focused `internal/projection` and `internal/protocol` tests, formatting,
  `git diff --check`, and broader verification where the execution sandbox
  permits local socket listeners.

## Unresolved questions

None.
