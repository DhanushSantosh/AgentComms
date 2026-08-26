# RFC 0023: Single-use, ID-scoped approval for orchestrator grants

## Status

**Accepted 2026-08-26**, direction confirmed by the project owner after live testing on a
real project (`/home/dhanush/Projects/Portfolio`) surfaced the exact gap this RFC closes. Per
`docs/rfcs/README.md` and `docs/development-workflow.md`'s design-proposal rule, reviewed and
accepted before implementation began.

## Context

Granting the `ORCHESTRATOR` role — via `agent.activate --role ORCHESTRATOR` or the self-service
`agent.switch-role --role ORCHESTRATOR` (RFC 0018) — requires a pre-existing, separately-approved
`HUMAN`-tier approval, closing the gap where a fully autonomous agent session, signing with a
genuinely human-owned credential by construction (the ambient-owner-fallback connection
`docs/agent-onboarding.md` describes), could otherwise complete the entire escalation in one
unattended command. The intent, stated directly in the code (`internal/protocol/
transitions.go:460-471`): *"giving a human a real request to see and manually approve ... before
the grant can proceed, rather than a single self-contained command completing the whole
escalation unattended."*

The actual check, `hasHumanApproval(st, action)`, does not enforce that intent. It scans every
approval in state for **any** record whose `Action` string matches, `Tier == "HUMAN"`, and
`Status == "APPROVED"` — regardless of which specific approval record it is, and regardless of
whether it has ever been used before:

```go
func hasHumanApproval(st model.State, action string) bool {
	for _, a := range st.Approvals {
		if a.Action == action && a.Status == "APPROVED" && a.Tier == "HUMAN" {
			return true
		}
	}
	return false
}
```

Nothing marks an approval as consumed once it has authorized a grant — `AgentActivated`'s own
projection (`internal/projection/apply.go`) only mutates the agent record, never the approval.
Approval IDs are also freely chosen by the caller at request time (`OrchestratorGrantApprovalID`
is documented as a *suggested* convention for the error message's copy-paste command, "the
server never generates one") — so the check cannot even distinguish which approval was meant to
authorize this specific grant from any other approval that happens to share the same action
string.

**Confirmed live**, not hypothetically: a real project's approval history (`agent-comms approval
list --json`) showed exactly this —

```
grant-orchestrator-THOR:   action "agent.activate:THOR", status APPROVED
grant-capabilities-THOR:   action "agent.activate:THOR", status APPROVED
```

Two independently-requested-and-approved records with the identical action string. Either one
alone permanently satisfies `agent.activate --id THOR --role ORCHESTRATOR` forever — including a
fully unattended re-grant after some future revoke, with no human anywhere in the loop at that
later moment, on the strength of a decision made for an entirely different original reason. Every
grant *feels* like it went through fresh per-event human control, because the request-then-
approve ritual is followed each time; the actual enforcement is "has a human ever approved this
exact string, once, ever" — a materially weaker guarantee than the one the code's own comment
describes.

**Desired outcome:** an orchestrator-grant approval authorizes exactly one grant. A human deciding
to approve `THOR`'s escalation today must not silently pre-authorize `THOR`'s escalation again
next year with nobody watching.

## Proposed design

Two changes, both scoped to the orchestrator-grant approval gate specifically (`agent.activate`
granting `ORCHESTRATOR`, and `agent.switch-role` to `ORCHESTRATOR`) — not to the general approval
system. See "Alternatives considered" for why the broader `hasApproval` (non-human-tier) call
sites are deliberately left out of this RFC's scope.

### 1. ID-scoped lookup, not action-string scan

`OrchestratorGrantApprovalID(id)` (`"grant-orchestrator-" + id`) stops being a mere suggestion and
becomes the only ID that can ever satisfy this specific gate. The check changes from a scan to a
direct lookup:

```go
func consumableOrchestratorApproval(st model.State, principalID string) (model.Approval, bool) {
	approval, exists := st.Approvals[OrchestratorGrantApprovalID(principalID)]
	if !exists || approval.Tier != "HUMAN" || approval.Status != "APPROVED" ||
		approval.Action != OrchestratorGrantApprovalAction(principalID) {
		return model.Approval{}, false
	}
	return approval, true
}
```

`principalID` is the target being activated for `agent.activate`, and the switching actor itself
for `agent.switch-role` — unchanged from today's `OrchestratorGrantApprovalAction` call
arguments. A `grant-capabilities-THOR` approval, however genuinely approved, can no longer stand
in for `THOR`'s orchestrator grant; only a record actually named `grant-orchestrator-THOR` can.
The existing error-hint text is already exactly this command (`run \`approval request --id
grant-orchestrator-<id> --tier HUMAN --action agent.activate:<id>\``) — this makes that
suggestion the *only* one that works, closing the gap where a differently-named approval could
substitute for it by coincidence.

### 2. Consumption on successful grant

The moment a grant succeeds using a specific approval, that approval's `Status` moves from
`APPROVED` to a new terminal status, `CONSUMED`, applied within the *same* event's projection —
no separate signed event is needed, since consumption is a pure function of the granting event's
own type, entity ID, and role, fully derivable by anyone replaying history:

```go
// internal/projection/apply.go
case *model.AgentActivated:
	a := s.Agents[e.EntityID]
	a.Status = "ACTIVE"
	a.Role = p.Role
	a.Capabilities = p.Capabilities
	a.Scopes = p.Scopes
	s.Agents[e.EntityID] = a
	if p.Role == model.RoleOrchestrator {
		consumeOrchestratorApproval(s, e.EntityID)
	}
case *model.AgentRoleSwitched:
	a := s.Agents[e.EntityID]
	a.Role = p.Role
	s.Agents[e.EntityID] = a
	if p.Role == model.RoleOrchestrator {
		consumeOrchestratorApproval(s, e.EntityID) // e.EntityID == e.Actor here (self-service only)
	}
```

`consumeOrchestratorApproval` looks up `OrchestratorGrantApprovalID(principalID)` (the same ID the
transition-validation step above just required to exist and be `APPROVED`) and flips its `Status`
to `CONSUMED`. A `CONSUMED` approval fails `consumableOrchestratorApproval`'s `Status != "APPROVED"`
check identically to a never-approved one — the very next attempt to (re-)grant that principal
`ORCHESTRATOR`, whenever it happens, requires a brand new `approval request` +
separately-run-`approval approve`, exactly as if none had ever existed. Revoking, suspending, or
switching a principal away from `ORCHESTRATOR` does not un-consume its old approval; there is no
path back to `APPROVED` for a `CONSUMED` record.

`internal/projection` gains a new, one-directional import of `internal/protocol` for
`OrchestratorGrantApprovalID`/`OrchestratorGrantApprovalAction` — `internal/protocol` does not
import `internal/projection`, so this introduces no cycle.

### Not required: a schema migration

`Approval.Status` is stored as a plain string with no database `CHECK` constraint restricting it
to a fixed enum (confirmed: `internal/authority`'s Postgres schema has no such constraint) — adding
`CONSUMED` as a new value needs no migration, on either `personalauthority` (SQLite) or the shared
Postgres authority.

## Alternatives considered

- **Fix the general `hasApproval` (ORCHESTRATOR-tier) primitive too**, which gates five other call
  sites with the structurally identical scan-and-never-consume pattern (`shared-write:`,
  `task.takeover:`, `contract:`, `invocation:`, `invocation-sensitive:` — see
  `internal/protocol/transitions.go`). Rejected for *this* RFC: each of those sites' intended reuse
  semantics hasn't been individually audited (a shared-write exception, for instance, may
  legitimately need to remain valid for the duration of an ongoing arrangement rather than being
  single-use), and applying single-use consumption uniformly without that audit risks breaking a
  workflow that depends on an approval covering more than one event. Recorded in
  `docs/backlog.md` as a real, structurally-identical follow-up investigation, not silently
  dropped.
- **Expire approvals after a fixed time window instead of (or in addition to) consuming them on
  use.** Narrows the window but doesn't close the gap: an approval reused twice within its expiry
  window still authorizes an unattended second grant with no human involved the second time. Time
  alone doesn't distinguish "used once, correctly" from "reused." Left as a possible additional
  hardening layer, not a substitute for consumption.
- **Require the approver to differ from the requester**, closing the same-person-both-ends gap
  `guide/governance.md` documents as explicitly allowed today. Out of scope: unrelated to the
  reuse problem this RFC addresses (the THOR example above shows the *same* record, correctly
  approved by a different-from-requester human in one case, still being silently reusable), and
  changes a separately-documented, deliberate governance decision this RFC has no reason to
  revisit.
- **Auto-generate the approval ID server-side** instead of relying on a caller-followed
  convention, closing the free-choice gap more forcibly. Rejected: `approval.request`'s IDs are
  caller-chosen for every approval type, human-tier orchestrator grants included; carving out one
  special case where the server overrides the caller's `--id` would be a more surprising, less
  consistent CLI contract than simply requiring (and already documenting, via the existing error
  hint) the one ID that satisfies this specific gate.

## Compatibility and rollout

- **Breaking**, but narrow: any *already-`APPROVED`* orchestrator-grant approval that does not use
  the `grant-orchestrator-<id>` ID will no longer satisfy a **future** grant/re-grant attempt after
  this ships. A principal's *current, already-active* `ORCHESTRATOR` role is entirely unaffected —
  nothing re-checks approval state for a role a principal already holds; this only changes what's
  required the next time that role is (re-)granted, e.g. after a suspend/revoke/switch-away and a
  later re-activation.
- A principal that legitimately needs `ORCHESTRATOR` again after this ships requests and gets a
  fresh, correctly-`--id`'d, one-time approval, exactly as the error message already instructs
  today — no new command, no new flag, no documentation to rewrite.
- No schema migration, confirmed above. No change to elevated-key signing requirements (RFC 0018's
  human-principal-only self-switch gate, and `RequiresElevatedKey`'s coverage of `agent.activate`,
  are both untouched).
- `docs/backlog.md` gains an entry recording the five structurally-identical `hasApproval` call
  sites as a real, deliberately-deferred follow-up (see "Alternatives considered").

## Security and privacy implications

- This closes a real gap between documented intent and actual enforcement: an orchestrator grant
  now requires a human decision made specifically for *this* grant, not merely *a* grant that once
  shared the same target and role. This is the entire point of the change; there is no privacy
  exposure or new data surface — `Approval.Status`'s existing values (`PENDING`/`APPROVED`/
  `REJECTED`) simply gain one more (`CONSUMED`), visible the same way the others already are
  (`approval list`, `--json`, the TUI).
- Consumption happens deterministically inside the same signed event's projection, not as a
  separate write — no new race window between "check" and "use" that a concurrent second grant
  attempt could slip through; both a passing and a failing `agent.activate`/`agent.switch-role`
  remain single, atomic transactions exactly as today.

## Test and rollout plan

- A real, regression test reproducing the exact live-confirmed scenario: two approvals for the
  same target/action under different IDs (mirroring `grant-orchestrator-THOR` /
  `grant-capabilities-THOR`), asserting the non-conventionally-ID'd one can never satisfy the
  gate.
- `agent.activate --role ORCHESTRATOR` and `agent.switch-role --role ORCHESTRATOR` each: succeed
  once against a correctly-`--id`'d `APPROVED` approval; fail against a `PENDING` one (unchanged
  from today); fail against a wrong-ID'd but same-action `APPROVED` one (the new behavior); fail on
  a second attempt reusing the now-`CONSUMED` approval, requiring and then succeeding against a
  freshly-requested-and-approved one.
- `approval list`/`--json` after a successful grant shows the consumed approval's `status` as
  `CONSUMED`, not `APPROVED`.
- Existing `TestAgentActivateCLIRequiresHumanToGrantOrchestratorRole` and its revoke-side sibling
  (`internal/app/app_test.go`) continue to pass unmodified where they only exercise the
  already-correct reject path; extended for the new consume-on-success and wrong-ID cases.
- Full `go test ./...` and `go vet ./...` before merge, per standard practice.

## Unresolved questions

- Whether the five other `hasApproval` (non-`HUMAN`-tier) call sites should get the same
  ID-scoping-plus-consumption treatment, and under what semantics for the ones that may
  legitimately need multi-use (e.g. an ongoing shared-write exception) — deferred to the
  `docs/backlog.md` follow-up noted above, not decided here.
- Whether `approval request` should warn (not block — other approval types still need free-form
  IDs) when its `--id`/`--tier`/`--action` combination looks like an attempted orchestrator-grant
  approval that won't use the conventional ID the gate actually checks for — a possible UX
  improvement, not required for this RFC's core fix.
