# RFC 0037: Honor expiry on action-scoped approvals

## Status and owners

**Accepted, 2026-09-24.** Drafted by codex-main and accepted by the project
owner before implementation under [the RFC process](README.md). The owner
also confirmed trusted-orchestrator self-approval, redemption by any trusted
active principal, and continued eligibility of no-expiry approvals until used.

## Problem and desired outcome

`internal/protocol.hasApproval` authorizes `task.takeover:<taskID>` and
`shared-write:<taskA>:<taskB>` by action and `APPROVED` status alone. An
approval may carry `ExpiresAt`, but this check ignores it. A reviewer who
requests a 12-hour window can therefore authorize a takeover or a new
overlapping work lease after that window has closed.

Takeovers have a second constraint: [RFC 0024](0024-single-use-task-takeover-approval.md)
consumes one matching approval when the event is projected. If validation
starts ignoring expired records but projection still consumes the first
`APPROVED` record by ID, it may consume an expired record instead of the
unexpired record that authorized the transition. The latter would remain
available for a second takeover.

The desired outcome is to enforce a stated expiry at the point of use,
consume exactly the record that authorized a takeover, and replay historical
events without changing which legacy approval they consumed.

The project owner has separately decided that, within a trusted self-hosted
team, an orchestrator may approve its own takeover and any trusted active
principal may redeem an action-scoped takeover approval. This RFC does not
introduce a human-only tier or requester/redeemer binding.

## Proposed design

1. An action-scoped approval for `task.takeover` or `shared-write` is eligible
   only when it is `APPROVED`, its action matches, and `ExpiresAt` is either
   absent or strictly after the authority's transition time. An absent
   expiry continues to mean no expiry; the CLI's
   generic `approval request` currently makes expiry optional. A supplied
   expiry that is not in the future is rejected when a new approval is
   requested, so a new record cannot be born unusable.
2. `task.takeover` validation selects the eligible matching approval with
   the lexicographically smallest ID. It adds that ID to the accepted
   `TaskStatus` event payload as `approval_id`. The caller does not choose
   it; the authority derives it from state and its own transition timestamp
   during command validation. This server-normalized event field does not
   change the CLI, MCP, or TUI command shape.
3. Projection of a takeover event with `approval_id` consumes exactly that
   approval. Historical takeover events have no `approval_id` and retain
   RFC 0024's original action-only, sorted-ID consumption rule. This
   preserves historical replay even if an old transition used an approval
   after its stated expiry. The new validator never creates such an event.
4. `shared-write` remains a reusable task-pair arrangement, but a new
   overlapping claim may use its approval only while the stated expiry is
   still open. Expiry does not revoke a lease that was already granted;
   normal lease and renewal rules continue to govern existing work.
5. The missing-approval error should distinguish "no approved record" from
   "matching approvals exist but all have expired" so the operator can
   request a fresh approval rather than debug the wrong condition.

## Alternatives considered

- **Filter expired approvals only in `hasApproval`.** Rejected: projection
  could consume a different, expired record and leave the valid one reusable.
- **Make projection skip expired approvals for every event.** Rejected as a
  silent change to replay of historical takeover events produced before
  expiry enforcement.
- **Require a finite expiry on every new takeover approval.** Stronger, but
  changes the generic CLI/MCP/TUI approval-request contract and invalidates
  currently approved no-expiry records. That is a separate policy decision,
  not necessary to honor a window an approver explicitly chose.
- **Bind takeover approval to requester or redeemer, or require HUMAN tier.**
  Rejected for this RFC because the owner explicitly chose autonomous
  orchestrators and redemption by any trusted active principal. Revisit if
  the service is ever offered to mutually untrusted tenants.

## Compatibility and rollout

No database migration is needed. `approval_id` is an additive field in
`task.takeover` event data; old events without it continue to project with
the legacy rule. Existing approved records without expiry remain eligible
once. Existing records whose stated expiry has passed cease authorizing new
claims or takeovers; that is the intended behavioral correction, and users
can request a fresh approval.

Before rollout, verify that every event consumer which projects
`task.takeover` understands `approval_id`, and document the supported
authority/client upgrade order. A downlevel projector ignoring the field
could consume a different approval when expired and unexpired records share
an action, so mixed-version readers must not be treated as equivalent. A
downlevel authority would also continue accepting expired approvals, so the
authority—not just clients—must be upgraded before relying on this policy.
Deploy the new authority and matching local binaries together, and do not
use a downlevel projector against a project after it has recorded a new
`task.takeover` event. The field is additive and needs no database or
project-format migration; the existing binary/build compatibility checks
remain the operational guard against mixed readers.

## Security and privacy implications

The change prevents an explicitly time-limited approval from authorizing
work after expiry. The `approval_id` is an existing local identifier, not a
new secret or personal datum. It makes a takeover's authorization auditable
and keeps one approved record tied to one takeover event. No new principal
or tier privilege is granted.

## Test and rollout plan

- Protocol: a takeover with only an expired matching approval fails; a
  fresh approval succeeds; a no-expiry legacy approval remains eligible.
- Protocol/projection: with one expired lower-ID record and one unexpired
  higher-ID record, the accepted event names and consumes the unexpired
  record exactly once. A second takeover fails without another approval.
- Projection: historical takeover events without `approval_id` still
  consume the same sorted first record as RFC 0024, even if it had expired
  by the event's timestamp.
- Shared-write: expired approval refuses a new overlapping claim; an
  unexpired or no-expiry approval permits one.
- Run focused protocol/projection/service tests, the full Go suite and
  vet, then Windows and other CI before release promotion.

## Unresolved questions

None for this release. If mixed-version projection becomes a supported
deployment mode, add explicit event-feature negotiation before relaxing the
same-version rollout rule.
