# RFC 0027: CLI surface consolidation and command help

## Status

**Proposed, 2026-09-02.** Owner: Dhanush Santosh. Implementation branch:
`review/cli-commands`. Awaiting owner acceptance before implementation, per
`docs/rfcs/README.md` and `docs/development-workflow.md`.

This RFC changes the human-facing CLI command surface -- it removes,
renames, and regroups public commands -- so it requires review before
implementation. It follows RFC 0022, which redefined the CLI's *output*
contract but left the command *tree* untouched.

## Problem and desired outcome

A full read of `internal/app` found 136 commands across ~40 groups. The
review surfaced four problems:

1. **Missing help.** ~90 of 136 commands have an empty `Short`.
   `configureRootHelp` (`internal/app/app.go`) fills in descriptions for
   top-level groups only, so every leaf -- `agent register`, `task
   create`, `invocation next`, `message post`, `approval approve` -- prints
   a blank line under `--help`. The onboarding guide tells agents to run
   `agent-comms <cmd> --help`; that guidance currently dead-ends.

2. **Redundant "what is the state" commands.** `status`, `control
   overview`, and `doctor` overlap. `status` and `control overview` print
   nearly the same counts-plus-integrity summary. `control settings`
   restates `config` plus a few constants.

3. **A shipped no-op.** `session heartbeat` builds a result map, prints
   `"durable": false`, and returns -- it never records anything. It is a
   command that does nothing.

4. **Misleading names.** `invocation wait` reads as a sibling of
   `invocation listen` / `invocation next` (receive work) but actually
   means "the worker is blocked, retry later." `search` implies a
   full-history search but only substring-greps the *current* history
   page.

Desired outcome: every command has a one-line purpose; the "state"
commands have one obvious entry point each; no command is a no-op; names
match what the command does.

## Proposed design

### 1. Command help (non-breaking, but specified here for completeness)

Every leaf command gains a `Short`. The commands whose semantics are not
obvious from the name -- the invocation lifecycle
(`request`/`next`/`listen`/`claim`/`start`/`defer`/`resume`/`complete`),
the approval tiers, `task lock`, `agent
elevate-key`/`revoke`/`suspend`/`delete` -- also gain `Long` and, where a
concrete call helps, `Example`. `agent-comms agent delete --help` must
state how it differs from `revoke` and `suspend`.

Delivered as data on the `cobra.Command` values, verified by a test that
walks the command tree and asserts no non-hidden command has an empty
`Short`. The generated docs-site reference (`internal/app/docs.go`) picks
these up automatically.

### 2. Fold `control` into `status` and `config`

Remove the `control` group. Its three subcommands move:

| Was | Becomes |
| --- | --- |
| `control overview` | `status --details` gains the per-status task/invocation/runtime breakdown |
| `control settings` | `config --details` gains the control-plane limits and per-agent invocation policies |
| `control attention` | new top-level `agent-comms attention` |

`control attention` is the one genuinely distinct command -- the
actionable queue of blocked tasks, pending approvals, waiting
invocations, failed deliveries, degraded runtimes. It graduates to a
top-level verb rather than being buried.

### 3. Remove `search`; add `history --grep`

`search` is deleted. `history` gains `--grep <substring>` (case-insensitive
match against the rendered event record) and `--all` (page through the
whole log rather than one page). `history --grep X --all` is the search
that `search` implied but never was. `history` keeps its existing
`--actor` / `--key-fingerprint` / `--cursor` / `--limit` flags.

### 4. Rename `invocation wait` -> `invocation defer`

The CLI verb becomes `defer`. The underlying event type stays
`invocation.wait` (renaming a durable event type is a schema change and
out of scope). `--reason` and `--retry-in` are unchanged. Callers
updated: `internal/onboarding/decision_tree.tmpl`,
`docs/site/agents/invocations.md`, any `--json` examples. MCP's
`invocation_wait` tool name is left as-is for this RFC (MCP tool renames
are tracked separately); only the CLI verb changes.

### 5. Remove `session heartbeat`

Deleted outright -- it records nothing. `session start` and `session end`
stay (they emit real `session.start` / `session.end` events). If a
durable session liveness signal is wanted later it is a new, real command
with its own event, not a resurrection of this stub.

### 6. Move `claude` and `codex` under a `live` group

`agent-comms claude serve|attach|tail` and `agent-comms codex
serve|attach` become:

```
agent-comms live serve  --provider claude|codex   [--listen ...]
agent-comms live attach --provider claude|codex   --runtime ... [--server ...]
agent-comms live tail   --session ... [--project-dir ...] [--no-replay]   # claude only; errors for --provider codex
```

Rationale: `claude` and `codex` as top-level verbs imply the product is
about those two tools, and they pin two of the three supported providers
(`opencode` has no such group) as first-class. A `live` group scoped by
`--provider` scales and reads as what it is -- the live-session broker.
The `claudeserve` / `codexserve` packages are unchanged; this is a
command-tree change only. `PersistentPreRunE`'s per-path exemptions in
`app.go` update to the new paths.

### 7. Move `theme set` under `config`

`agent-comms theme set --name X` becomes `agent-comms config theme <name>`
(`auto`, `dark`, `high-contrast`). One fewer top-level group for a
single-setting writer; `config` already *reports* the resolved theme, so
it is the natural place to set it.

### 8. Finish `draft` rather than cut it

The review flagged `draft` (`save`, `list` only) as half-built. It is
*not* removed -- `internal/draftstore`, `internal/tui/drafts.go`, and the
daemon all back a real TUI feature. Instead it is completed:

- `draft show --id <id>` -- render one draft.
- `draft delete --id <id>` -- remove one (new `draftstore.Store.DeleteDraft`).

This is additive and could ship outside this RFC, but is listed so the
review item is closed.

### 9. Uniform singular inspect

Add `show` to the domains that only have `list` today: `task show --id`,
`agent show --id`, `approval show --id`, `decision show --id`. Additive.
`invocation inspect` keeps its name (it shows delivery evidence, not just
the record) with `invocation show` added as a hidden alias for muscle
memory... **[open question -- see below]**.

### 10. Optional auto-generated IDs

`task create` and `approval request` and `decision create` make `--id`
optional; when omitted an ID is generated and printed, matching
`invocation request` / `message post` / `task lock`. Backward compatible
(an explicit `--id` still works).

### 11. `task claim` flag cleanup

`--worktree` becomes the documented canonical flag for the
working-directory lock. `--repo` stays as a hidden deprecated alias for
one release, then is removed. (This is the one place the RFC keeps a
transitional alias, because `--repo` was the name in the original
feedback that motivated the feature and is the more likely thing in
someone's shell history.)

## Alternatives considered

- **Keep `claude` / `codex` top-level, just add `opencode`.** Rejected:
  three provider groups with duplicated `serve`/`attach` trees is more
  surface, not less, and the next provider makes it four.
- **Deprecation aliases for every rename.** Rejected by the owner: the
  project is pre-1.0, `0.x` SemVer already disclaims stability, and hidden
  aliases carry their own long tail. Clean break, one CHANGELOG entry.
- **Leave `control` as the "operator" namespace.** Rejected: on the CLI
  it only ever duplicated `status` / `config` / a new `attention`; the
  operator's real console is the TUI.
- **Rename the `invocation.wait` event too.** Out of scope -- durable
  schema change, separate RFC if ever wanted.

## Compatibility and rollout

Breaking. All in one release, called out in `CHANGELOG.md` under a
**Breaking** heading with the old -> new mapping:

| Removed / renamed | Replacement |
| --- | --- |
| `control overview` | `status --details` |
| `control settings` | `config --details` |
| `control attention` | `attention` |
| `search <q>` | `history --grep <q> [--all]` |
| `invocation wait` | `invocation defer` |
| `session heartbeat` | (removed; was a no-op) |
| `claude serve\|attach\|tail` | `live serve\|attach\|tail --provider claude` |
| `codex serve\|attach` | `live serve\|attach --provider codex` |
| `theme set --name X` | `config theme X` |
| `task claim --repo` | `task claim --worktree` (`--repo` hidden alias for one release) |

Docs updated in the same PR: `docs/site/guide/projects.md`,
`docs/site/guide/maintenance.md`, `docs/site/agents/invocations.md`,
`internal/onboarding/decision_tree.tmpl`, and the regenerated
`sites/docs/src/generated/reference.json`.

No change to the `--json` / `--output` envelope, exit codes, event
schema, storage, or authority protocol.

## Security and privacy implications

None. No change to authorization, signing, approval gating, or what is
persisted. `attention` surfaces the same data `control attention` already
did. Removing `search`'s ad hoc JSON-marshal-and-grep slightly *reduces*
the chance of a future field leaking into a match unintentionally.

## Test and rollout plan

- Command-tree test: no non-hidden command has an empty `Short`.
- Golden-path tests updated for every renamed/removed command; new tests
  for `attention`, `history --grep`, `history --all`, `live` with each
  `--provider`, `config theme`, `draft show`, `draft delete`, and the
  four new `show` commands.
- `docs.go` reference generation re-run; `verify-content.mjs` link/anchor
  check must stay green after the docs edits.
- One squash-merged PR from `review/cli-commands` against `dev`.

## Unresolved questions

1. **`invocation inspect` vs `invocation show`.** Keep `inspect`
   (accurate -- it shows delivery evidence), add `show` as a hidden alias,
   or rename to `show` for consistency with the other domains? Leaning
   "keep `inspect`, no alias."
2. **`attention` as a top-level verb vs `watch --once`.** `watch` already
   streams operator-relevant changes; `attention` could be `watch --once
   --attention`. Leaning standalone `attention` -- it is a different
   shape (a categorized snapshot, not a stream).
3. **`live tail` for a non-claude provider.** Error, or silently no-op?
   Leaning hard error (`--provider codex` + `tail` is a user mistake).
