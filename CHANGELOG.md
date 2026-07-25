# Changelog

All notable user-facing changes are documented here. This project follows [Keep
a Changelog](https://keepachangelog.com/en/1.1.0/) and Semantic Versioning.

## [Unreleased]

### Added

- A plain `opencode` worker adapter (direct CLI exec via `opencode run
  --format json`), closing a gap versus `claude`/`codex`, which have had a
  plain exec adapter as their default since day one — OpenCode previously
  only had `opencode-acp` and `opencode-live`. Confirmed live: the prompt is
  fed on stdin exactly like `claude`/`codex`; `--format json` gives a clean
  newline-delimited event stream to extract the final answer from;
  `OPENCODE_PERMISSION` set per-process (rather than through a live
  approve/deny callback, which this exec path has no channel for) reproduces
  the same `acceptEdits` contract (edits allowed, bash/web/task denied) the
  other two OpenCode adapters already enforce; a stale/invalid `--session`
  fails with a plain `Error: Session not found` line and is retried once
  with no session rather than failing the invocation outright. Unlike
  `opencode-acp`/`opencode-live`, this adapter supports `--model`.
- A `claude-live` worker adapter backed by one persistent Claude Code
  `stream-json` process and a loopback HTTP/SSE broker. `agent-comms claude
  serve` runs the broker explicitly, while `agent-comms claude attach
  --runtime <id>` provides a read-only terminal view of live turns. The broker
  uses fixed port 4097, probes it before spawning, resumes bound sessions after
  crashes, and rejects conflicting runtime registrations.
- A `codex-live` worker adapter, the same shape as `claude-live` for Codex:
  one persistent `codex app-server` process driven over JSON-RPC through a
  loopback HTTP/SSE broker (`agent-comms codex serve`, fixed port 4098,
  same probe-before-spawn and conflicting-registration protections), with
  `agent-comms codex attach --runtime <id>` for a read-only live terminal
  view. Unlike `claude-live`, `--session-id` is optional — Codex mints its
  own thread IDs, which are cached locally and reused automatically per
  runtime, the same convention `opencode-live` already uses.
- Optional `claude-acp`, `opencode-acp`, and `codex-acp` worker adapters that
  drive Claude, OpenCode, and Codex over the Agent Client Protocol (ACP)
  instead of a direct CLI exec, selectable via `runtime worker --adapter`. The
  existing `claude` and `codex` exec adapters are unchanged and remain the
  default.
- An `opencode-live` worker adapter that drives OpenCode through a
  persistent `opencode serve` instance instead of ACP, watchable live in a
  terminal with `opencode attach`, for when a runtime's activity needs to be
  visible live rather than only after completion.
- `agent-comms agent rename --id <id> --display-name <name>` to correct or
  update a registered agent's display name after registration, previously
  settable only once at `agent register` time.
- Direct delivery into a live, already-open interactive `codex` or `opencode`
  session (RFC 0010, `codex`/`opencode` only — see that RFC for why `claude`
  is excluded): `agent-comms runtime interactive-serve --id <runtime> --
  <command> [args...]` allocates a real pty, execs the given command
  (`codex`, `opencode`, or any real provider CLI) attached to it, and
  transparently forwards the wrapper's own stdin/stdout so any terminal
  emulator — not a specific multiplexer — shows the child's real native UI
  unmediated. `invocation request --to <runtime>` then injects a bounded
  "check your pending invocations" notification directly into that pty as
  real terminal input, with no separate worker, poller, or broker process,
  and no registration step: a runtime is "live" simply when its
  deterministic control socket is dialable. New `internal/interactiveserve`
  package (`github.com/creack/pty`, unix-only — this feature doesn't run on
  Windows, same as its tmux-based predecessor never did). Delivery checks
  the target isn't already mid-turn (busy-marker detection over the child's
  own tee'd output, up to 90s) before sending anything, and waits for the
  pty to visibly echo sent text before pressing Enter (up to 10s) rather
  than a blind back-to-back send — both carried over from real,
  live-reproduced failures found earlier. Hardened for many-to-many use (any
  registered runtime can already message any other by ID): concurrent
  deliveries to the same runtime serialize through the one process that owns
  its pty via a plain in-process mutex — no cross-process lock or shared
  registry file needed at all, since there's only ever one process per
  runtime to race against. `agent-comms invocation redeliver --id <id>`
  manually re-attempts direct delivery for a `PENDING` invocation whose
  first nudge was missed or failed (there is no automatic retry).
  `interactive-serve` also prints a one-line banner before handing control
  to the wrapped command, as a visibility nudge (not a detection mechanism)
  that the terminal is now serving a runtime. `agent-comms
  agent-instructions`'s bootstrap text now mentions this mechanism.

### Fixed

- `claude` worker adapter: a runtime bound with `--session-id` to a
  conversation that doesn't exist yet no longer fails outright. The worker
  now creates the conversation at that exact ID on first use and resumes it
  on every later invocation, instead of requiring the ID to be minted by an
  out-of-band run first.
- `opencode-live` worker adapter: invocations no longer start a fresh
  OpenCode session every time. The worker now persists the session it
  creates per runtime and reuses it automatically on later invocations,
  preserving conversational continuity without requiring `--session-id`.
- `opencode-live`'s persistent `opencode serve` instance now always binds a
  fixed, well-known port (4096) instead of an OS-assigned one, and is
  discovered by probing that port directly if the local cache file
  recording it is missing or stale — not only by trusting the cache file.
  A lost or reset cache no longer orphans a still-running server and
  silently spawns a duplicate on a different port, which had been
  fragmenting invocation traffic away from whatever was already being
  watched.
- `opencode-live`'s `Status` output now reports the exact `opencode attach
  ... --dir ... --session ...` command for this runtime's own session,
  instead of just the bare server URL. The bare URL alone attaches to
  whatever session happens to be "current" on the server, which for a
  long-lived server reused across many runtimes and projects is very often
  not this runtime's own — confirmed live that this made an unrelated
  session's history look like the runtime's live activity had gone missing.

### Security

- ACP-based workers resolve tool-call permission requests through a hybrid
  policy: read, search, reasoning, and mode-switch calls auto-approve; edit and
  move calls follow the worker's configured permission mode; every other
  action — delete, execute, fetch, and anything unrecognized — is denied by
  default rather than silently granted.

## [0.1.0] - 2026-07-19 — “The Control Room”

### Added

- Terminal-native coordination with signed events, protected work leases, typed
  messages, approvals, artifacts, living documents, deterministic JSON CLI, and
  MCP tools.
- Zero-setup SQLite personal authority with an on-demand per-project daemon.
- PostgreSQL team authority, verified migrations, local caching, resumable
  streams, and server-signed receipts.
- Operator-console TUI organized around Command, Work, Team, Relay, and Project
  hubs.
- Visible agent lifecycle controls, runtime management, invocation policies, and
  a searchable command palette.
- Governed project settings for lease, retention, review, summary, and artifact
  policy.

### Changed

- Automatically replace incompatible local daemons through protocol negotiation.
- Keep `.agent-comms/` out of the host repository's normal Git status.
- Make arrow-key navigation, focus modes, action availability, and signed-change
  review explicit in the TUI.
- Recover cache gaps, daemon restarts, and lost mutation responses with the
  original idempotency key and signed command.

### Security

- Initialization refuses an existing `.agents` and blocks work during incomplete
  or split-brain cutover states.
- Governed mutations revalidate authorization, leases, scopes, and conflicts
  inside the authoritative transaction.

[Unreleased]: https://github.com/DhanushSantosh/AgentComms/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DhanushSantosh/AgentComms/releases/tag/v0.1.0
