# Changelog

All notable user-facing changes are documented here. This project follows [Keep
a Changelog](https://keepachangelog.com/en/1.1.0/) and Semantic Versioning.

## [Unreleased]

### Added
- `interactive-serve` and `--takeover-pid` now work on Windows (10 version
  1809/October 2018 Update or later), closing a platform gap that
  previously errored outright. Built on ConPTY
  (`github.com/charmbracelet/x/conpty`) in place of `creack/pty`, a named
  pipe control socket (`github.com/Microsoft/go-winio`) in place of a unix
  domain socket, and `TerminateProcess` in place of POSIX signals for
  process lifecycle — see RFC 0014 for the full design and the live
  testing behind each of those choices. Closes #17.

### Fixed
- `install.ps1`'s cosign prerequisite check no longer rejects a real,
  correctly installed cosign: it now also recognizes
  `cosign-windows-amd64.exe`, cosign's actual upstream release/winget
  asset name, instead of only the bare `cosign` name that no documented
  Windows install method for cosign actually produces. Previously this
  blocked every Windows install that followed the documented steps
  exactly — see #16.

## [0.3.0] - 2026-08-08 — “Point and Click”

*A TUI you can drive with a mouse from a real-sized terminal, session-pinned
interactive delivery that survives a restart, a declarative path for adding
new CLI providers without touching Go, and a public marketing/docs site.*

**Added**
- Full native mouse support across the TUI — click, scroll, sidebar and
  hub-tab navigation, double-click-to-act, and Project settings (the one
  view that was missing it).
- The TUI now scales down to a real small terminal instead of requiring a
  desktop-sized minimum: dynamic responsive layout, full body viewport
  scrolling for non-table views, live PTY preview, a split-pane inspector,
  rich status indicators, and background toast notifications.
- `--takeover-pid` safely migrates a live interactive session into
  `interactive-serve`, and every migrated/resumed session now pins its
  exact provider session ID (auto-discovered for claude and opencode)
  instead of racing each provider CLI's own "most recent session" guess —
  closes a real class of stale/forked-session bugs.
- A declarative JSON adapter specification system: add a new CLI provider
  by dropping a spec file under `.agent-comms/adapters/`, no Go changes
  required.
- `runtime verify-adapter` checks an adapter's assumed CLI flags against
  the real installed binary's own `--help` output.
- `runtime.delete` (protocol, CLI, TUI), `task lock` (create+claim a task
  in one step), and human-readable table output by default for
  agent/runtime/invocation list commands.
- A public marketing site and docs site, a nightly beta build channel, and
  one-keypress Orchestrator-approval requests from the TUI.

**Fixed**
- Dozens of TUI layout and rendering bugs, most surfaced by making the TUI
  usable at real (non-desktop) terminal sizes: sidebar/row-list overflow,
  click-position drift, footer/keybinding wrapping, and two ANSI/wrap
  corruption bugs (a truncated escape sequence leaking onto screen; a
  bordered box that could render wider than the terminal and split its own
  border mid-line).
- `interactive-serve --takeover-pid` now refuses outright if the calling
  process is itself a descendant of the target PID, instead of silently
  killing its own controlling terminal — confirmed live before the fix.
- Stale interactive-serve sockets are cleaned up automatically on startup.

**Security**
- Removed the `agy` (Google Antigravity) worker adapter and all
  agy-specific integration code, over an unresolved third-party Terms of
  Service compliance question — never reached a tagged release, so nothing
  for existing installs to migrate away from. See `docs/backlog.md`'s
  "Compliance / third-party terms of service" section for the full
  research record.

Full technical detail is below and in [CHANGELOG.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CHANGELOG.md).

### Added

- **Mouse support and TUI scaling.** Every view now responds to the mouse
  (click, scroll, sidebar/hub-tab navigation, double-click-to-act), and the
  TUI no longer requires a desktop-sized terminal — layout, row-list
  windowing, and column widths are computed from the real terminal size,
  not a comfortable fixed guess. Also added: a live PTY preview pane
  (streams a runtime's control-socket snapshot), a split-pane inspector,
  rich per-row status indicators, background toast notifications, and a
  one-keypress Orchestrator-approval-request shortcut that chains
  request+approve+activate into a single confirm.
- **Session-pinned interactive delivery.** `interactive-serve
  --takeover-pid` safely migrates a live session in place. Resuming that
  session (or any interactive-serve restart) now pins the exact provider
  session/conversation ID via each adapter's own explicit resume-by-ID
  flag, instead of relying on the wrapped CLI's own implicit "most recent
  session" behavior — confirmed live to be racy across a kill/respawn
  boundary for every provider tested. The session ID is auto-discovered
  and persisted for claude (`~/.claude/sessions/<pid>.json`) and opencode
  (`opencode session list --format json`) without requiring an operator to
  run `runtime bind-session` by hand; it's shown per-runtime in the TUI's
  Runtimes view.
- **Declarative CLI adapters.** `internal/worker`'s adapter registry now
  loads `.agent-comms/adapters/*.json` specs at startup, letting a project
  add a new CLI provider (executable name, argument templates, permission
  modes, session env vars, busy markers) without writing Go code. Built-in
  adapters can't be silently overridden by a same-named declarative spec.
- `runtime verify-adapter` statically scans an adapter's source for
  `--flag`-shaped literals and diffs them against the real installed
  binary's own `--help` output, catching flag drift automatically instead
  of requiring a human to re-run that comparison by hand.
- `runtime.delete` removes a runtime record outright (protocol, CLI, and
  TUI), `task lock` creates and claims a minimal task in one governed step,
  and `agent`/`runtime`/`invocation list` commands print human-readable
  tables by default instead of requiring `--json` to be legible.
- Interactive runtimes now default to an `AUTOMATIC` invocation policy on
  first registration (only when the agent has no policy configured yet —
  existing configured policies are never touched), so a freshly registered
  interactive runtime is reachable without a separate manual policy step.
- `agent-comms runtime interactive-serve --launch-terminal` opens a fresh,
  dedicated terminal window running the same command instead of requiring
  it to be typed by hand into a new terminal.
- A public Next.js marketing site and Astro docs site (landing page,
  install/download page, releases and changelog pages, full CLI/MCP
  reference generated from the binary's own `--help` output), deployed to
  production on every `dev` push. A nightly, unstable build channel
  (`agentcomms-nightly` on GHCR, versioned `0.0.0-nightly`) for developers
  sanity-checking `dev`'s current state, separate from tagged Beta
  releases.

### Fixed

- A long tail of TUI correctness fixes found by making the interface work
  at real terminal sizes rather than a desktop-sized minimum, including:
  row lists silently overflowing their own pane with no way to scroll to
  the hidden rows; click coordinates drifting out of sync with rendered
  rows after layout changes; Project Settings' domain rail and footer
  keybindings wrapping or truncating; sidebar clicks breaking after the
  first one; and toast notifications overlapping the command rail.
- Two real ANSI/text-wrapping corruption bugs, both confirmed live and now
  regression-tested: `renderSidebar`'s compact fallback (short terminals)
  truncated the sidebar title *after* styling it, so the naive rune-based
  `truncate()` chopped through a color escape code and leaked raw,
  unterminated codes onto the screen instead of the title. Separately,
  `settingsControl`'s bordered box relied on lipgloss's own
  `Width()`-triggered implicit wrap for multi-line content, which
  confirmably renders several columns wider than requested for specific
  (width, text) pairs — the widened box then got clipped by the outer
  screen, splitting its own border mid-line. Fixed with a `wrapText()`
  helper that pre-wraps content correctly before any styling or box width
  is applied, rather than trusting the implicit wrap.
- `interactiveserve.Takeover` now walks its own parent chain before
  touching the target PID and refuses outright if it would be taking over
  its own ancestor — confirmed live that an agent self-relaunching through
  its own Bash tool call (a descendant of the session being taken over)
  took its own controlling terminal down with it, with no clear error
  explaining why.
- Stale interactive-serve sockets and lock files are now cleaned up
  automatically on startup via a dial-liveness check, instead of requiring
  manual cleanup after an unclean shutdown.
- `discoverClaudeSessionID` takes the Claude home directory as an explicit
  parameter instead of resolving it internally, fixing a real Windows CI
  failure (`os.UserHomeDir()` reads `%USERPROFILE%` there, not `$HOME`,
  which had made a test's `t.Setenv("HOME", ...)` silently a no-op).
  `buildinfo`'s dev build ID now hashes binary content directly instead of
  path+size+modtime, which could collide or drift without a real content
  change.
- `LoadProjectAdapters` errors now surface as warnings instead of being
  discarded silently, and are wired into CLI startup; declarative adapters
  can no longer silently override a built-in adapter of the same name; a
  blocking socket read was removed from the TUI's own `View()` method.
- An orchestrator-grant approval error message suggested a command that
  couldn't actually be run as written.
- `agent-comms init` no longer leaves Agent Comms' own local files
  unignored when run against a fresh repo with no existing `.gitignore`.
- Assorted CI/release fixes: missing Cosign `.bundle` uploads for CLI
  binaries, a Windows build break in `GracePeriod`, Lighthouse performance
  noise failing docs/landing CI non-deterministically, and a truncated
  action SHA in the landing job.

### Security

- **Removed the `agy` (Google Antigravity) worker adapter and every
  agy-specific integration point** — the built-in adapter itself, its
  session-ID auto-discovery and resume-pinning, and its undocumented
  environment-variable capture — over a genuinely unresolved question of
  whether *any* third-party automation of the official `agy` CLI complies
  with Antigravity's Terms of Service (Section 6's broad "products not
  provided by us" language, and an active, ongoing pattern of Google
  suspending accounts for exactly this on its own official forum, neither
  ever resolved by explicit clarification from Google). This never reached
  a tagged release, so there is nothing for an existing install to migrate
  away from. The underlying declarative adapter system can still fully
  express agy's real CLI shape for a project that wants to add it back at
  its own discretion — nothing agy-specific is required in this project's
  own source for that. Full research record and rationale in
  `docs/backlog.md`'s "Compliance / third-party terms of service" section.
- A new adapter contract test (`TestAdapterDefaultPermissionModeIsNotANoOp`)
  catches a real, previously-shipped class of bug: an adapter's `Validate`
  defaulting an unset `PermissionMode` to a value its own `Arguments()` has
  no case for, so the default silently behaves as if it were never set.

## [0.2.1] - 2026-08-02 — “The Missing Bundle”

*A hotfix restoring the Cosign-signed installer bundles that v0.2.0's CLI
release was missing, so `install.sh`/`install.ps1` work again.*

**Fixed**
- The published release was missing the Cosign `.bundle` file for every
  primary CLI binary — the documented installers fail closed without it.

Full technical detail is below and in [CHANGELOG.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CHANGELOG.md).

### Fixed

- The published release was missing the Cosign `.bundle` file for every
  primary CLI binary (`agent-comms-{os}-{arch}[.exe]`) — `install.sh` and
  `install.ps1` both require that exact file and fail closed without it,
  so a fresh install of v0.2.0's CLI never worked. The daemon and server
  binaries were unaffected (their release-asset wildcards happened to
  sweep the bundle in); Cosign was already signing the CLI bundles too,
  they simply were never attached to the release. This release re-cuts
  the same source with the release workflow's asset list corrected.

## [0.2.0] - 2026-07-31 — “Chain of Custody”

*A managed-lifecycle and security-hardening release: safer credential
handling, a distinct human-approval gate on orchestrator grants, and a
truthful interactive-delivery model.*

**Added**
- One-command project upgrades reconcile schema, binary, and daemon state
  automatically, with automatic backups and full post-upgrade verification
  (RFC 0011).
- **Breaking:** orchestrator grants now require a separate, explicitly
  human-approved decision — closes a self-escalation gap.
- A passphrase-protected elevated signing key now gates the most sensitive
  actions.
- Agent identities can be deleted and safely reused; every signed event
  carries its signer's key fingerprint (RFC 0012).
- Interactive delivery is now a real, auditable state machine — no connector
  can falsely report a message as delivered (RFC 0013).
- The TUI is a full control center: write actions everywhere, new panels,
  typo-proof pickers, redesigned views.

**Fixed**
- A duplicate `agent register` call could silently destroy an existing
  agent's credential with no recovery path — now rejected before any
  credential is generated.
- MCP's `agent_register` tool could register or squat an unrelated agent
  identity — now enforces its documented self-registration invariant.
- Assorted authorization and Postgres reliability fixes.

Full technical detail is below and in [CHANGELOG.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CHANGELOG.md).

### Added

- Managed project lifecycle and one-step upgrades (RFC 0011):
  `agent-comms project upgrade`, plus automatic reconciliation of every
  project recorded in the user's profile registry the first time a newly
  installed binary touches it — each reconciled independently so one
  broken project can never block the rest. Covers inspecting every
  component's version, disruptive-vs-automatic migration classification
  with a post-lock confirmation re-check, an ordered and resumable
  migration plan, a durable reconciliation journal (resumable after a hard
  crash), an automatic backup before mutating anything, transactional
  database migrations, atomic managed-file/manifest publication,
  disposable projection-cache rebuild, daemon restart, and full
  post-upgrade verification — storage, managed files, daemon health, and
  signed history, including confirming imported/attested history survives
  a cache rebuild, not only freshly signed events. Backed by a four-pass
  review that found and fixed 21 correctness/safety findings, plus real
  fault-injection coverage: a genuine `kill -9` mid-upgrade against a live
  project caught a backup-resume bug the exhaustive unit-test suite itself
  had missed.
- Orchestrator grants now require a distinct, separately-approved human
  decision, closing a gap the RFC 0011 fix pass surfaced live: granting
  the ORCHESTRATOR role previously required only that the signing
  credential be cryptographically HUMAN, which an unregistered agent
  operating over the ambient owner-fallback identity satisfies trivially —
  letting an autonomous agent self-grant orchestrator with no human ever
  actually deciding in the moment. `agent.activate` granting ORCHESTRATOR
  now additionally requires a pre-existing, separately-approved,
  HUMAN-tier approval record for that exact grant: an agent may apply
  (`approval request --tier HUMAN --action agent.activate:<id>`), but only
  a human can approve it, in a distinct later action.
- A second, passphrase-protected "elevated" signing key
  (`agent-comms agent elevate-key`), closing a gap the two-step
  orchestrator-grant approval above still left open: it's purely
  credential-based, so whoever holds an actor's stored primary key can
  complete both steps — including a locally-running agent with OS-level
  access to that credential. Live testing proved this directly: an agent
  completed the whole flow itself using the owner's stored key,
  indistinguishably from the owner acting directly. The elevated key
  (Argon2id + AES-256-GCM, one per HUMAN principal) is required instead of
  the everyday primary key for exactly those two transitions — granting
  ORCHESTRATOR and approving a HUMAN-tier approval — enforced identically
  by both authority backends. Decrypting it demands an interactive
  terminal passphrase prompt that refuses outright when stdin isn't a real
  TTY, confirmed live to be a real barrier against an agent's own
  non-interactive tool-call subprocess, not a theoretical one; the exact
  attack sequence that worked before now fails cleanly at both steps.
  Routine signing (tasks, messages, ordinary activations) is unaffected —
  scoped narrowly, not a blanket passphrase-on-every-write policy.
  `agent-comms init` now offers to set this up immediately after project
  creation (interactive mode, defaults to yes, degrades gracefully rather
  than failing the whole command if no terminal is attached to answer the
  prompt); `doctor` carries a persistent `NO_ELEVATED_KEY` warning if the
  offer is declined or can't be answered, so the gap can never stay
  silently invisible.
- `agent-comms agent delete` and per-event actor-key fingerprinting
  (RFC 0012): permanently releases a `REVOKED` principal's ID for reuse by
  an unrelated future principal — previously the only workaround was a
  numbered-suffix ID. Requires a HUMAN principal unconditionally (stricter
  than revoke, which only requires HUMAN for orchestrator/human targets)
  and the elevated key once one is registered; CLI-only, no MCP tool,
  matching `agent elevate-key`'s precedent. Every newly committed event now
  attests the exact verified signing key's fingerprint, so a reused ID's
  old and new occupants stay cryptographically distinguishable in history
  forever, not just by ID string; `agent-comms history`/`search` gained
  `--key-fingerprint` and `--actor` filters to isolate them directly.
  Adding this field to the event hash risked invalidating every
  already-committed event's hash — it's `omitempty` on both the event and
  its canonical hash struct, so every pre-existing event without a
  fingerprint marshals and verifies exactly as before, independently
  confirmed against the pre-change code, not only by trusting the new
  test.
- Truthful, isolated interactive delivery (RFC 0013): separates four facts
  that invocation delivery previously blurred together — that a request
  was durably committed, that a transport actually attempted delivery,
  that the target acknowledged it, and that the target completed it.
  Runtimes now declare a `kind` (`WORKER` or `INTERACTIVE`); invocations
  declare a consumer mode (`INTERACTIVE_ONLY`, `WORKER_ONLY`, or `EITHER`,
  the race-permitted compatibility default). Delivery is now a real,
  auditable state machine — request, then resolve one policy-compatible
  runtime, then reserve a delivery attempt, then execute the transport,
  then commit real evidence or a failure — instead of a side effect of the
  request itself; `MANUAL`/`MCP`/`QUEUE` connectors can no longer produce a
  false delivery-success record. Interactive socket paths moved to an
  owner-only, UID-scoped directory that deliberately ignores `$TMPDIR`, so
  a desktop-launched provider and the daemon can never silently disagree
  about which control socket to use. Each installation now has one
  random, never-hostname-derived host ID, so PTY delivery is provably
  local-host-only — a foreign-host interactive runtime stays visible but
  unreachable rather than silently attempted. `doctor` gained findings for
  unresolved connector references, malformed interactive runtimes,
  foreign-host sessions, ambiguous automatic routing, dead sockets, and
  stale delivery attempts. Storage: event/model schema `2.1.0`, PostgreSQL
  schema `3`, projection cache `3`, local daemon protocol `4` — upgraded
  automatically through the managed project-lifecycle upgrade path above.
- The TUI is closer to a full control center now, not a thin dashboard,
  across five phases:
  - **Documents** and **Contracts & decisions** gained real write actions
    (create/update/supersede) — previously read-only, even though the
    equivalent CLI commands always existed.
  - New **Artifacts**, **Drafts**, and **Environment** panels, plus
    `agent.delete`/`agent.rename` row actions on Agents — CLI surfaces
    that previously had zero TUI presence at all.
  - Every enum-shaped form field (Role, Kind, Connector, Priority,
    Consumer, Tier, Principal type, invocation Mode) is now a
    left/right-cycling picker instead of free text a typo could silently
    corrupt.
  - **Runtimes** redesigned from an 11-column table (unreadable outside a
    very wide terminal) into a compact table plus a detail pane for the
    selected row; **Invocations**' delivery evidence redesigned from a raw
    timestamped log into RFC 0013's actual five-stage delivery pipeline
    shown as status chips with relative timestamps.
  - **Audit & health** now surfaces `doctor`'s findings directly
    (previously it showed only chain integrity and lifecycle status),
    computed lazily — only when the panel is actually opened — since it
    dials every online interactive runtime's local PTY socket, and that
    can take real time against a genuinely busy live session.
- `agent revoke` / `agent_revoke` (CLI and MCP) / a `revoke` row action in
  the TUI: a terminal, irreversible removal option for agent principals,
  mirroring the existing `runtime.revoke` pattern rather than inventing
  "delete" as a second word for the same concept — nothing is ever erased
  from the hash-chained event log, only marked with a permanent status.
  Once revoked, a principal can never act again, be reactivated, renamed,
  or suspended. The project Owner can never be revoked, by anyone,
  including itself. Revoking an Orchestrator-role principal or any HUMAN
  principal requires the same human-only check that granting the
  Orchestrator role already requires, unless self-revoking. Revocation
  cascades to the agent's own runtimes (also marked revoked) but never
  auto-cancels its invocations or auto-reassigns its tasks — `doctor` now
  flags any such orphaned work separately.
- Per-project, per-host actor resolution via `AGENT_COMMS_HOST_LABEL`,
  closing the gap left by global MCP configs that hardcode one fixed
  `--actor` per host identically across every project: a host now tags its
  global config once with a stable label (e.g. `claude`, `codex`,
  `opencode`) instead of a fixed actor, self-registers under a
  project-chosen ID the first time it connects to a given project (an
  owner-fallback MCP connection — one with no dedicated identity yet in
  this project — is now permitted to `agent_register` under any id, not
  only its own bound actor; `Register()` always mints a fresh, self-signed
  keypair regardless of caller, so this bootstraps a new identity rather
  than reopening the impersonation hole the self-registration invariant was
  added to close), and every later connection from that same host, in that
  same project, resolves straight to that same actor automatically —
  `identity.Profile` now records the registering host alongside the actor
  and project, and actor resolution (`internal/app/app.go`) checks for a
  matching (project, host) profile before falling through to the existing
  active-profile/owner fallback. This makes the real, chosen actor ID (not
  the purely cosmetic, non-unique `display_name`) usable directly as the
  `target` in `invocation_request` for agent-to-agent addressing, with no
  translation layer and no per-project config edits.
- `agent_register` and `agent_activate` MCP tools (`internal/mcp/server.go`),
  closing the one gap that stood between "MCP is the general, adapter-free
  way for any agent to participate" and it actually being true: without
  them, a brand-new agent identity couldn't do anything over MCP until
  someone ran `agent-comms agent register` via the CLI first, since every
  other tool requires the connection's bound actor to already have signing
  credentials. `agent_register` mirrors `agent-comms agent register`
  exactly (self-registration needs no elevated authorization); `agent_activate`
  mirrors `agent-comms agent activate` and stays exactly as gated
  (owner/orchestrator-only) — this doesn't loosen that governance boundary,
  it just exposes the existing rule over MCP instead of requiring a CLI
  shell-out for that one step. Verified live: a real Claude Code session
  self-registered via its own MCP connection with zero prior CLI setup.
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
- Direct delivery into a live, already-open interactive `codex`, `opencode`,
  or `claude` session (RFC 0010): `agent-comms runtime interactive-serve
  --id <runtime> -- <command> [args...]` allocates a real pty, execs the
  given command (`codex`, `opencode`, `claude`, or any real provider CLI)
  attached to it, and transparently forwards the wrapper's own
  stdin/stdout so any terminal emulator — not a specific multiplexer —
  shows the child's real native UI unmediated. `invocation request --to
  <runtime>` then injects a bounded "check your pending invocations"
  notification directly into that pty as real terminal input, with no
  separate worker, poller, or broker process, and no registration step: a
  runtime is "live" simply when its deterministic control socket is
  dialable. New `internal/interactiveserve` package (`github.com/creack/pty`,
  unix-only — this feature doesn't run on Windows, same as its tmux-based
  predecessor never did). Delivery checks the target isn't already
  mid-turn (busy-marker detection over the child's own tee'd output, up to
  90s) before sending anything, and waits for the pty to visibly echo sent
  text before pressing Enter (up to 10s) rather than a blind back-to-back
  send — both carried over from real, live-reproduced failures found
  earlier. Hardened for many-to-many use (any registered runtime can
  already message any other by ID): concurrent deliveries to the same
  runtime serialize through the one process that owns its pty via a plain
  in-process mutex — no cross-process lock or shared registry file needed
  at all, since there's only ever one process per runtime to race against.
  `agent-comms invocation redeliver --id <id>` manually re-attempts direct
  delivery for a `PENDING` invocation whose first nudge was missed or
  failed (there is no automatic retry). `interactive-serve` also prints a
  one-line banner before handing control to the wrapped command, as a
  visibility nudge (not a detection mechanism) that the terminal is now
  serving a runtime. `agent-comms agent-instructions`'s bootstrap text now
  mentions this mechanism. `--claude-allow-agent-comms` scopes
  `--allowedTools` to this Agent Comms executable (both its resolved
  absolute path and its bare basename — confirmed live that Claude invokes
  it via the bare name, so an absolute-path-only rule silently matches
  nothing) so a `claude` runtime can drive `agent-comms invocation *`
  unattended. Confirmed live across three simultaneous runtimes (`codex`,
  `opencode`, `claude`) that Claude's willingness to act on a delivered
  invocation is risk-proportionate judgment, not a categorical refusal: a
  low-stakes request in a project that checks out as legitimate gets
  completed; a bare claim of authorization for autonomous action, tested
  three separate ways, is correctly refused as indistinguishable from a
  prompt injection — see RFC 0010 for the full evidence on both.

### Fixed

- Several authorization gaps in `internal/protocol/transitions.go`, all
  found the same way the orchestrator-grant gap above was: `agent.suspend`
  had no protection against targeting the OWNER (a suspended principal
  fails every subsequent action, including reactivating itself — a full,
  potentially unrecoverable lockout); `agent.revoke` of an ORCHESTRATOR or
  HUMAN principal never got the elevated-key requirement extended to it;
  `agent.rotate-key` targeting a different principal had no consent check
  at all (a full identity-hijack primitive, removed outright — a key can
  only ever be rotated for the caller's own actor now);
  `project.settings.update` could be changed by any orchestrator-role
  AGENT principal, including disabling the project-wide review requirement
  (now requires a HUMAN principal); `env.set`/`env.delete` had no role
  gate whatsoever, not even for an OBSERVER principal (now requires
  ordinary owner-or-orchestrator elevation).
- `agent.rename` was completely broken in the Postgres/service authority
  backend — missing from its `decodePayload` switch since the commit that
  introduced it, silently masked because personal mode's decoder is
  generic. A new regression test cross-checks every registered event type
  against every backend-specific decoder so this class of bug can't ship
  silently again.
- `Service.PassphrasePrompt` could hang rather than fail cleanly if
  invoked from the TUI (which already owns raw stdin for its own
  key-event loop) or an MCP host that allocates a pty for the subprocess.
  Both now refuse the prompt outright and unconditionally instead of
  attempting a terminal read that could race another consumer of the same
  file descriptor.
- `runtime interactive-serve` now propagates the actor resolved for the
  wrapper (`--actor`/profile/host-label) into the wrapped provider's own
  environment as `AGENT_COMMS_ACTOR`. Previously the wrapped
  `claude`/`codex`/`opencode` process just inherited whatever the
  wrapper's shell happened to have set, so every `agent-comms` call it
  made on its own resolved its actor however ambient fallback landed,
  independent of which identity was actually resolved for the session.
- The TUI's Inbox compared the viewing actor against the literal string
  `"owner"` instead of the project's real owner ID, so the intended "the
  owner sees every message" behavior silently never fired on any real
  project — every test happened to use `"owner"` as the literal owner ID,
  which is exactly what let this hide.
- `Service.State()` made exactly one remote call to the local daemon with
  no retry and no recovery, unlike the write path. A single transient
  "local daemon is unavailable" blip — e.g. its socket file briefly
  missing — silently killed long-running `runtime interactive-serve`
  sessions on the very next 15-second heartbeat tick, with nothing ever
  getting a chance to reconnect. Reads now share the same
  backoff-and-recover logic writes already had. Confirmed live: deleting a
  real daemon's socket file mid-session no longer ends the wrapped
  session.
- `agent-comms profile list`/`use` now work correctly outside an
  initialized project, instead of requiring one to already exist.
- `agent-comms init`'s elevated-key setup now starts the local daemon
  first if needed — previously it always failed with "daemon unavailable"
  even when the passphrase was answered correctly, since `init` never goes
  through the same startup path every other command does.
- The MCP `initialize` response now reports the same release-injected
  version as `agent-comms version` instead of a stale independent
  `0.2.0-preview.2` literal.
- MCP setup documentation now uses per-project, per-host identity resolution
  for Claude, Codex, and OpenCode instead of recommending one fixed global
  `--actor`; RFC 0009 now records the shipped `codex-live` implementation.
- `agent-comms mcp`: every tool with zero required arguments (`status`,
  `history`, `invocation_next`, `verify`) marshaled `"required":null` in its
  JSON Schema instead of `"required":[]` — Go's zero value for a variadic
  parameter called with no arguments is `nil`, not an empty slice. `null`
  is invalid per JSON Schema wherever `required` is present. Confirmed
  live: Claude Code's real MCP client fetched `tools/list` successfully
  every time but silently rejected the entire response ("tools fetch
  failed", no further detail) because of it — the server looked connected
  and broken at once, with no error message pointing at the cause.
- `agent-comms mcp`: notifications (any `notifications/*` method, per MCP
  convention — matches the JSON-RPC 2.0 rule that notifications never
  receive a response) no longer get a response line. `notifications/initialized`
  previously did, which is spec non-compliance a strict client's response
  correlation could choke on, even though the two live-tested clients this
  session (Claude Code, Codex) both tolerated it.
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

- `Service.Register` (`agent-comms agent register`, and the `agent_register`
  MCP tool built on it) no longer lets a second registration for an
  already-registered agent ID silently destroy that agent's real
  credential. Confirmed live, not theoretical: a duplicate registration
  attempt (plausibly an MCP client retrying a call that had already
  succeeded) generated a brand-new keypair, overwrote the existing valid
  credential with it, and appended a new ledger event replacing the
  original public key — permanently bricking that agent's ability to sign
  anything, with no recovery path, since the destroyed private key was
  never stored anywhere else. Root cause: `ValidateTransition`'s
  "principal already exists" check was already enforced server-side for
  remote/personal/service-mode registrations, while the retired filesystem
  engine appended unconditionally, skipping it entirely, and generated +
  persisted the fresh credential before any validation ran at all. All
  supported authority paths now validate duplicate registration before ever
  generating a credential, and the credential/profile are only persisted
  after a confirmed-successful append — never speculatively beforehand.
- `agent-comms mcp`'s `agent_register` tool now enforces the self-registration
  invariant its own docstring already promised: `id` must equal the MCP
  connection's own bound actor, and `principal_type` is validated against
  `HUMAN`/`AGENT` before use. Caught by automated security review before
  release — the first implementation let one MCP-bound actor register (or
  squat) an arbitrary, unrelated agent identity, breaking the per-actor
  scoping an MCP connection is supposed to guarantee, and cast an
  unvalidated string straight into `model.PrincipalType`.
- ACP-based workers resolve tool-call permission requests through a hybrid
  policy: read, search, reasoning, and mode-switch calls auto-approve; edit and
  move calls follow the worker's configured permission mode; every other
  action — delete, execute, fetch, and anything unrecognized — is denied by
  default rather than silently granted.

## [0.1.0] - 2026-07-19 — “The Control Room”

First tagged release: terminal-native, signed coordination between humans and
agents — typed messages, protected work leases, approvals, artifacts, living
documents — backed by either a zero-setup local SQLite authority or a shared
PostgreSQL team authority, and operated through a full console TUI or a
deterministic JSON CLI/MCP surface.

### Added

- Terminal-native coordination with signed events, protected work leases, typed
  messages, approvals, artifacts, living documents, deterministic JSON CLI, and
  MCP tools.
- Zero-setup SQLite personal authority with an on-demand per-project daemon.
- PostgreSQL team authority, local caching, resumable streams, and
  server-signed receipts.
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

- Initialization refuses an existing `.agents` and publishes a complete runtime
  atomically.
- Governed mutations revalidate authorization, leases, scopes, and conflicts
  inside the authoritative transaction.

[Unreleased]: https://github.com/DhanushSantosh/AgentComms/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/DhanushSantosh/AgentComms/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/DhanushSantosh/AgentComms/releases/tag/v0.1.0
