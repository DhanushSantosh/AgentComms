# RFC 0017: Refuse ambiguous legacy-fallback actor resolution for governed writes

## Status

**Accepted 2026-08-13**, direction confirmed by the project owner after a live incident
(`INCIDENT-2026-08-12-thor-key-misuse.md`, a real project) traced the root cause to exactly
the gap this RFC closes. Per `docs/rfcs/README.md` and `docs/development-workflow.md`'s
design-proposal rule, reviewed and accepted before implementation began.

## Context

RFC 0016 closed accidental cross-session identity inheritance for any invocation carrying a
recognized provider session ID (`CLAUDE_CODE_SESSION_ID`/`CODEX_THREAD_ID`). It explicitly
left one case shared, by design: an invocation with **no** recognized session ID at all falls
back to the legacy, machine-wide `UserConfig.ActiveProfile` field -- the same single value
every such invocation on the machine shares, exactly as it always did.

A live incident confirmed this is a real, not just theoretical, gap: an agent (ZEUS) whose
runtime-management commands run through an invocation path with no ambient session signal
(confirmed empirically, twice, including after a full session restart -- consistent with
opencode, which provides no equivalent to `CLAUDE_CODE_SESSION_ID`/`CODEX_THREAD_ID` at all)
had two governed writes silently signed under a *different* agent's identity (THOR), because
the shared legacy field happened to hold THOR's profile at that moment. Root-caused via the
project's own signed event log, not guessed at -- every claim traceable to a specific sequence
number and signature.

**The actual risk is narrower than "any legacy fallback is dangerous."** A project with zero
or one locally-registered identity has no real ambiguity -- there's nothing else the legacy
field could reasonably mean. The risk is specific: a project with **two or more** locally
registered identities, where an invocation with no session ID and no explicit `--actor`/
`--profile`/`AGENT_COMMS_ACTOR` silently signs under whichever one happens to be sitting in
that one shared slot.

## Proposed design

`Service` gains a new field, `AmbiguousActor bool`, following the exact existing pattern
`PassphrasePrompt` already establishes: set once by whichever caller constructs/configures
the `Service` (CLI, TUI, and MCP all share the same `PersistentPreRunE` in `internal/app`),
consulted internally at the one place that actually matters.

- `internal/identity`: new `UserConfig.ProfileCountForProject(projectID string) int` --
  counts locally-saved profiles scoped to one project (the existing map holds profiles across
  every project on the machine, so this must filter, not just count `len(Profiles)`).
- `internal/app`'s `PersistentPreRunE` sets `c.svc.AmbiguousActor = true` exactly when
  `c.actorResolution.Source == identity.ActorSourceActiveProfile` (the legacy field was
  actually used to resolve this invocation's actor) **and**
  `userConfig.ProfileCountForProject(cfg.ProjectID) > 1`. Every other resolution source
  (explicit flag, explicit profile, `AGENT_COMMS_ACTOR`, a real session's own scoped slot, an
  unambiguous host-label binding, or the safe project-owner fallback) is, by construction,
  never ambiguous -- none of them are touched by this change.
- `Service.ExecuteWithPassphrase` (the one function `Execute` itself delegates to, and the
  single choke point every governed write already goes through regardless of interface)
  checks this field first and refuses outright with a clear, actionable error if set --
  before any credential lookup, before any network/transaction work.
- **This is not a per-command allowlist/denylist.** Read paths (`State()`, `history`, `doctor`,
  ...) never call `Execute`/`ExecuteWithPassphrase` at all, so they are structurally
  unaffected without needing to be named anywhere. A future new write command is
  automatically covered the moment it calls `Execute`, with zero additional wiring --
  matching this project's own stated principle for the transition validator ("the one...
  shared by every interface... not duplicated per interface, so it cannot be bypassed by
  going through a different one").
- The TUI's own actor-switch form (`internal/tui/agents.go`) explicitly sets
  `m.svc.AmbiguousActor = false` on a successful switch -- a deliberate, human-confirmed
  choice from the picker (which already shows each candidate's role, per the earlier TUI
  fix) is never itself ambiguous, regardless of how the TUI's *initial* actor was resolved
  at startup.

## Alternatives considered

- **Classify CLI commands as read/write by name, gate in `PersistentPreRunE`.** Rejected:
  requires an allowlist or denylist of dozens of command paths, fragile against future
  additions, and covers CLI only -- TUI and MCP would need the identical logic duplicated
  separately. The chosen design covers all three from one place, automatically, because it
  sits at the actual signing choke point instead of guessing from a command name upstream of
  it.
- **Change `Execute`/`ExecuteWithPassphrase`'s signature to accept resolution context
  directly.** Rejected on cost/risk: 64+ call sites across `internal/app`, `internal/tui`,
  `internal/mcp`, `internal/worker`, and `internal/onboarding` would all need updating for a
  change that a single struct field (matching the existing `PassphrasePrompt` pattern)
  achieves with zero call-site changes.
- **Apply the same refusal to reads too.** Rejected: reads carry no misattribution risk (they
  never sign anything), and blocking diagnostic commands (`doctor`, `history`, `status`) in
  exactly the situation someone would want to run them to investigate an ambiguous setup
  would be actively counterproductive.

## Compatibility and rollout

- Real behavior change, stated plainly: a project with 2+ locally-registered identities and
  an invocation with no session ID and no explicit actor now gets refused where it previously
  silently succeeded (correctly or not). This is the entire point.
- Unaffected: any project with 0-1 local identities; any invocation carrying an explicit
  `--actor`/`--profile`, `AGENT_COMMS_ACTOR`, a real recognized session's own scoped profile,
  or an unambiguous host-label binding; every read-only command; `internal/worker`'s
  standalone worker-loop `Service` instances and `internal/onboarding`'s, which never go
  through `internal/app`'s `PersistentPreRunE` and so never have this field set at all
  (default `false`, unchanged behavior).
- `docs/agent-onboarding.md` gains a callout for agents with no ambient provider session ID
  (opencode-based agents specifically, or any script/background process): export
  `AGENT_COMMS_ACTOR` explicitly, or expect governed writes to be refused rather than
  silently misattributed from now on.

## Security and privacy implications

- Strictly additive to the existing guarantees: no transition that was rejected before is now
  accepted; some that were previously (silently, incorrectly) accepted are now rejected with
  a clear reason instead.
- Does not change which transitions require the elevated key, nor any server-side
  authorization check in `internal/protocol`. This is a client-side refusal to *attempt* a
  signature under ambiguous local conditions -- the server-side guarantees this project
  already relies on for its strongest protections (the four elevated-key-gated transitions)
  were never affected by the underlying gap in the first place, and remain unchanged here.

## Test and rollout plan

- Unit tests for `UserConfig.ProfileCountForProject` and the `Service.AmbiguousActor` gate in
  `ExecuteWithPassphrase` (refuses when set, passes through unchanged when not).
- Live reproduction, not just unit tests: simulate the exact reported scenario (a project
  with 2+ local profiles, an invocation with no session ID and no explicit actor) and confirm
  it's refused with a clear message; confirm a single-profile project and every other
  resolution source remain unaffected.
- Full existing test suite, `go vet`, `staticcheck`, and `go test -race` stay clean on every
  touched package.

## Unresolved questions

- None at write time; this is a narrowly-scoped, single-choke-point addition with no open
  design questions. Revisit only if a future write path is found that legitimately needs to
  bypass `Service.Execute` entirely (none currently exist).
