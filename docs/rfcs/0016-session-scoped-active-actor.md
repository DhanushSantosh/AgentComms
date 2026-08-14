# RFC 0016: Session-scoped active-actor resolution

## Status

**Implemented on `dev`, 2026-08-12.** Direction confirmed by the project
owner (chose the full session-isolation design over three lighter
alternatives — see "Alternatives considered"). Per `docs/rfcs/README.md`
and `docs/development-workflow.md`'s design-proposal rule, this RFC was
reviewed and accepted before implementation began; the design below was
built as proposed, with one real correction found during implementation:

- **`internal/runtimeinit` also cannot import `internal/sessionbind`**,
  for the identical reason `internal/service` can't (documented under
  Proposed design): `internal/service`'s own test build reaches it
  transitively (`retry_unix_test.go`, compiled as `package service`, →
  `internal/runtimeinit` → `internal/sessionbind` → `internal/worker` →
  `internal/service`) — confirmed live via `go vet ./...` and a full
  `go test -run NONE_MATCH_NOTHING ./...` compile-only pass across every
  package, not assumed. `saveProfile` (used by `agent-comms init`) uses
  `identity.DetectProviderSessionID` instead, the same narrower detector
  `internal/service.go`'s `Register` already had to use — so `init` and
  first-registration both use the Claude/Codex-only detector, while the
  broader declarative-adapter-aware `sessionbind.Capture` is used
  everywhere else (`internal/app`'s `PersistentPreRunE`, `profile use`,
  `profile list`).

## Context

Confirmed live: a human owner switched actors in the TUI, expecting it to
give them elevated standing back, and kept hitting "owner or orchestrator
role required" — because a *different* agent, running concurrently on the
same machine, had run `agent-comms profile use --name <itself>` for its
own ordinary convenience (so its own bare commands default to itself
without `--actor` on every call). That command silently changed the
default actor for *every* process on the machine, not just that agent's
own session.

Traced to the root: `identity.UserConfig.ActiveProfile` is a single string
in one JSON file (`~/.config/agent-comms/config.json` or platform
equivalent), read and written by every `agent-comms` process running under
that OS account — the human's own CLI/TUI, and every agent's, with zero
process- or session-level isolation. `internal/service.go`'s `Register()`
also auto-sets it the first time a principal self-registers with no active
profile yet, another ordinary, non-malicious action with the same
machine-wide side effect.

This is worse than a UI inconvenience: every registered actor's real
private key — the human's, and every agent's — lives in the *same shared
OS keyring* (`internal/identity.go`'s `KeyringStore`, keyed only by
`(projectID, actor)`, readable by any process on that account). So when a
bare command resolves to the wrong actor because of this shared-file
leak, it doesn't just *display* the wrong name — it gets **cryptographically
signed as that actor** and durably recorded that way in the tamper-evident
event log. An ordinary, well-intentioned convenience action by one agent
silently misattributes another session's real governed actions.

**What is *not* at risk**, checked specifically: the four transitions
gated behind the passphrase-protected elevated key (granting Orchestrator,
approving a HUMAN-tier approval, revoking an Orchestrator/HUMAN, deleting a
revoked principal) independently verify the signer is a genuine
`PrincipalType == HUMAN` server-side (`internal/protocol/transitions.go`).
An agent identity accidentally becoming the resolved actor cannot pass that
check no matter what the local default-actor state says, so this gap
cannot be used to escalate privilege or forge a human approval. It's
**ordinary governed work** — task claims, messages, plain activations —
that's exposed to silent cross-session misattribution.

## Proposed design

Replace the single machine-wide default with one scoped to the exact
session that set it, using a signal this project already relies on
elsewhere for the identical reliability property
(`internal/sessionbind.Capture`'s own doc comment): **Claude Code exports
`CLAUDE_CODE_SESSION_ID`, and Codex exports `CODEX_THREAD_ID`, to every
process either spawns** — publicly documented behavior of each CLI, not
something an agent has to remember to export itself, and (confirmed by
reading `sessionbind.Capture`'s existing use for a different purpose)
already proven reliable in this codebase. A plain human terminal, running
neither, simply has no such variable — which is exactly the case that
should keep behaving as it always has.

- `identity.UserConfig` gains `ActiveProfileBySession map[string]SessionProfile`
  (session ID → `{Profile string, SetAt time.Time}`) alongside the existing
  `ActiveProfile string`, which becomes the fallback used **only** when no
  recognized session ID is present at all.
- Two new methods centralize every read/write instead of duplicating the
  branch at each of the four call sites that touch this field:
  `UserConfig.ActiveProfileFor(sessionID string) string` and
  `UserConfig.SetActiveProfileFor(sessionID, profile string)`. Critically,
  `ActiveProfileFor` with a **non-empty** `sessionID` that has no entry
  returns `""` — it never falls through to the shared legacy field. That
  fallthrough is exactly the leak this closes; a session that hasn't set
  its own profile yet must resolve to the safe project-owner default, not
  inherit whatever another session happened to leave in the old field.
- `identity.ActorResolutionRequest` gains `ProviderSessionID string`;
  `ResolveActor`'s existing `ActiveProfile` step is replaced with a call to
  `ActiveProfileFor(request.ProviderSessionID)`, reporting the new
  `ActorSourceSessionProfile` source when a session ID was actually used
  (vs. the existing `ActorSourceActiveProfile` for the legacy-field case) —
  same position in the precedence chain, after host-label binding, before
  the project-owner fallback.
- **New, dependency-free `identity.DetectProviderSessionID()`** duplicates
  just the two harness-guaranteed checks (Claude, Codex) — not the full
  `sessionbind.Capture` (which also checks declarative-adapter session env
  vars). This exists because `internal/identity` is a zero-internal-dependency
  leaf package, and `internal/service` (which needs this for its
  auto-set-on-register path) cannot import `internal/sessionbind` without
  creating a real cycle: `service → sessionbind → worker → service` (`worker`
  imports `service`). `sessionbind.Capture` is refactored to call this new
  function for its own first two checks instead of duplicating them, so
  there is exactly one implementation of the Claude/Codex detection, not
  two independently-maintained copies.
- Every call site that reads or writes "the active profile" is updated to
  detect and pass a session ID: `internal/app`'s `PersistentPreRunE`
  (`sessionbind.Capture`, the full detection, feeds `ResolveActor`),
  `cmd_settings.go`'s `profile use`/`profile list` (`sessionbind.Capture`),
  `internal/runtimeinit`'s `saveProfile` (`agent-comms init`,
  `sessionbind.Capture`), and `internal/service.go`'s `Register` auto-set
  path (`identity.DetectProviderSessionID`, the narrower one, for the
  import-cycle reason above).
- `SetActiveProfileFor` opportunistically prunes `ActiveProfileBySession`
  entries older than a fixed TTL on every write, so the map doesn't grow
  forever across every agent conversation ever run on the machine.

## Alternatives considered

- **(Option A) Restrict the shared field to only ever point back at the
  project owner.** Closes the signing-misattribution risk with less code,
  but removes real, legitimate functionality: an agent (or a human running
  multiple local identities) genuinely benefits from "default my own bare
  commands to me" without typing `--actor` on every call, and that's
  exactly what full session isolation still provides, more precisely.
- **(Option B) Steer agents toward `AGENT_COMMS_ACTOR` (already
  session-scoped, since env vars don't leak to sibling processes) via
  documentation alone, no code change.** Investigated and rejected as
  insufficient on its own: Claude Code's own tool-call shells do not
  reliably persist a manually-exported variable across separate tool
  invocations within one conversation (only cwd persists) — an agent would
  have to pass `--actor` on literally every single command, which is the
  exact friction `profile use` exists to avoid. This is precisely *why*
  `profile use` gets used by agents in practice, and why fixing this
  requires meeting that real need safely rather than just discouraging the
  command.
- **(Option C) Add confirmation/warning friction to `profile use` without
  changing its scope.** Cheaper, but leaves the actual shared-global-state
  design in place — reduces how often the leak is triggered by accident,
  doesn't close it.
- **(Do nothing / leave as machine-wide.)** Rejected — this is a confirmed
  live defect with a real integrity consequence (misattributed signing),
  not a theoretical concern.

## Compatibility and rollout

- Existing `UserConfig.ActiveProfile` values keep working exactly as
  before for any process with no recognized provider session ID (plain
  human terminals) — no migration, no forced re-configuration, no data
  loss. `ActiveProfileBySession` starts empty and is populated only by
  future `profile use`/`init`/auto-registration calls made from within a
  recognized session.
- `profile use`/`profile list`'s JSON output gains a `session_scoped`
  boolean so a caller (human or agent) can tell at a glance whether the
  change/state it just saw is isolated to its own session or the shared
  machine-wide legacy default — closing the same "which mode am I in"
  visibility gap the TUI actor-ID fix closed for the status rail.
- No change to `AGENT_COMMS_ACTOR`, `--actor`, `--profile`, or host-label
  binding — all four remain exactly as reliable (or unreliable, for
  `AGENT_COMMS_ACTOR` across Claude Code tool calls) as before; this RFC
  only changes what the *next* step down in the precedence chain does.
- **Known, pre-existing, explicitly out-of-scope risk, not solved here**:
  `internal/tui/model.go`'s theme-toggle keybinding ("h") does a full
  load-mutate-`Theme`-save round trip of the entire `UserConfig` struct.
  Two processes racing a concurrent write to different fields could still
  have one clobber the other's `ActiveProfileBySession` update with a
  stale copy. This is a narrow, pre-existing, generic config-file
  read-modify-write race (not specific to this RFC's change, and not
  something `ActiveProfileFor`/`SetActiveProfileFor` can fix on their own
  without real file-locking infrastructure this config file doesn't have).
  Noted honestly rather than silently left for someone to rediscover; not
  blocking this fix, which closes the actual reported, confirmed-live
  defect.

## Security and privacy implications

- Directly closes a real signing-misattribution defect: ordinary governed
  actions (not the four elevated-key-gated ones, which were never at risk)
  can no longer be silently signed under the wrong local identity purely
  because of another session's own unrelated, non-malicious convenience
  action.
- Provider session IDs (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`) are
  used here exactly as `sessionbind` already uses them elsewhere in this
  codebase — as a local, non-authoritative routing/scoping key, never as
  proof of identity or part of the signed event chain. Nothing about
  transaction authorization changes; this only changes which *default*
  actor a bare, flag-less command resolves to.
- No new secrets or credentials are read, stored, or transmitted; this is
  purely a local JSON config file structure and lookup-key change.

## Test and rollout plan

- Unit tests for `UserConfig.ActiveProfileFor`/`SetActiveProfileFor`:
  session-scoped set/get round-trip, legacy-field fallback when
  `sessionID == ""`, a real session with no entry correctly returning `""`
  rather than falling through to the legacy field, and TTL-based pruning.
- Unit tests for `ResolveActor`'s new precedence step, including the
  `ActorSourceSessionProfile` vs. `ActorSourceActiveProfile` distinction.
- Live verification (not just unit tests): simulate two different session
  IDs setting two different active profiles against the same on-disk
  `UserConfig`, confirm each resolves independently and a plain
  no-session-ID resolution is unaffected by either — reproducing the
  originally reported scenario directly and confirming it no longer
  occurs.
- Full existing `internal/identity`, `internal/app`, `internal/service`,
  `internal/runtimeinit`, and `internal/sessionbind` test suites stay
  green; `go vet`, `staticcheck`, and `go test -race` clean on every
  touched package.

## Unresolved questions

- Whether `internal/tui/model.go`'s theme-toggle read-modify-write race
  (noted under Compatibility) is worth a real fix (e.g. a narrower
  theme-only persistence path that doesn't round-trip the whole
  `UserConfig`) is left open as a small, separate, non-blocking follow-up.
