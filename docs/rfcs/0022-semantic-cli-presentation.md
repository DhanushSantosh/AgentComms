# RFC 0022: Semantic CLI presentation and output contracts

## Status

**Accepted, 2026-08-25.** Owner: Dhanush Santosh. Implementation branch:
`codex/cli-ux-redesign`. The project owner accepted the output contract and
the four public test seams before implementation began.

This RFC changes the human-facing CLI contract and adds output modes, so it
requires review before implementation under `docs/rfcs/README.md` and
`docs/development-workflow.md`.

## Problem and desired outcome

Agent Comms has one strong machine contract and one weak human default. The
versioned `--json` envelope is consistent and scriptable, but 74 successful
command paths call the generic `emit` function and, unless they are one of
the three list commands migrated to `emitTable`, print `json.MarshalIndent`
output to a human terminal. A user sees backend/event representation rather
than the outcome they asked for.

The desired CLI:

1. communicates what happened, the resulting state, and the most useful next
   action without requiring the user to interpret JSON;
2. has a recognizable Agent Comms visual language grounded in its governance
   domain rather than copying another product's styling;
3. preserves the existing `--json` envelope and exit-code behavior for
   automation;
4. remains clean and deterministic when redirected, piped, color-disabled,
   or run on a limited terminal;
5. gives bounded commands, streams, diagnostics, and destructive workflows
   output forms appropriate to their semantics.

Research against the official OpenCode and OpenClaw documentation and source
is recorded in `docs/cli-ux-reference-research.md`.

## Proposed design

### 1. Output modes are explicit contracts

Add a canonical persistent flag:

```text
--output human|plain|json|jsonl
```

The modes are:

- `human`: the default for an interactive terminal. Concise summaries,
  semantic styling, responsive tables, and TTY-only progress are allowed.
- `plain`: stable, uncolored, non-animated text. This is also the automatic
  presentation fallback when human output is redirected or `TERM=dumb`.
- `json`: exactly one versioned JSON document for a bounded command. The
  existing `Envelope` is retained unchanged.
- `jsonl`: one versioned event per line for commands whose natural result is
  a stream. It is rejected for bounded commands until a command explicitly
  declares support.

`--json` remains a supported alias for `--output json`. Supplying conflicting
output selections is a usage error. `--no-color` and the `NO_COLOR`
environment variable affect human rendering only. JSON/JSONL never contain
ANSI, progress, headings, or prose outside their schemas.

`--quiet` retains its current meaning for human/plain modes: suppress
non-essential success output. It never silently overrides an explicitly
requested JSON/JSONL result. `--verbose` exposes operational metadata and
`--details` exposes secondary/nested result fields; neither changes JSON,
whose schema remains complete.

### 2. One presentation boundary

Introduce `internal/cliui` as the terminal presentation module. Command
handlers continue to obtain typed domain/service results, then pass a
semantic presentation description to one presenter. The presenter owns:

- output-mode selection;
- TTY, width, color-profile, Unicode, and hyperlink capability detection;
- stdout/stderr routing;
- semantic styling;
- responsive tables and detail blocks;
- progress lifecycle;
- error presentation.

The module exposes a small, typed vocabulary rather than accepting arbitrary
Go values as its primary interface:

- result summary and key/value fields;
- status (`success`, `info`, `warning`, `danger`, `muted`);
- table with column priority and compact renderers;
- timeline entries;
- detail section;
- next-action hint;
- progress event.

A readable reflection-based tree may exist only as a temporary fallback for
unmigrated command shapes. Important commands receive intentional views.

`internal/app` remains the CLI adapter and owns Cobra wiring. Domain, service,
protocol, authority, and projection packages do not import `internal/cliui`.

### 3. Agent Comms design language

The semantic palette is deliberately small:

- accent/cyan: identities, commands, references, and selected values;
- success/green: verified, active, completed, and delivered;
- warning/amber: waiting, stale, degraded, or approval required;
- danger/red: failed, rejected, revoked, or integrity failure;
- muted: timestamps, fingerprints, and secondary metadata.

Color never carries meaning alone. Statuses include a label and may include a
capability-appropriate glyph. Limited terminals fall back to ASCII. Existing
Lip Gloss, ANSI, terminal, and display-width dependencies are preferred over
adding a second terminal styling stack.

Human views follow several stable patterns:

- mutations: outcome heading, important resulting fields, next action;
- lists: responsive table, count/empty state, filters when relevant;
- details: identity/status header, grouped fields, optional nested details;
- status/doctor: grouped health summary with actionable remedies;
- history/delivery: chronological timeline;
- destructive operations: explicit target/risk summary before confirmation;
- streaming operations: compact live progress or stable event lines.

### 4. stdout, stderr, errors, and progress

Requested result data is written to stdout. Warnings, progress, recovery
notices, and diagnostics are written to stderr. Human failures contain a
concise summary, stable error code when available, and an actionable hint
when the classifier can provide one. They do not show a stack trace by
default. Failures retain their existing non-zero exit status.

The JSON error envelope remains on stderr for compatibility with current
behavior. Changing that stream would be a separate contract proposal.

Progress is enabled only when stderr is an interactive terminal and output is
human. It is disabled for plain, JSON, JSONL, `TERM=dumb`, and redirected
stderr. Cleanup always restores cursor state on success, error, cancellation,
or interrupt.

### 5. Streaming contract

Bounded commands continue to produce one JSON envelope. Natural streams such
as `watch`, invocation listening, worker status, and live adapter attachment
may opt into JSONL. Each line carries an API version, command, event type,
timestamp, and typed payload. JSONL is added only with a schema and contract
tests; it is not produced by splitting existing pretty JSON across lines.

The same semantic event should feed TTY progress, plain event lines, and JSONL
serialization. This keeps behavior aligned and leaves the existing full TUI
free to consume the same concepts later without coupling it to CLI strings.

## Command-output inventory and migration groups

The current 74 generic emit sites and three table sites divide into these
user-facing groups:

1. **Foundation/reference slice:** `version`, `status`, `doctor`, `task list`,
   `task claim`, `invocation inspect`, `invocation request`, `artifact verify`.
2. **Read/list/detail:** agents, runtimes, invocations, tasks, inbox, approvals,
   decisions, documents, artifacts, drafts, environment entries, profiles,
   configuration, history, search, control views, and project upgrade plans.
3. **Mutations:** identity lifecycle, task lifecycle, message/decision/
   approval transitions, runtime configuration, documents/artifacts/drafts,
   project settings, update, upgrade, and deletion.
4. **Streaming/long-running:** watch, invocation listen, runtime worker,
   Claude/Codex attach and serve surfaces, interactive runtime serving,
   update/download, and multi-project upgrade.
5. **Pass-through/export:** MCP stdio, shell completion, JSONL/Markdown export,
   and wrapped provider processes. These retain their protocol/native output
   and do not receive decorative rendering.

Every command is assigned a group and explicit human shape before migration.
No command is considered migrated merely because generic JSON was colorized.

## Alternatives considered

### Colorize and syntax-highlight the existing JSON

This is inexpensive and preserves every field, but it leaves backend shape as
the human information architecture. Rejected as the primary design. It may be
offered as a detail/fallback renderer during migration.

### Add bespoke `fmt.Printf` output inside every command

This gives each command full control but repeats terminal detection, styling,
width handling, warnings, and compatibility decisions across dozens of call
sites. Rejected because those cross-cutting policies would drift.

### Adopt a large third-party CLI/TUI framework

The project already has Cobra, Bubble Tea, Lip Gloss, terminal detection, and
display-width support. A new framework would not define the missing semantic
contract and could duplicate the existing TUI stack. Rejected for the
foundation; individual narrowly-scoped dependencies can be proposed later if
the existing stack cannot meet a measured need.

### Make the full-screen TUI the default for every interactive invocation

Agent Comms already has a capable TUI, but many workflows are one-shot shell
commands, agent subprocesses, or documentation examples. Rejected as part of
this RFC. Whether bare `agent-comms` launches the TUI is a separate behavior
decision after the command presentation model is stable.

### Break or replace the existing JSON envelope

Rejected. It is a public automation interface with existing tests and docs.
New machine formats are additive.

## Compatibility and rollout

- Existing `--json` output remains the same versioned `Envelope`, including
  its current stdout/stderr placement and error classification.
- Existing exit codes remain unchanged.
- `--no-color`, `--non-interactive`, `--quiet`, `--project`, `--profile`, and
  `--actor` remain supported.
- Human output changes intentionally and is not promised byte compatibility.
- Plain mode becomes the stable choice for scripts that intentionally parse
  text rather than JSON.
- Commands migrate by vertical slice. Unmigrated commands use a readable
  unstyled fallback; they do not block early delivery of migrated commands.
- Documentation generation continues to use the real Cobra tree and gains
  the new output flags automatically.

Rollout is split across reviewable changes:

1. presentation context and output-mode contract;
2. reference vertical slice;
3. read/list/detail migrations;
4. mutation and governed-workflow migrations;
5. streaming JSONL and progress;
6. help, examples, completion, and consistency audit.

## Security and privacy implications

- Human summaries must not reveal fields that the current command does not
  authorize. Presentation receives only already-authorized result values.
- Secrets and private-key material remain forbidden from output in every
  mode. A verbose/detail view is not permission elevation.
- Terminal escape sequences in user-controlled values must be stripped or
  escaped before rendering to prevent terminal-control injection.
- OSC-8 links accept only validated destinations and always have a plain-text
  fallback.
- Progress and diagnostics must never contaminate JSON/JSONL stdout.
- Destructive commands keep their existing authorization, elevated-key, and
  confirmation requirements; presentation cannot weaken protocol checks.

## Test and rollout plan

The agreed public test seams are:

1. `app.Run(args, stdout, stderr)` for end-to-end CLI behavior;
2. stable `--json` envelopes for machine compatibility;
3. human/plain golden output under explicit terminal capabilities and widths;
4. the exported `cliui.Presenter` contract, without testing private helpers.

Required gates:

- red/green vertical-slice tests for every new output behavior;
- golden output at narrow, normal, and wide widths;
- zero ANSI/control bytes in plain, redirected, JSON, and JSONL output;
- `NO_COLOR`, `--no-color`, `TERM=dumb`, non-TTY, ASCII fallback, and Windows
  terminal coverage;
- JSON envelope compatibility and JSONL schema tests;
- stdout/stderr separation tests for results, warnings, progress, and errors;
- cursor/progress cleanup under success, failure, cancellation, and interrupt;
- focused `go test` during slices, then `go test ./...` and `go vet ./...`.

The reference slice is not complete until its commands are exercised through
the real `app.Run` seam in both human and JSON modes.

## Unresolved questions

1. Should non-TTY default output select `plain` automatically while
   `--output human` forces styled structure without color, or should explicit
   `human` retain color when stdout is redirected? The proposal recommends
   automatic plain behavior even when human is explicit, prioritizing safe
   pipes.
2. Should `--details` and `--verbose` be separate flags? The proposal treats
   details as result depth and verbose as operational metadata, but the first
   reference slice should validate whether users perceive that distinction.
3. Should bare `agent-comms` launch the TUI when attached to a terminal? This
   remains outside scope and should be decided after the new help and command
   output can be compared with the TUI entry experience.
4. Which streaming commands need JSONL in the first release? `watch` and
   `invocation listen` are the likely minimum; live provider attachment may
   be better treated as protocol/pass-through output.
