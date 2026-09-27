# RFC 0039: Actor IDs name the provider; display names stay free

## Status and owners

**Proposed, 2026-09-27.** Requested by the project owner; drafted by
claude-main. Changes the public `agent register` contract, so it requires
review before implementation.

## Problem and desired outcome

`agent register --id` accepts any string. Nothing ties a principal's
identity to the AI provider actually behind it, so what a project calls an
agent is pure convention, unenforced and unparseable.

This project already settled on a convention by hand — `claude-main`,
`codex-main`, `antigravity-main` — which is evidence the shape is wanted.
But because nothing enforces it, a reader cannot rely on it, and tooling
cannot derive the provider from the identity. Two agents named `reviewer`
and `builder` tell you nothing about which runtime is answering, which
matters when behaviour, rate limits, and failure modes differ per provider.

Desired outcome: an AGENT principal's actor ID always begins with the
provider that backs it. Display names stay free-form for humans to read,
and either name can be used to refer to a principal.

## Proposed design

### 1. Actor ID grammar (AGENT principals only)

    <provider>            e.g. claude
    <provider>-<suffix>   e.g. claude-main, codex-reviewer

Lowercase; `<suffix>` is `[a-z0-9][a-z0-9-]*`. HUMAN principals are
unaffected and keep free-form IDs — a person is not a provider, and the
owner `dhanush` must stay valid.

Every actor ID already in this project satisfies this, deliberately: the
grammar was chosen to ratify the existing convention rather than force a
rename.

### 2. Providers are an explicit, extensible list

`claude`, `codex`, `opencode` to start. Not derived from
`internal/worker.builtInAdapters`, because that list contains transport
variants (`claude-acp`, `codex-live`) which are not separate providers, and
a principal named `claude-acp-main` would be wrong.

A project that registers a declarative adapter
(`.agent-comms/adapters/<name>.json`) extends the accepted set with that
name, so custom providers work without a code change — the same escape
hatch the adapter system already provides.

### 3. `--provider` becomes the primary flag; `--id` becomes optional

    agent-comms agent register --provider claude
    agent-comms agent register --provider claude --id claude-reviewer
    agent-comms agent register --provider claude --display-name "Atlas"

- `--provider` is required for `--principal-type AGENT`.
- `--id` defaults to `<provider>`, or `<provider>-2`, `-3`, … when taken.
- A supplied `--id` must match the grammar for that provider, or the command
  fails naming both what was given and what was expected.
- `--display-name` stays optional and free-form, exactly as today.

### 4. Referring to a principal by either name

A reference resolves in this order: exact actor ID, then case-insensitive
display name. An ambiguous display name is an error listing the candidates
rather than a guess.

Resolution happens at the CLI/service boundary, never in the protocol.
**Events always record the canonical actor ID.** A signed record that stored
"Atlas" would become ambiguous the moment a display name is reused or
renamed, and `agent rename` already exists.

Applies wherever a principal is named today: `--to`, `--actor`, task
ownership, invocation targets.

### 5. Enforcement point

In `ValidateTransition` for `agent.register`, gated on
`PrincipalType == PrincipalAgent`. That is the validator shared by CLI, MCP,
TUI and both authority backends, so one change covers every entry point.

Replay is unaffected: `internal/projection.ApplyEvent` does not call
`ValidateTransition`, so historical registrations of non-conforming IDs
continue to project exactly as before.

## Compatibility and rollout

Existing principals are untouched and keep working — validation applies to
new registrations only. No migration, no schema change, no project-format
bump.

The one real break: a project that today registers `--id reviewer` will be
refused after this lands, and must say `--provider claude --id
claude-reviewer`. That is the intended correction, and the error message
should show the exact replacement rather than only stating a rule.

`antigravity` is deliberately not in the starting provider list — its
adapter was removed (see `docs/backlog.md`). The existing
`antigravity-main` principal is grandfathered and keeps working; a project
that wants to register new ones adds a declarative adapter by that name.

## Alternatives considered

- **Convention only, documented but unenforced.** The status quo. It is what
  produced the current inconsistency, and leaves tooling unable to trust the
  format.
- **Provider as a separate field, not encoded in the ID.** Cleaner in theory
  and rejected for one practical reason: the actor ID is what appears in
  every event, table, log line, and error message. A field nobody sees does
  not answer "which runtime produced this event" when reading history. A
  separate field could still be added later as metadata; it does not replace
  putting the provider in the name.
- **Auto-suffix everything (`claude-1`, `claude-2`).** Guarantees uniqueness
  but throws away the human meaning in `claude-reviewer`, which is the part
  that makes a multi-agent project legible.

## Security and privacy implications

None meaningful. Actor IDs are already public within a project and appear in
the signed log. The grammar narrows what can be registered; it grants
nothing. Display-name resolution is a lookup convenience at the CLI
boundary and cannot cause an event to be attributed to the wrong principal,
because events still carry the canonical actor ID and every command remains
signed by the principal's own key.

## Test and rollout plan

- Grammar: `claude`, `claude-main`, `codex-reviewer` accepted;
  `reviewer`, `Claude-Main`, `claude_main`, `claude-` rejected.
- HUMAN principals keep free-form IDs; `dhanush` still registers.
- `--id` omitted defaults to `<provider>`, then `<provider>-2` when taken.
- A mismatched `--id` names both the given and the expected form.
- Declarative adapter registration extends the accepted provider set.
- Replay: a projection of historical events containing a non-conforming
  registration still applies cleanly.
- Resolution: display name resolves; ambiguous display name errors with
  candidates; the resulting event records the canonical actor ID.

## Discovered while implementing: the prefix does not fit the TUI

Implementing the grammar surfaced a cost worth recording before this is
accepted. `internal/tui/view.go:643` truncates the agent name to 13
characters in the workforce table. `claude-` alone is 7 of those, leaving
six for the part that distinguishes one agent from another:

    reviewer          -> reviewer          (fits today)
    claude-reviewer   -> claude-review...
    claude-developer  -> claude-develo...

This is not hypothetical. `cmd/agent-comms-tui-wasm/seed.go` documents the
same lesson already learned once: its demo IDs were chosen short
specifically because "the 'agent-'-prefixed forms first tried here got cut
to 'agent-develo...' mid-word". Mandating a provider prefix reintroduces
that for essentially every agent.

Two fixes, and they compose:

1. **Show the display name where one exists**, falling back to the actor ID.
   This is already half the requested behaviour -- display names are the
   human-facing label, actor IDs the canonical one -- and it removes the
   pressure on the column entirely for any agent that has one.
2. **Widen the column**, or truncate from the middle (`claude-...viewer`)
   so the distinguishing half survives rather than the prefix every agent
   shares. Truncating a common prefix is the worst of both: it costs
   characters and conveys nothing.

Whichever is chosen, it belongs in this RFC rather than as follow-up: a
naming rule that makes the primary UI unreadable is not finished.

## Unresolved questions

1. Should `agent rename` also be able to change the actor ID for a
   grandfathered principal, or is rename display-name-only forever? Renaming
   an actor ID rewrites identity across the whole signed history and is
   probably never safe, but the question should be answered explicitly.
2. Should `doctor` report principals whose IDs predate this grammar, as a
   nudge rather than an error?
