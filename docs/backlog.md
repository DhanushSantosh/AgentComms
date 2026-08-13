# Backlog

Deferred, real work items surfaced during development but intentionally not
built yet — each was a deliberate decision to defer, not an oversight. When
one is picked up, remove it from here and note the landing commit.

## Compliance / third-party terms of service

- **RESOLVED 2026-08-08: `agy` (Google Antigravity) support removed from the
  project entirely**, rather than pursued further by inference or by
  waiting on clarification from Google (the "worth pursuing" note at the
  end of this entry was never acted on). Removed: the built-in worker
  adapter (`internal/worker/adapter_agy.go`, deleted), its `builtInAdapters`
  entry, its declarative-Prompt() special case, `sessionbind`'s
  `ANTIGRAVITY_CONVERSATION_ID` capture (`AgyUndocumentedEnvAllowed`,
  `AGENT_COMMS_ALLOW_UNDOCUMENTED_AGY_ENV`, all deleted, not just gated),
  `interactiveserve.PinResumeArgs`'s agy resume-flag mapping, and
  `app.discoverSessionID`'s `/proc/<pid>/environ` auto-discovery for it. The
  generic `interactive-serve -- <any CLI>` wrapping capability itself is
  untouched and unaffected — nothing agy-specific was needed for that to
  work, since it wraps whatever executable it's given regardless.
  Deliberately kept technically reachable, not walled off: the declarative
  JSON adapter system (`internal/worker/declarative.go`) can fully replicate
  agy's real argument shape (confirmed by
  `TestDeclarativeAdapterAgyEquivalence`, which predates this removal and
  still passes), and "agy" is no longer a blocked built-in name
  (`TestRegisterDeclarativeAdapterAcceptsAgy`) -- a project that wants this
  can add its own `.agent-comms/adapters/agy.json` spec, entirely at its own
  discretion and risk, with zero agy-specific code shipped by this project
  itself. The rest of this entry is kept verbatim below as the research
  record that led here.

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

- **SHIPPED 2026-08-13: self-service role switching, custom role labels,
  and removal of the AGENT/OBSERVER roles ([RFC 0018](rfcs/0018-self-service-role-switching-and-custom-roles.md)).**
  `model.Role` was a closed four-value enum (`OWNER`, `ORCHESTRATOR`,
  `AGENT`, `OBSERVER`) with two redundant/under-used members: `AGENT`
  duplicated `PrincipalType`'s own `AGENT` value for no added meaning, and
  `OBSERVER` was a narrow, two-transition read-only tier, not a coherent
  permission model. Both are removed; `Role` is now `OWNER` |
  `ORCHESTRATOR` | any freeform label a principal chooses for itself
  (`Frontend-Architect`, `Tester`, ...), with zero permission effect for
  anything other than the two reserved values. New self-service transition
  `agent.switch-role`/`agent_switch_role` lets any active principal relabel
  its own role at any time, no owner/orchestrator elevation required --
  strictly bounded by construction: self-only (`id == actor`), `OWNER`
  never reachable as either target or source, and switching to
  `ORCHESTRATOR` keeps the exact same two-step human-approval-plus-
  elevated-key gate `agent.activate` already enforces for the same grant
  (kept, not weakened to "passphrase alone," per explicit direction).
  Never touches `Capabilities`/`Scopes` -- only `agent.activate` (unchanged,
  still owner/orchestrator-gated) can grant those. Also closes a related,
  previously-unintended gap while touching this code: the general
  `agent.activate` path used to silently accept `Role: OWNER` for any
  target; `OWNER` is now reachable only through the one special bootstrap
  event at project creation, exactly as it always should have been.
  `agent activate --role` lost its `AGENT` default and is now a required
  flag, since there's no longer a generic role to fall back to.

- **RESOLVED 2026-08-12: local default-actor resolution could silently
  misattribute real, signed governed actions across concurrent
  sessions.** Reported live: a human owner switching actors in the TUI kept
  hitting "owner or orchestrator role required" with no obvious cause.
  Root-caused (not guessed at): `identity.UserConfig.ActiveProfile` was a
  single string in one JSON file shared by *every* process on the machine
  — a different agent running `agent-comms profile use --name <itself>`
  for its own ordinary convenience ("default my own commands to me")
  silently redirected every other concurrent session's default actor too,
  including the human's own. Worse than a display bug: every registered
  actor's real private key lives in the same shared OS keyring
  (`KeyringStore`, keyed only by `(projectID, actor)`), so a bare command
  resolving to the wrong actor got **cryptographically signed as that
  actor** and durably recorded that way — real misattribution of ordinary
  governed work (task claims, messages, plain activations), not just a
  wrong name on screen. The four elevated-key-gated transitions (granting
  Orchestrator, HUMAN-tier approval, revoking an Orchestrator/HUMAN,
  deleting a revoked principal) were never at risk — each independently
  verifies the signer is a genuine `HUMAN`-type principal server-side, so
  this couldn't be used to escalate privilege or forge a human approval.
  Fixed per RFC 0016: replaced the single machine-wide field with
  `ActiveProfileBySession`, keyed by the provider session ID
  `sessionbind.Capture` already reliably detects (`CLAUDE_CODE_SESSION_ID`/
  `CODEX_THREAD_ID`, harness-injected into every subprocess, not something
  an agent has to remember to export). The legacy field remains the
  fallback only when no recognized session is present at all (a genuine
  plain terminal) — and critically, a *real, recognized* session with no
  profile of its own set yet resolves to the safe project-owner default,
  never falls through to the shared legacy field. Verified live, not just
  in unit tests: two different simulated sessions setting different active
  profiles against the same on-disk config resolved fully independently,
  and a plain-terminal resolution was unaffected by either — reproducing
  the originally reported scenario directly and confirming it no longer
  occurs.

- **RESOLVED 2026-08-13: RFC 0016's legacy fallback was still silently
  exploitable for any session-less invocation path.** Reported live, on a
  real project: one agent's `runtime register` call got cryptographically
  signed as a completely different, unrelated agent. Root cause was RFC
  0016's own accepted fallback boundary, hit for real: the calling agent's
  session (an opencode-based agent) exposes none of the provider session IDs
  `identity.DetectProviderSessionID`/`sessionbind.Capture` know how to read
  (`CLAUDE_CODE_SESSION_ID`/`CODEX_THREAD_ID` are opencode-specific gaps, not
  a detection bug), so resolution fell all the way through to the same
  shared, machine-wide `ActiveProfile` field RFC 0016 already knew was
  unsafe to trust blindly — which another concurrent agent's session had
  pointed at itself moments earlier for its own ordinary convenience. No
  automatic fix is possible here: opencode's process model gives no ambient
  signal a library can detect without spawning a subprocess on every
  invocation, for every user, to check. Fixed per
  [RFC 0017](rfcs/0017-refuse-ambiguous-legacy-actor-for-writes.md): rather
  than trying to detect the session, `Service.ExecuteWithPassphrase` now
  refuses outright to sign any governed write resolved via the legacy
  fallback whenever the project has 2+ locally-registered identities to
  silently choose between (`identity.UserConfig.ProfileCountForProject`),
  forcing an explicit `--actor`/`AGENT_COMMS_ACTOR` instead of ever guessing.
  Projects with 0-1 local identities are unaffected — there's nothing
  ambiguous to guess between. A deliberate, human-picked switch through the
  TUI's actor-switch form (which shows each candidate's real role, not a
  blind list) explicitly clears the flag — that resolution was never
  ambiguous regardless of how the TUI's own actor was set at startup.
  Documented for session-less agents (opencode, scripts, cron jobs) in
  [agent-onboarding.md](agent-onboarding.md#if-your-session-has-no-ambient-session-id-opencode-scripts-cron-jobs).
  Verified end-to-end, not just in isolation: a project with two
  locally-registered identities and no session ID present refuses a bare
  write and points at the fix; the identical call succeeds immediately with
  `--actor` supplied explicitly.

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

- **`TestInvocationDeliveryFailureDoesNotTerminateObligation` is flaky on
  loaded/slow windows-latest runners, not fixed.** Observed live on PR #25's
  CI (2026-08-13): failed once with `"an unexpired delivery attempt already
  exists for this runtime"` on a run where every package's tests ran visibly
  slower than normal (`internal/mcp` 72s vs the usual ~24s,
  `internal/projectlifecycle` 62s vs ~20s) -- a re-run of the identical
  commit passed cleanly, as did a separate parallel windows-latest run on
  the same SHA. Root cause is a known race class already documented inline
  by the neighboring test (`TestFailedRedeliveryPreservesEarlierSuccessfulEvidence`'s
  comment): `setupWithLocalConnector` runs a real background daemon whose
  delivery coordinator retries dispatch for PENDING invocations every
  500ms, and on a slow enough runner that automatic retry can win the race
  against this test's own explicit `invocation.delivery-attempt` call,
  colliding on the same runtime's outstanding attempt. Unrelated to RFC
  0017's actor-resolution guard (this test drives a `Service` instance
  directly with an explicit `"owner"` actor, never through CLI actor
  resolution). Worth tightening the 500ms retry/test timing assumption if
  it recurs; not done here since a single confirmed flake isn't enough to
  diagnose the right fix.

- **RESOLVED 2026-08-12: `internal/protocol`'s `ValidateTransition` direct
  coverage gap closed, and a per-package coverage floor now guards against
  it recurring anywhere else.** Prompted by a codebase-hardness audit that
  flagged this entry specifically. `transitions_lifecycle_test.go` adds
  direct unit tests for task lifecycle, message routing, runtime lifecycle,
  approval/decision/document, and invocation policy/delivery -- raising
  `internal/protocol` from 32.2% to 73.5% coverage with zero functions left
  at 0%. Separately, `scripts/coverage-floor.sh` (wired into `ci.yml`'s
  `test (ubuntu-latest)` job) now fails CI if any package's coverage drops
  below a floor ratcheted from real measured numbers, so a package quietly
  losing its tests -- the exact shape of this original gap -- gets caught
  going forward instead of relying on someone noticing. `internal/authority`
  gets its own floor in the `postgres-integration` job instead, checked
  against real Postgres-backed coverage (49.5% measured, 40% floor) rather
  than the unit-only run where most of its paths are `t.Skip`-guarded.
  Original entry kept below for context.

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

## TUI

- **Actor-switch/elevated-key trio of bugs, fixed 2026-08-12.** Reported
  live: a user granting ORCHESTRATOR to an agent switched actors in the TUI
  to what they believed was their owner identity, then still hit "owner or
  orchestrator role required" with no way to tell why. Root-caused via a
  live investigation (not guessed at) into three separate, real problems,
  all fixed together:
  1. **`refresh()` unconditionally overwrote `m.notice`.** Every action
     handler that set an informative result notice ("Switched to X",
     "Applied agent.activate to Y", ...) then called `refresh()` to reload
     lists/findings/drafts — which stomped that notice with the generic
     "State refreshed at HH:MM:SS" one line later, at 12 of 14 call sites.
     A user switching actors (or completing any other action) never
     actually saw confirmation of what happened. Fixed by splitting
     `refresh()` (bare "r" keybinding only, still reports "state
     refreshed") from a new `refreshState()` (everything else, leaves
     `m.notice` alone) — `TestActorSwitchNoticeSurvivesTheFollowingRefresh`
     is the regression test, confirmed to fail without the fix.
  2. **The actor-switch picker showed bare IDs with no role/status**,
     pulled from every locally-saved credential for the project regardless
     of standing — trivial to pick a non-owner/non-orchestrator identity by
     mistake with nothing warning you. Fixed: each candidate is now labeled
     with its actual current role (`TestActorSwitchFormShowsRoleNextToEachCandidate`).
  3. **The status rail showed role only, never actor ID** ("authority
     owner" with no name attached) — no persistent way to confirm *who*
     you're acting as without reopening the switch form. Fixed: the actor
     ID is now shown alongside the role
     (`TestCommandRailShowsActiveActorID`).
  Separately, `docs/governance.md`, `docs/agent-onboarding.md`, and —
  most consequentially, since it's the deployed public doc site —
  `docs/site/start/tui.md` all incorrectly claimed the TUI refuses every
  elevated-key-signed action and the CLI is required; the code has
  actually supported completing these directly via a masked passphrase
  form field for a while. This false claim is almost certainly what led an
  agent operating this project to hand a user CLI-only instructions that
  then failed on an unrelated actor-resolution mismatch — both problems
  compounding into one confusing dead end. Docs corrected; `internal/
  protocol/transitions.go`'s "owner or orchestrator role required" error
  now also names the resolved actor and its actual role
  (`TestElevationRejectionNamesTheResolvedActorAndItsRole`), confirmed live
  against a real scratch project, so this exact failure mode is
  self-diagnosing going forward instead of a silent puzzle.

- **Sidebar title corruption on a short terminal, fixed 2026-08-08.** Asked
  to make the TUI dynamically scalable rather than requiring a minimum
  terminal size, found the real bug behind it: `renderSidebar`'s compact
  fallback layout (the branch that drops spacer lines and keybinding hints
  once the full sidebar can't fit, `len(rows)+2 > h` — kicks in around
  height 23-25 depending on hub count) truncated the sidebar's title
  *after* it had already been styled. `truncate()` slices runes with no
  notion of ANSI escape codes, so it chopped straight through the middle of
  a 24-bit color sequence as readily as through the visible text, leaking a
  raw, unterminated code like `[1;38;2;0;25…` onto the screen in place of
  "● AGENT COMMS" — exactly what made a shorter terminal look broken rather
  than gracefully smaller. Fixed by truncating the plain, unstyled title
  text first and applying the style afterward, the same order every other
  `truncate()` call site in this package already used — this was the one
  place that truncated an already-`.Render()`'d string instead. Confirmed
  live down to a 24x8 terminal, and against a real revert of the fix that
  the new regression test (`TestSidebarTitleSurvivesCompactFallback`)
  actually fails without it. (Correction to what was written here at the
  time: this was called "the one genuine corruption bug, not a sign of a
  deeper structural gap" — a second, separate bug in the same sweep turned
  up right after, below.)

- **Overview page-level scroll couldn't reach its own trailing content,
  fixed 2026-08-08.** Reported directly: "I scrolled down as far as I could
  and still can't see the end of the page." Root cause in `renderBody`'s
  scroll window for every non-table view (Overview, Blockers, Audit &
  health, Activity, Archive search): `availH := max(22, contentH-4)`
  floored the available height at a comfortable desktop constant instead of
  the real terminal size — unlike `visibleRowCount`'s identically-shaped
  `max(0, h-4)` for row-list views, which got this right. On any terminal
  short enough that `contentH-4` fell under 22, the page believed it had
  more room than it actually did, so even scrolling all the way to the
  reported `maxScroll` still rendered past the real screen edge: content
  existed but its own trailing lines (Overview's `"[g] agents · ..."`
  key-hint row) were permanently unreachable, confirmed live at 80x16.
  Fixed by flooring at 0 like every other survival floor in this file, plus
  reserving exactly one line of the budget for the scroll indicator itself
  (only when it's actually shown) instead of leaving that unaccounted for.
  Separately also found and removed a redundant `-4` stacked on top of
  `contentH`, which is already `bodyPrefixHeight`'s *precise* measurement
  of everything above the content area — subtracting further was
  needlessly shrinking the usable window past what the exact math already
  gave for free, the same "flat guess instead of trusting the precise
  measurement" mistake fixed once already for `innerH` itself. Confirmed
  by driving real PgDn key presses in a test
  (`TestOverviewScrollReachesTrueEndOnASmallTerminal`) until the page's own
  trailing content was actually reached, not just by checking a
  fixed-size snapshot.

## Cross-reference

- [RFC 0012](rfcs/0012-agent-identity-deletion-and-key-fingerprinting.md) —
  implemented: revoked principals can be deliberately deleted by a HUMAN
  through the elevated CLI path, while every new event permanently attests
  the verified actor-key fingerprint so a reused ID's occupants remain
  distinguishable.
- [RFC 0011's "Known gaps"](rfcs/0011-managed-project-lifecycle-and-upgrades.md#known-gaps) —
  the TUI's one-confirmation upgrade-approval UX isn't built; a
  confirmation-required plan just blocks the TUI from launching instead.
