# RFC 0046: Honor orchestrator-grant approval expiry

## Status and owners

Proposed, 2026-10-04. Owner: project maintainer; author: codex-main.
Implementation requires maintainer acceptance. This governance change extends
RFC 0023's single-use orchestrator-grant authorization and RFC 0037's explicit
expiry semantics. RFC 0045 acceptance does not approve this change.

## Problem and desired outcome

On 7315f3a, a disposable protocol regression used a fixed authority time and
an APPROVED, HUMAN-tier approval at the exact conventional grant ID/action.
Both `agent.activate` and `agent.switch-role` accepted ORCHESTRATOR when the
approval expired one second earlier or exactly at the transition time. All
four cases reproduced in three uncached runs. Future-approved controls pass;
PENDING, CONSUMED and REJECTED controls fail. The expiry field survives intact.

`hasOrchestratorGrantApproval` checks ID, action, status and tier but no
deadline. Both authorities call the same transition validator with their
own current time. This is a reproduced authorization-window defect, not proof
that an unauthenticated or ordinary agent can grant itself privileges: the
human-principal, elevated-signature and exact approval checks still apply.

An explicitly chosen expiry should bound new grants. A stale conventional
approval must be recoverable without editing signed history or relaxing the
human decision requirement.

## Proposed design

1. Pass the authority's transition time into the grant eligibility helper.
   An APPROVED HUMAN approval at the exact ID/action is usable only if its
   expiry is absent or strictly after that time. Apply to both grant routes.
   Equality is expired, matching existing action-approval semantics.
2. Preserve legacy no-expiry approvals and single-use consumption. Preserve
   human-principal, elevated-key, target-role and exact-ID/action checks.
3. Extend the conventional-ID re-request exception narrowly: alongside the
   existing CONSUMED case, allow replacement of an expired PENDING or APPROVED
   HUMAN grant record by a new HUMAN request for the same conventional ID and
   exact action. A supplied new expiry must be in the future, as today.
   The new request becomes PENDING and needs a fresh, separate human approval;
   it does not inherit approval, response, requester or expiry from its predecessor.
   Nonexpired records, other IDs/actions/tiers and rejected records keep their
   current replacement restrictions. This prevents stale approval deadlock.
4. Return a clear expired-grant diagnostic with the existing re-request command
   and requirement for separate human approval. Existing CLI/MCP/TUI request
   shapes remain unchanged; no new command or expiry requirement.
5. Apply expiry only when authorizing a new transition. Keep historical
   projection and conventional-ID consumption unchanged. Existing accepted
   events, including grants made after expiry by older validators, replay
   exactly as before; rejection of new grants never revokes an existing role.

## Alternatives considered

- Enforce expiry in projection: changes historical replay and can silently
  retain an authorization that the original grant consumed.
- Require expiry for every approval: changes an independent public policy
  and breaks existing no-expiry approvals unnecessarily.
- Reject expired grants without re-request recovery: the conventional ID
  remains occupied by an APPROVED/PENDING record that cannot be used.
- Permit arbitrary approval replacement: destroys unrelated immutability
  guarantees and is unnecessary for this scoped recovery.

## Compatibility and rollout

No event/schema/database migration or history rewrite. Existing clients can
submit the same request/approve/grant commands. Upgrade the authority/local
toolkit before relying on expiry enforcement; an older authority still
accepts expired records. Projection must preserve historical state, including
repeated conventional-ID request cycles and single-use consumption.

## Security and privacy

This honors an approver's chosen window without creating new principals or
weakening human/elevated-signature checks. Re-request recovery authorizes no
grant itself. Keep synthetic test principals and disposable stores; no live
approval or credential mutation during verification.

## Test and rollout plan

- Both grant routes: past/equal expiry reject; future/absent expiry accept.
- Wrong ID/action/tier, PENDING/REJECTED/CONSUMED and nonhuman callers reject.
- Expired conventional PENDING/APPROVED re-request yields a fresh PENDING
  record; grant fails until human approval, succeeds once, then is consumed.
- Nonexpired/wrong-ID/wrong-action/wrong-tier/rejected replacement rejects;
  existing CONSUMED recovery stays accepted; nonfuture new expiry rejects.
- Real personal-authority/service workflow and Postgres authority workflow
  prove rejection adds no event and fresh approval permits one grant.
- Historical expired-grant event replay produces identical role/consumption
  state; later conventional-ID request history remains intact.
- Focused uncached protocol/projection/service/backend race tests, vet,
  generated references, full candidate platform CI and docs checks.

## Unresolved questions

Maintainer acceptance of the bounded expiry enforcement and expired-record
re-request recovery is required before implementation.
