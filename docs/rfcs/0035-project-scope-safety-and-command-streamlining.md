# RFC 0035: Project-scope directory safety and command streamlining

## Status

**Implemented, 2026-09-19.** The project owner requested this directly after a
v0.7.0 patch bug (`agc update apply` crashing on a stray, non-project
`.agent-comms` directory — see RFC-less fix commit `13d8cf0`) exposed both
a confusing error-message gap and a command surface with real redundancy.
Design discussed and approved by the project owner in conversation before
implementation began, per `docs/rfcs/README.md`.

This RFC changes the human-facing CLI command surface (removes and merges
public commands) and where several files are stored on disk (a storage
transaction concern), both on this project's RFC-required list.

## Problem and desired outcome

Two related problems surfaced from the same incident:

**1. A `projectRequired` command run outside any project leaks an internal
error instead of guidance.** `classifyProjectScope` (RFC 0027 §12) already
cleanly classifies every command into `projectRequired` / `projectOptional`
/ `projectUserOnly` / `projectExempt` — that architecture is sound. What's
missing: for the default `projectRequired` case, `PersistentPreRunE` never
checks "is this actually an initialized project" before handing `root` to
`projectlifecycle.Reconcile`, which fails with a raw filesystem error.
Reproduced live: `agc task list` in an empty directory today prints
`error [VALIDATION]: lstat /tmp/.../.agent-comms: no such file or
directory`, with the generic hint "Run the command with --help to review
required flags" — actively misleading, since the problem has nothing to do
with flags.

**2. `projectExempt` commands write into whatever directory they're given,
without regard for whether it's a real project.** `live serve`/`live
attach` are deliberately project-independent (a live broker can reasonably
run against a Codex/Claude session outside any agent-comms-managed repo),
but their session caches — `claudeserve`, `codexserve`, `opencodeclient`,
and `sessionbind` — write literally `<cwd>/.agent-comms/cache/*.json`, the
exact directory name a real project uses for its own managed state. This is
confirmed to be the actual origin of the incident: a stray
`.agent-comms/cache/runtime-sessions.json` in the project owner's own
`$HOME` (no `config.json` next to it, meaning `init` was never run there)
was left by an earlier session-bound command run from that directory, and
was indistinguishable from a real project to any code that only checked
"does `.agent-comms/` exist."

**3. The command surface has real, not-cosmetic redundancy.** `update
check` and `update apply` are two steps a human always wants combined —
"is there an update, and if so, install it" — with no reason to run them
separately outside of scripting. `project upgrade status` and `project
upgrade plan` are, on inspection, the exact same code registered under two
names with no behavioral difference at all.

Desired outcome: a `projectRequired` command run in the wrong place fails
immediately with an accurate, actionable message; no command writes project
look-alike state into a directory nobody asked it to manage; and the two
confirmed-redundant command pairs collapse into one each.

## Proposed design

### 1. Guided error for a misplaced `projectRequired` command

In `PersistentPreRunE` (`internal/app/app.go`), when `scope ==
projectRequired`, check `initializedProject(root)` (the same helper fixed
in commit `13d8cf0` to require `config.json`, not just the directory)
first — before `reconcileUserInstallation` and before
`projectlifecycle.Reconcile` — so a misplaced command fails fast without
wastefully reconciling other, unrelated registered projects first. On
failure, return a new `*projectlifecycle.Error`:

- `Code: CodeNotAProject` ("NOT_A_PROJECT"), added alongside the existing
  `CodeUpgradeRequired`/`CodeProjectTooNew`/etc. in
  `internal/projectlifecycle/types.go`.
- `Message`: names the exact directory checked, e.g. `"%s is not an
  Agent Comms project"`.
- A new `errorHint` case in `internal/app/app.go` (alongside `VALIDATION`,
  `AUTHORIZATION`, etc.): *"Run `agent-comms init` here to start a new
  project, or run this command from an existing one."*
- `failure.ExitStatus` maps `NOT_A_PROJECT` to exit 2, grouped with the
  existing `VALIDATION` family — this is fundamentally a usage error, not a
  new severity class.

No behavior changes for any command already inside a real project; this
only replaces one confusing error with one accurate one for the
already-broken case.

### 2. Session caches move out of candidate project directories

`claudeserve`, `codexserve`, `opencodeclient`, and `sessionbind` stop
writing `<root>/.agent-comms/cache/*.json`. Instead, each adopts the
hashing technique `internal/interactiveserve.SocketPath` already uses —
hash `(root, sessionKind)` into a filename, never write inside the
directory a broker happens to be launched from — but targets a location
appropriate for cache data rather than `SocketPath`'s own target: `identity.ConfigDir()`
(alongside the existing `lifecycle.json`/`host-id`), not the ephemeral
per-boot temp directory (`os.TempDir()`-based, per-UID) `SocketPath` uses
for its live control sockets. A socket is meaningless after a reboot
anyway; a "where was this project's live broker last running" cache is
exactly the kind of thing `ConfigDir()` already exists to hold. This
applies uniformly — not only when `root` isn't a real project — keeping
one code path instead of two.

`projectRequired` commands that also touch `sessionbind`
(`runtime bind-session`, `interactive-serve`) don't need this move on their
own account — item 1 already stops them from reaching a write at all when
misplaced — but they move to the same shared location too, for consistency
and so every session-cache reader/writer agrees on where to look.

This is a clean break: existing cache files at the old
`<project>/.agent-comms/cache/*.json` location become inert. They are
disposable session cache (a live broker re-establishes them on next use,
same as today), never durable project data, so there is nothing to
migrate.

### 3. `update check` + `update apply` → `update`

One command, `agent-comms update`, replacing both subcommands:

1. Always checks first (today's `update check` logic).
2. If a newer verified release exists and the session is interactive: shows
   `current → latest` and prompts `Update available: vX → vY. Install?
   [y/N]`. A no answer exits 0 with nothing changed.
3. `--yes`/`-y` or `--non-interactive` skips the prompt and installs
   immediately when available (for scripts and CI) — same flag semantics
   `update apply --yes` already has today.
4. If already current, reports that and exits 0 — no prompt.
5. `--channel`, `--version`, `--all-known`, `--current-project-only`,
   `--skip-project-upgrade` all carry over unchanged in meaning.

No hidden alias for the old two-step form: a script that wants
check-then-decide-separately still can, by parsing `update`'s own
`--json`/`--output json` result before it would otherwise prompt (checking
happens before the prompt in every mode) — a separate `check` subcommand
adds no capability that doesn't already exist through `--json` plus
`--non-interactive`, only a redundant path.

### 4. `project upgrade plan` / `project upgrade status` → `project upgrade plan`

Drop `status` as a name; `plan` is kept as the sole entry point for the
existing (verbatim-unchanged) dry-run behavior. `plan`'s existing `--yes`,
`--all-known` flags and JSON shape are unaffected.

## Alternatives considered

- **Cache relocation, project-local when possible / fallback otherwise**:
  keep writing inside `<root>/.agent-comms/cache/` when `root` is
  confirmed to already be a real project, only redirect to the shared
  location otherwise. Rejected: two code paths to maintain and test for a
  file that is pure disposable cache either way, against this project's
  own established clean-break precedent (RFC 0027, RFC 0031, RFC 0034).
- **`update apply` kept as an undocumented scripting alias** for the
  merged `update`: rejected as an unrequested extra surface — `--json`
  plus `--yes`/`--non-interactive` already covers every scripting need the
  alias would have, and the project owner confirmed a single command is
  preferred.
- **Widening `projectExempt` commands to require a real project too**
  (closing the stray-write path by narrowing what they can run against,
  instead of relocating their caches): rejected — `live serve`/`live
  attach` working outside an agent-comms project is an intentional,
  existing capability (a broker for a bare Codex/Claude session), not a
  bug to remove.

## Compatibility and rollout

Breaking, pre-1.0, no deprecation aliases — consistent with every prior CLI
consolidation in this project:

| Removed | Replacement |
| --- | --- |
| `update check` | `update` (always checks first) |
| `update apply` | `update` (`--yes`/`--non-interactive` to skip the prompt) |
| `project upgrade status` | `project upgrade plan` |

A stray `.agent-comms/cache/*.json` from a prior version is simply never
read again after this ships; nothing to clean up, nothing to migrate. A
`.agent-comms/cache/` directory with no `config.json` next to it already
fails `initializedProject`'s check (commit `13d8cf0`), so it cannot be
mistaken for a project either way.

## Security and privacy implications

Net positive: session-cache files (which can carry a live provider session
ID) no longer get written into arbitrary, potentially unrelated directories
a broker happens to be launched from — they live in one user-level location
with the same access-control expectations as the rest of `ConfigDir()`
(host ID, lifecycle state). No change to signing, authority, or approval
behavior.

## Test and rollout plan

- A regression test asserting a `projectRequired` command (e.g. `task
  list`) run against a non-project directory returns `Code ==
  "NOT_A_PROJECT"` with the new message, not a raw filesystem error.
- A regression test confirming a `projectRequired` command run against a
  directory with a stray `.agent-comms/cache` (no `config.json`) gets the
  same clean `NOT_A_PROJECT` error, not a crash — the direct CLI-level
  counterpart to commit `13d8cf0`'s unit-level fix.
- Cache-relocation tests per adapter (`claudeserve`, `codexserve`,
  `opencodeclient`, `sessionbind`): writing and reading back a session
  entry never touches the working directory at all — e.g. run against a
  `t.TempDir()` "root" and assert nothing is created inside it.
- `update`: an interactive-prompt test (approve/decline) and a
  `--non-interactive`/`--yes` test against a fake newer-release fixture,
  plus a "no update available" no-prompt case.
- `project upgrade plan`: existing `status`/`plan` test coverage
  consolidates onto `plan`'s name; docgen reference and CHANGELOG updated
  for both removed names.
- Full `go test ./...`, `go vet`, `agent-comms-docgen --check`, and a real
  built binary exercising: `task list` outside a project (guided error),
  `live serve` outside a project (no stray directory left behind), `update`
  with and without an available release, `project upgrade plan`.

## Unresolved questions

- Whether other, less obvious writers into `<workDir>/.agent-comms/` exist
  beyond the four identified here (`claudeserve`, `codexserve`,
  `opencodeclient`, `sessionbind`). `worker/declarative.go`'s
  `LoadProjectAdapters` reads (never writes) `<root>/.agent-comms/adapters`,
  and is reachable for `projectOptional` commands too, not only
  `projectRequired` ones — since it only reads and already treats a
  missing directory as "no adapters" rather than an error, it is not part
  of this problem, but worth a final grep sweep at implementation time to
  confirm nothing else was missed.
