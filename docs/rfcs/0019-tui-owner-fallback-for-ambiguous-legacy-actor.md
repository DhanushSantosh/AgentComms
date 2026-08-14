# RFC 0019: TUI resolves straight to OWNER when the legacy actor is ambiguous

## Status

**Accepted 2026-08-13**, direction confirmed by the project owner after live testing surfaced
the exact scenario this closes. Per `docs/rfcs/README.md` and `docs/development-workflow.md`'s
design-proposal rule, reviewed and accepted before implementation began.

## Context

RFC 0017 makes `Service.ExecuteWithPassphrase` refuse any governed write resolved via the
legacy, machine-wide `UserConfig.ActiveProfile` fallback whenever the project has two or more
locally-registered identities -- the fix for a real incident where one agent's write got
silently signed under a different agent's identity.

Confirmed live, immediately after shipping RFC 0018: the project owner, running `agent-comms
tui` in a plain terminal against a project with several locally-registered identities
(`Dhanush`, `HADES`, `THOR`, `ZEUS`), hit this exact refusal trying to approve a HUMAN-tier
approval -- as themselves, the actual owner, sitting at the keyboard. `agent-comms tui` has no
recognized provider session ID any more than a session-less agent script does (it's launched
as a plain terminal program, not from inside a Claude Code or Codex session), so it falls into
the identical `ActorSourceActiveProfile` tier RFC 0017 targets, and gets refused identically.

Two paths were considered and rejected before landing on this one -- see Alternatives below.
The one that held up: **the risk RFC 0017 defends against does not exist for the TUI at all.**
That risk is specifically a session-less script or agent silently signing under whichever
identity happens to be sitting in the shared legacy slot, with no human present to notice.
`agent-comms tui` requires a real, attached TTY, running bubbletea's raw-mode input loop --
nothing session-less can drive it. Every real invocation of it has a human physically present,
watching the resolved actor shown continuously in the interface, with a one-keypress way
(the actor-switch form) to correct it before anything signs. The CLI, MCP, and worker paths
remain exactly as exposed to that risk as before -- those genuinely are how session-less
agents and scripts interact with a project, and RFC 0017's refusal stays fully in effect for
all three, unchanged.

## Proposed design

`identity.ActorResolutionRequest` gains one new field:

```go
// PreferOwnerOnAmbiguousLegacy, when true, skips the legacy machine-wide
// ActiveProfile fallback entirely whenever it would otherwise be
// genuinely ambiguous (2+ locally-registered identities for this
// project, no recognized provider session) and resolves straight to
// ProjectOwner instead -- see RFC 0019. Only ever set by the TUI's own
// entry point: a real, attached, interactive terminal session nothing
// session-less could plausibly be driving, unlike the CLI/MCP/worker
// paths this does not apply to.
PreferOwnerOnAmbiguousLegacy bool
```

`identity.ResolveActor` consults it only inside the existing legacy-fallback branch (the one
reached when no explicit actor/profile/env/host-binding signal is present and
`ProviderSessionID == ""`): if the resolved legacy profile's project has two or more
locally-registered identities and this flag is set, it does not use that profile at all --
it falls through to the same `ActorSourceProjectOwner` result the function already returns
whenever the legacy field is empty. Every other resolution tier (explicit actor/profile, env
var, host binding, and the RFC 0016 session-scoped tier for a real provider session) is
completely unaffected -- this only ever intervenes in the one specific tier RFC 0017 already
classifies as ambiguous.

`internal/app`'s `PersistentPreRunE` sets it to `cmd.Name() == "tui"` when constructing the
request -- the same per-command check already used to exclude `doctor` from `ensureDaemon`.
Because the resulting `ActorResolution.Source` becomes `ActorSourceProjectOwner` rather than
`ActorSourceActiveProfile` whenever this fires, `Service.AmbiguousActor`'s existing computation
(`Source == ActorSourceActiveProfile && ProfileCountForProject > 1`) is false automatically --
no separate change needed there, and no change needed anywhere `ActorResolution.Source` is
otherwise consumed (`profile current`'s reported source, the TUI's own status rail) either;
they already display whatever `ResolveActor` returns.

Explicit overrides still win over this, exactly as they do today: `--actor`, `--profile`,
`AGENT_COMMS_ACTOR`, a host-label binding, or a real provider session all resolve before this
tier is ever reached, so a human deliberately driving the TUI as a different identity is not
affected by any of this.

## Alternatives considered

- **Exempt any actor that resolves to a `HUMAN` principal from RFC 0017's guard.** Rejected:
  role/`PrincipalType` are validated server-side, against the authoritative signed state, only
  *after* a local credential is already chosen -- resolution itself cannot know in advance
  what it will land on. Worse, `docs/governance.md` already documents that an unregistered
  agent connection resolves to the project owner *by ambient fallback, indistinguishably from
  the owner acting directly* -- exempting "resolved as human" would hand that exact,
  already-known gap a free pass through the one guard built to catch a version of it, making
  the single most dangerous misattribution case (an agent silently signing as the human owner)
  strictly worse, not better.
- **Exempt the `OWNER` role specifically.** Rejected for the identical reason: the same
  ambient-fallback path can resolve an unregistered agent connection to the literal project
  owner today, by design, as a documented bootstrap convenience -- exempting `OWNER` exempts
  exactly that path too.
- **Segregate human and agent credential storage entirely** (a separate, possibly
  OS-auth-gated keyring for the human's own key, distinct from the one shared project keyring
  every registered actor's key lives in today). This is the actual root fix for the deeper,
  already-accepted limit RFC 0016's own Security section documents -- any process running as
  the same OS user can already read any registered actor's key, TUI or not. Deliberately out
  of scope here: a substantially larger undertaking (new storage backend, migration, per-OS
  auth integration), not something to fold into a narrow, immediate fix. Left as a candidate
  for its own future RFC if the project owner wants to close that root gap directly.

## Compatibility and rollout

Strictly narrows the set of situations RFC 0017 refuses a write in -- no existing safe
resolution becomes unsafe, and CLI/MCP/worker behavior is completely unchanged. The only
observable difference: `agent-comms tui`, launched with no explicit actor/profile/env/host
signal and no recognized provider session, in a project with 2+ locally-registered identities,
now resolves to the project owner and proceeds, where it previously refused outright with RFC
0017's ambiguous-actor error.

## Security implications

- Does not weaken RFC 0017's actual protection for CLI, MCP, or worker invocations in any way
  -- all three are exactly as exposed to session-less misattribution as before, and the
  refusal still fires for every one of them under the identical conditions.
- The TUI's continuous, on-screen display of the resolved actor (already shipped) remains the
  operative safety mechanism for this path: a human who notices the wrong identity in the
  status rail still has the same one-keypress actor-switch correction available before signing
  anything. This RFC does not add or remove that mechanism -- it only stops a refusal from
  standing in front of it for the one interface where it was never actually needed.
- Explicitly does not attempt to close the shared-keyring limit described in Alternatives.
  Anything with genuine local shell access as the operating OS user could already extract and
  sign with any registered actor's key directly, through the CLI, independent of this change.

## Test and rollout plan

- Unit coverage in `internal/identity` for `ResolveActor` with `PreferOwnerOnAmbiguousLegacy`:
  redirects to `ActorSourceProjectOwner` only when the legacy tier is genuinely ambiguous
  (2+ profiles); leaves a 0-1-profile (unambiguous) legacy resolution completely unchanged;
  never fires when any earlier tier (explicit actor/profile/env/host binding) or the RFC 0016
  session-scoped tier already resolved the request.
- End-to-end coverage in `internal/app` confirming `agent-comms tui`'s own `PersistentPreRunE`
  wiring sets the flag (`cmd.Name() == "tui"`) and that `Service.AmbiguousActor` ends up false
  for exactly this case, with an equivalent CLI invocation (a non-`tui` command under the
  identical ambiguous conditions) still refusing exactly as RFC 0017 specifies.
- `go test -race`, `staticcheck`, `gofmt`, `scripts/coverage-floor.sh` all clean before
  shipping, matching this session's established verification bar.

## Unresolved questions

None outstanding for this narrow scope. The credential-storage segregation idea from
Alternatives remains open as a candidate for a future, separate RFC.
