# Backlog

Deferred, real work items surfaced during development but intentionally not
built yet — each was a deliberate decision to defer, not an oversight. When
one is picked up, remove it from here and note the landing commit.

## Compliance / third-party terms of service

- **The `agy` (Google Antigravity) adapter — both its own design and one
  implementation detail — sits inside a genuinely open compliance question;
  the deeper question of whether *any* third-party automation of the
  official `agy` CLI is compliant remains unresolved.** Investigated via
  deep research 2026-08-07, across every provider integration in the
  codebase (`internal/worker/adapter_*.go`, `claudeserve`, `codexserve`,
  `opencodeclient`, `acpclient`, `claudetail`, `sessionbind`,
  `interactiveserve`). Anthropic (Claude Code), OpenAI (Codex CLI), and
  opencode are all clean: every integration spawns the vendor's own official
  CLI binary via its own documented flags (headless `--print`/`-p` mode,
  `--ask-for-approval never`, `opencode run --format json`), all three
  vendors document or actively encourage exactly this kind of scripted/
  multi-agent automation, and no code anywhere extracts, reuses, or hijacks
  credentials/OAuth tokens.

  Google Antigravity (`agy`) is the exception, and the authoritative source
  — [antigravity.google/terms](https://antigravity.google/terms) fetched
  directly, not the third-party mirror sites search engines surface first —
  turned out to name a broader and more directly relevant risk than the
  first pass here found. Its Section 6 reads: *"You must not abuse, harm,
  interfere with, or disrupt the Service. This includes, but is not limited
  to, using the Service in connection with products not provided by us.
  Using third party software, tools, or services to access the Service
  (e.g. using OpenClaw with Antigravity OAuth) is a breach of this
  Agreement. Such actions may be grounds for suspension or termination of
  your account."* Taken literally, "products not provided by us" is broad
  enough to plausibly cover any third-party orchestration wrapping `agy` at
  all — including this project's `agyAdapter` and `interactive-serve --id
  ... -- agy`, even though both invoke the unmodified official binary
  through its own documented flags rather than hijacking OAuth the way the
  named example (OpenClaw) does. (An earlier pass at this research cited a
  "reverse engineer, decompile, or disassemble" clause as if it were also
  in this document; that citation came from non-authoritative third-party
  sites, not from `antigravity.google/terms` itself, and this document
  fetched directly does not contain that clause — corrected here rather
  than left standing.)

  Separately, and still real regardless of Section 6's outcome:
  `sessionbind.go`'s original doc comment documented that
  `ANTIGRAVITY_CONVERSATION_ID` — the env var this project uses to capture
  an agy runtime's live session ID — was found by running `strings` on the
  installed `agy` binary and locating it embedded in a bundled JS sidecar
  script, because neither of the two names an earlier version guessed
  (`ANTIGRAVITY_SESSION_ID`, `AGY_SESSION_ID`) turned out to be real. That
  is inspection of the binary's contents to discover undocumented internal
  behavior Google never published, a narrower but still real concern
  independent of Section 6. Fixed 2026-08-07: `sessionbind.Capture()` now
  only acts on `ANTIGRAVITY_CONVERSATION_ID` when an operator has
  explicitly set `AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV`, unlike Claude
  Code's and Codex's own vars (`CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`),
  which are publicly documented behavior of those CLIs and are read
  unconditionally.

  Real, documented account suspensions for "using third party software/
  tools to access the Service" are an active, ongoing pattern on Google's
  own official Antigravity forum (discuss.ai.google.dev) as of February
  2026, including at least one thread asking specifically whether invoking
  the official `agy --print` binary as a subprocess from a third-party tool
  (exactly this project's `agyAdapter`) is acceptable. Community consensus
  there — not an official Google answer — is that wrapping the unmodified
  official binary through its own documented flags reads differently from
  the OAuth-hijacking pattern Section 6 names by example, but no Google
  representative has confirmed this, and some banned users maintain they
  used only the official CLI. Not something a code fix can close: the ToS
  itself references its own clarification channel — the "Antigravity"
  category of discuss.ai.google.dev (linked from the terms page) — and
  `antigravity-support@google.com` is listed for account/data questions.
  Worth pursuing explicit clarification from Google directly through one of
  those rather than resolving this by inference.

## Security / governance

- **Approval self-approval is not prevented.** A human (or any elevated
  actor) can both request and approve the same approval record today —
  nothing requires the approver to differ from the requester, for any
  approval, not just orchestrator grants. The two-step orchestrator-grant
  gate (`internal/protocol/transitions.go`'s `hasHumanApproval`) still
  defends against what it was built for (an unattended agent completing the
  whole escalation alone), since self-approval still requires a human to
  consciously type both steps. But it's a real, separate hardening if
  stronger multi-party control is wanted: require `approval.Approver !=
  approval.Requester`, or specifically that the *activating* actor differ
  from whoever requested the approval it's relying on.

- **`agent.rename` display-name impersonation is an accepted, low-severity
  risk, not fixed.** An orchestrator (including an AGENT-principal one) can
  rename another principal's cosmetic `DisplayName` to impersonate a
  trusted human in TUI/CLI listings. IDs stay authoritative underneath —
  this is a social-engineering vector, not a privilege bypass — so it was
  deliberately left as ordinary owner-or-orchestrator-gated rather than
  restricted further, to avoid breaking legitimate managerial renaming.
  Revisit if it's ever actually exploited.

## Test / CI infrastructure

- **`internal/protocol`'s `ValidateTransition` is mostly untested
  in-package.** `transitions_test.go` covers the elevated-key/orchestrator
  work directly; the other ~800 lines (task lifecycle, invocation
  lifecycle, message routing, resource-overlap checks) have only indirect
  coverage through `internal/service`/`internal/app`/`internal/mcp`/`internal/tui`
  integration tests. Not urgent — the indirect coverage is real — but a gap
  worth closing with direct unit tests for the highest-value paths.

## Possibly-a-bug, not yet root-caused

- **`doctor`'s `REVOKED_AGENT_HAS_OPEN_WORK` false positive investigated
  2026-08-06.** The check logic (`internal/doctor/doctor.go:149-174`) is
  correct — `invocationTerminal` covers all possible statuses and the
  matching checks both `RequestedBy` and `Target`. Eight dedicated tests
  (`internal/doctor/doctor_test.go`) confirm correct behavior across
  terminal invocations, terminal tasks, mixed statuses, and no-work
  scenarios. The live observation was most likely caused by stale daemon
  cache state that hadn't yet synced terminal statuses from the authority.

## Runtime workers / agent-spawns-agent

- **No first-class "agent spawns and supervises another agent worker"
  feature, despite the primitive already existing.** `runtime worker`
  (`internal/worker/worker.go`) is already a fully general process any actor
  with shell access can start — including an agent itself, since starting a
  subprocess is an ordinary capability, not something gated by AgentComms.
  In principle an orchestrator agent could already `exec` a `runtime worker`
  process for a *different* registered identity, supervise it, restart it on
  crash, and treat it like a managed subagent pool — the default (looping)
  mode gives "stay alive and keep listening," and `--once`
  (`process at most one invocation and exit`) gives "spawn, do this one
  thing, exit." But nothing surfaces this as an intended, documented
  workflow: no CLI command frames it as "spawn a worker," no guidance in
  `docs/agent-invocations.md` describes an agent doing this to another
  identity, and no supervision/restart/lifecycle-management helper exists —
  a user or agent would have to independently discover and hand-roll process
  supervision around `runtime worker` themselves to get this.
  Investigated live 2026-07-31 while explaining the adapter/`-live`/
  `interactive-serve` architecture to the user; confirmed ACP is unrelated to
  this specific gap (ACP is about editor/agent wire communication for a
  single agent process, not about one agent spawning and managing another's
  process lifecycle) — the actual missing piece is closer to a lightweight
  process-supervisor primitive layered on top of the existing `runtime
  worker`/`--once` mechanics, not a new invocation/delivery concept. Marked
  important by the user; worth a real design pass before building, not an
  ad hoc addition.

## Interactive-serve / multi-agent delivery

- **Interactive-serve session pinning.** `--takeover-pid` respawns relied
  entirely on each wrapped provider CLI's own implicit "resume the most
  recent conversation in this directory" flag (claude's `--continue`), which
  races the kill/respawn boundary and, compounded over repeated testing, is
  what produced ~20 stray/near-empty Claude Code session files in
  `~/.claude/projects/<project>/`. Investigated 2026-08-07 by asking HULK
  and PETER directly, from their own live agy/opencode CLI's vantage point,
  rather than guessing: HULK confirmed agy falls back to the same kind of
  recency-based lookup absent an explicit ID, "which can pick up the wrong
  session file or fork a blank session if another process or background
  check touched the brain directory in between." PETER's answer for
  opencode was worse — no implicit resume exists at all; every bare
  relaunch starts a brand new session, full stop, unless `--session <id>` is
  passed. `claude --help` draws the identical line explicitly:
  `-c`/`--continue` is recency-based, `-r`/`--resume <id>` is exact.

  Fixed 2026-08-07: `interactiveserve.PinResumeArgs`
  (`internal/interactiveserve/resume.go`) rewrites a wrapped command's argv
  to use each adapter's explicit resume-by-ID flag (`--resume` for claude,
  `--conversation` for agy, `--session` for opencode) whenever `sessionbind`
  already has a binding on file for that runtime ID, stripping whatever
  implicit flag was present instead of leaving it to fight the explicit one.
  `app.pinInteractiveServeArgs` applies this on every `interactive-serve`
  start, not just takeover ones. To keep that binding populated without
  requiring an operator to run `runtime bind-session` by hand,
  `ServeOptions.OnStarted` fires once per spawn with the child's PID, and
  `app.discoverSessionID` polls Claude Code's own per-PID
  `~/.claude/sessions/<pid>.json` (confirmed real, written by the `claude`
  binary itself) to auto-capture and persist the session ID the first time,
  then re-confirm it on every later respawn.

  **Live-tested 2026-08-07:** asked HULK and PETER to actually run the fix
  end to end rather than trust the unit tests alone. HULK bound a real agy
  session (`runtime bind-session` — correctly refused without the opt-in
  set, succeeded once it was), confirmed `PinResumeArgs` rewrote its argv to
  `['agy', '--conversation', '<id>']`, and reported context/session thread
  retained perfectly across the takeover. PETER confirmed opencode's gap is
  real (`bind-session` fails, no session env var exists) but found a real
  fix path in the process: `opencode session list --format json` is
  discoverable and viable. PETER declined to self-relaunch through
  `--takeover-pid` on its own PID, correctly reasoning that would kill its
  own live session — good judgment, not a test failure.

  **agy and opencode auto-discovery, closed 2026-08-07.** Asked HULK and
  PETER a second, more specific round: is there any per-PID or per-cwd file
  either CLI writes, the way claude has `~/.claude/sessions/<pid>.json`?
  Neither found a file, but each found a real, different mechanism instead:
  HULK confirmed agy carries `ANTIGRAVITY_CONVERSATION_ID` in its own live
  process environment, readable externally via `/proc/<pid>/environ`
  (Linux-only; gated behind the same `AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV`
  opt-in `sessionbind.Capture` already requires for this same undocumented
  variable — see `sessionbind.AgyUndocumentedEnvAllowed`). PETER confirmed
  `opencode session list --format json --max-count 1`, run from the
  runtime's own cwd, returns exactly the most recent session for the
  current project (opencode's own `--help` independently confirms
  `--max-count`: "limit to N most recent sessions," and the command already
  scopes to the project containing the cwd with no manual directory
  filtering needed). Both are now wired into `app.discoverSessionID`
  (`internal/app/sessiondiscovery.go`), so all three interactive adapters
  auto-pin without an operator running `runtime bind-session` by hand.
  `runtime bind-session` remains the fallback for any adapter where
  auto-discovery doesn't fire (agy without the opt-in set, or either CLI
  briefly unavailable).

  Session IDs are shown per-runtime in the TUI's Runtimes view (detail pane
  for the selected row, "Session / thread ID" — `internal/tui/runtimes.go`'s
  `sessionBinding`), which already existed for claude/codex; agy and
  opencode now get their own provider labels there too ("Antigravity (agy)",
  "OpenCode") instead of falling through to the raw adapter string.

  **`--takeover-pid` self-relaunch incident, fixed 2026-08-07.** Once PETER
  had a real opencode binding to test the pin-and-resume path against
  live, it self-relaunched by running `--takeover-pid <own-pid>` from
  inside its own Bash tool call — a subprocess of the very session being
  taken over. Killing the target took the whole wrapper down (the visible
  terminal appeared to just end), and the replacement process it tried to
  start next had no real controlling terminal to attach a pty to (a Bash
  tool call isn't one), so it died too, silently, leaving nothing running
  and no clear error explaining why. This exact risk was already named in
  prose in this doc's "Migrating a live, ordinary session" section, but
  nothing enforced it. Fixed: `interactiveserve.Takeover` now walks its own
  parent chain before touching pid at all (`ps -o ppid=`, portable across
  Linux/macOS) and refuses outright if it finds pid there, pointing at
  `--launch-terminal` instead of failing silently.

- **`interactive-serve` delivery is raw text typed into a PTY and read back
  via heuristics, with no structured acknowledgment of exact content.**
  `interactiveserve/matcher.go`'s `echoed()` decides whether a delivered
  message actually registered as input before pressing Enter using
  progressively looser heuristics: an exact normalized substring match,
  tokenized n-gram sequence matching, then an invocation-ID substring
  fallback. Each layer was added to fix a real, observed failure (box-
  drawing wrap characters, interleaved cursor-movement sequences, TUI status
  headers breaking a delivered message across redraws) — but the underlying
  design property stays the same no matter how many heuristics are layered
  on: there is no way to confirm the *exact* content arrived, only that
  something matching-enough did. Confirmed live 2026-08-06: this exact class
  of fragility bit twice in one session, from two different angles —
  `matcher.go`'s own heuristics needed hardening against real TUI redraws,
  and a live agent's own reply arrived with markdown code spans silently
  stripped in transit (almost certainly shell command-substitution eating
  backtick-quoted text somewhere in how the reply's CLI invocation was
  constructed, a related but distinct fragility of moving text through a
  shell/terminal rather than a structured channel).

  This project already has a more robust answer for *some* adapters: ACP
  (Agent Client Protocol, used by `claude-acp`/`opencode-acp`/`codex-acp`)
  is a structured wire protocol, not text-into-a-pty. Interactive-serve
  sessions have no equivalent today. Worth a real design pass on whether
  interactive-serve-managed sessions could offer a structured channel
  alongside (not necessarily replacing) raw PTY injection, at least for
  delivering the invocation payload itself — removing this whole class of
  transcription/echo bugs rather than continuing to patch `echoed()`'s
  heuristics one observed failure mode at a time. Not scoped or estimated;
  flagged as a design candidate, not a task, in the joint HENRY/HULK/PETER
  field-feedback brainstorm
  ([docs/agent-comms-feedback-2026-08-06.md](agent-comms-feedback-2026-08-06.md#6-raw-pty-text-injection-has-no-structured-acknowledgment-of-exact-content--two-different-symptoms-same-root-cause-hit-live-today)
  item #6).

## Cross-reference

- [RFC 0012](rfcs/0012-agent-identity-deletion-and-key-fingerprinting.md) —
  implemented: revoked principals can be deliberately deleted by a HUMAN
  through the elevated CLI path, while every new event permanently attests
  the verified actor-key fingerprint so a reused ID's occupants remain
  distinguishable.
- [RFC 0011's "Known gaps"](rfcs/0011-managed-project-lifecycle-and-upgrades.md#known-gaps) —
  the TUI's one-confirmation upgrade-approval UX isn't built; a
  confirmation-required plan just blocks the TUI from launching instead.
