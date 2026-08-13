# RFC 0018: Self-service role switching, custom role labels, and removing AGENT/OBSERVER

## Status

**Accepted 2026-08-13**, direction confirmed by the project owner. Per `docs/rfcs/README.md`
and `docs/development-workflow.md`'s design-proposal rule, reviewed and accepted before
implementation began.

## Context

`model.Role` today is a closed, four-value enum: `OWNER`, `ORCHESTRATOR`, `AGENT`,
`OBSERVER`. Two of these are redundant or under-used in practice:

- `AGENT` duplicates `model.PrincipalType`'s own `AGENT` value. A principal's *kind* (human
  vs. agent) is already fully captured by `PrincipalType`; `Role: AGENT` adds nothing beyond
  "not owner, not orchestrator" and reads as confusing next to `PrincipalType: AGENT`, a
  completely different axis.
- `OBSERVER` is a narrow, rarely-used read-only tier that blocks exactly two transitions
  (`task.claim`, `invocation.request`) and nothing else -- not a coherent permission tier, just
  two hardcoded exceptions.

Separately, every role change today (`agent.activate`) is owner/orchestrator-gated with no
exception for a principal changing its own role -- there is no self-service path at all. A
principal that wants a more specific, descriptive standing (e.g. "Frontend-Architect",
"Backend-Designer", "Tester") has no way to express that beyond the generic `AGENT` label,
and changing even that requires asking an owner or orchestrator to run `agent activate` on
its behalf every time.

## Proposed design

### Role becomes `OWNER` | `ORCHESTRATOR` | free text

`model.Role` stays `type Role string` (no new type). `RoleAgent` and `RoleObserver` are
removed as constants. `RoleOwner` and `RoleOrchestrator` remain the only two reserved values
with special meaning; anything else is a custom, freeform label a principal chooses for
itself (`Frontend-Architect`, `Tester`, `On-Call`, ...) with **zero permission effect** --
purely descriptive, exactly like `DisplayName`. There is no read-only tier left: every
non-owner, non-orchestrator principal has identical write standing regardless of its label,
matching what plain `AGENT` already granted today.

`OWNER` becomes reachable **only** through the one special bootstrap event
(`sequence == 1`, `agent.activate` for the project's own owner -- already handled by a
dedicated code path in `internal/personalauthority/engine.go` and
`internal/authority/postgres.go` that bypasses `ValidateTransition` entirely). The general
`agent.activate` path used to silently accept `Role: OWNER` for any target too, an unused and
unintended gap; this closes it -- `ValidateTransition` now rejects `OWNER` as a target role
for `agent.activate` outside that one bootstrap case.

### New transition: `agent.switch-role` (self-service)

A new payload type, `model.AgentRoleSwitched{ Role Role }`, and transition type
`agent.switch-role`, distinct from `agent.activate`:

- **Self-only.** `id` must equal `actor`. An owner or orchestrator changing *someone else's*
  role still goes through the existing, unchanged `agent.activate` path.
- **Never `OWNER`.** Rejected as a target unconditionally, for everyone, matching the
  existing "owner principal cannot be suspended/revoked" invariants elsewhere in
  `transitions.go` -- `OWNER` is fixed at project creation and never reachable through any
  self-service or administrative role change afterward.
- **The current owner cannot use it either.** A principal whose *current* role is `OWNER`
  cannot call `agent.switch-role` at all (self-demotion isn't blocked by the "never OWNER as
  a target" rule alone, since the owner would be switching *away from* OWNER, not *to* it) --
  `OWNER` is the one truly fixed identity in a project and this closes that gap rather than
  leaving a one-call path to a project with no owner.
- **Switching to `ORCHESTRATOR` keeps the full existing gate, unchanged:** the switching
  principal's own `PrincipalType` must be `HUMAN` (an `AGENT` principal can never self-promote
  to orchestrator -- symmetric with the existing rule that only a human can *grant* the role
  to someone else), and a pre-existing HUMAN-tier approval record for
  `agent.activate:<actor>` must already be `APPROVED` -- reusing
  `OrchestratorGrantApprovalAction` exactly as `agent.activate` does today. This keeps the
  two-step control (request, then a separate human approval, then the switch) that stops a
  single unattended action from completing the whole escalation -- explicitly preserved per
  the project owner's direction, not weakened to "passphrase alone."
- **Requires the elevated key when switching to `ORCHESTRATOR`.** `RequiresElevatedKey` gains
  a case for `agent.switch-role` (`switched.Role == model.RoleOrchestrator`), the same
  classification `agent.activate` already uses for the same target role -- reads only the
  payload, no state, matching the existing invariant every other elevated-key case in that
  function documents. This is what actually triggers the interactive passphrase prompt
  (`Service.PassphrasePrompt`) at the CLI and TUI, and a hard refusal over MCP (which never
  takes a passphrase parameter) -- both for free, from the one shared classification function
  every interface and both authority backends already call.
- **Only changes `Role`.** Unlike `agent.activate`, this never touches `Capabilities` or
  `Scopes` -- a principal relabeling itself cannot use this as a side-door to grant itself new
  scopes; that remains an owner/orchestrator-only action via the unchanged `agent.activate`.
- **No owner/orchestrator elevation gate at all for non-orchestrator targets.** This is the
  actual self-service unlock: `agent.switch-role` is a new transition type, never added to
  `elevated()`'s switch statement, so the blanket "owner or orchestrator role required" check
  in `ValidateTransition` never applies to it. Its own blast radius is bounded by construction
  (self-only, never OWNER, ORCHESTRATOR fully gated as above), so it needs no additional
  elevation check beyond those.

### Removing OBSERVER's behavioral gates

The two `RoleObserver` checks (`task.claim`, `invocation.request`) are deleted outright, not
replaced with an equivalent for custom roles -- per the project owner's explicit direction,
custom role labels carry no permission effect, so there is nothing to replace.

### Surfaces

- **CLI:** new `agent-comms agent switch-role --role <role>` (`Args: cobra.NoArgs`, always
  self -- mirrors `agent rotate-key`'s existing self-only pattern). `agent activate`'s
  `--role` flag drops its `AGENT` default and becomes required, since there is no longer a
  generic role to default to.
- **MCP:** new `agent_switch_role` tool, following the identical generic `s.Execute(...)`
  pattern every other tool already uses -- no special-casing needed; the elevated-key refusal
  for an `ORCHESTRATOR` target falls out of the same shared classification automatically,
  exactly like `agent_activate` already refuses today.
- **TUI:** new self-service "Switch role" form on the agent's own row, mirroring
  `activateForm`'s existing Orchestrator-approval-chaining `Dispatch` escape hatch and masked
  passphrase field. `activateForm`'s own Role field changes from a fixed
  `Options: []string{"AGENT", "OBSERVER", "ORCHESTRATOR", "OWNER"}` picker to free text
  (`OWNER` is no longer a legal target there either, following the bootstrap-only rule above)
  -- `FormField.Options` is a strict single-select with no room for arbitrary text, which a
  freeform custom-role field fundamentally needs.

## Alternatives considered

- **A separate `CustomRole` field alongside a smaller closed `Role` enum.** Rejected: doubles
  the surface (every check, every form, every doc would need to reason about two fields
  instead of one), for no behavioral benefit -- `Role` was already just a string underneath,
  and the whole point is that a custom label has no permission effect, so nothing is lost by
  letting it live in the same field `OWNER`/`ORCHESTRATOR` already occupy.
- **Reusing `agent.activate` for self-service switching instead of a new transition type.**
  Rejected: `agent.activate` is unconditionally `elevated()`-gated today with no per-target
  exception, and it also carries `Capabilities`/`Scopes` in its payload -- reusing it would
  require either weakening that gate for every caller (not just self-targeting ones) or adding
  an id==actor carve-out that still has to reason about scope/capability tampering. A distinct
  transition type keeps the self-service path's blast radius provably bounded (role only,
  self only) without touching `agent.activate`'s existing, well-understood semantics at all.
- **Passphrase alone as the orchestrator gate, dropping the pre-approval step.** Considered
  and explicitly rejected by the project owner: the two-step request-then-approve control
  exists specifically so a single unattended action (an agent typing a command, even one that
  happens to know or extract a passphrase) can never complete the whole escalation alone. Kept
  unchanged.
- **Giving custom roles some structured permission effect** (e.g. a role-to-scope mapping).
  Rejected for this RFC: the project owner's direction is that custom roles are purely
  descriptive, matching what `AGENT` already granted. A structured permission system per
  custom role is a meaningfully different, larger feature and not part of this change.

## Compatibility and rollout

This is a real behavior change to the permission model, not additive-only:

- **`model.RoleAgent` and `model.RoleObserver` no longer exist as constants** -- any code
  (internal or external, e.g. a script scraping `agent-comms agent list --json` for
  `"role":"AGENT"`) that compared against those literals by name still works unchanged (the
  string values `"AGENT"`/`"OBSERVER"` simply become ordinary custom labels rather than
  reserved ones), but any code importing the Go constants directly needs updating.
- **`agent activate --role` is now a required flag** with no default. Any script that
  previously omitted `--role` and relied on the implicit `AGENT` default must now pass one
  explicitly (`--role ORCHESTRATOR` or any custom label).
- **`agent.activate --role OWNER` for an existing, non-bootstrap target now fails outright**
  where it previously (silently, unintentionally) succeeded for an owner/orchestrator caller.
  No shipped interface ever exposed this path deliberately, so no real workflow should be
  affected -- but it is a closed capability, not purely additive.
- **OBSERVER's read-only behavior is gone.** Any project actually relying on OBSERVER to keep
  a principal from claiming tasks or requesting invocations loses that enforcement -- the
  label can still be applied to a principal (as ordinary custom text) but carries no effect.

## Security implications

- The new self-service path's blast radius is bounded by construction: self-only (`id ==
  actor`), `OWNER` unreachable as either source or target, `ORCHESTRATOR` fully gated behind
  the unchanged two-step human-approval-plus-elevated-key control. A principal can only ever
  move itself sideways (custom label to custom label) or, with a human in the loop exactly as
  today, upward to orchestrator -- never to owner, never on another principal's behalf.
- Closing the `agent.activate --role OWNER` gap for non-bootstrap targets removes a real
  (if apparently never-exercised) path for an owner/orchestrator caller to mint an unintended
  second "owner" with all of `OWNER`'s protections (immune to suspend/revoke, always
  elevated) -- a strict hardening, not a new restriction on anything that worked as intended.
- Removing `OBSERVER`'s enforcement is a deliberate reduction in what the permission model
  expresses, accepted explicitly by the project owner as the tradeoff for a purely
  descriptive, freeform role label. Anyone who needs an actual read-only principal after this
  change has no supported way to get one -- flagged here in case it needs to be revisited.

## Test and rollout plan

- Unit coverage for `agent.switch-role`: self-only (rejects `id != actor`), rejects `OWNER`
  as a target, rejects switching when the current role is `OWNER`, non-orchestrator targets
  succeed with no owner/orchestrator elevation required, `ORCHESTRATOR` target enforces the
  `PrincipalType == HUMAN` check, enforces the pre-existing HUMAN-tier approval, and requires
  (and correctly classifies via `RequiresElevatedKey`) the elevated key -- mirroring the
  equivalent existing `agent.activate` coverage.
- Confirms `agent.switch-role` never touches `Capabilities`/`Scopes` on the target.
- Confirms `agent.activate --role OWNER` now fails for a non-bootstrap target, on both
  authority backends (`internal/personalauthority`, `internal/authority/postgres`).
- Full existing suite updated everywhere `model.RoleAgent`/`model.RoleObserver` was
  referenced (mechanical for most call sites -- test helpers that just needed *some* active
  non-elevated role; targeted rewrites anywhere `OBSERVER`'s removed behavior was under test).
- `go test -race`, `staticcheck`, `gofmt`, `scripts/coverage-floor.sh` all clean before
  shipping, matching this session's established verification bar.

## Unresolved questions

None outstanding -- scope was narrowed through direct clarification with the project owner
before implementation (OWNER stays exactly as-is with no added HUMAN role; the orchestrator
two-step approval gate is kept, not weakened to passphrase-only; custom roles carry no
permission effect at all).
