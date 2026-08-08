# agent-comms: dogfooding-on-itself field feedback

**Editor's note (2026-08-08):** this record is unedited from the day it describes. The `agy` (Antigravity CLI) worker adapter this document narrates building has since been removed from the project entirely, over an unresolved third-party ToS compliance question — see [`docs/backlog.md`](backlog.md#compliance--third-party-terms-of-service). The engineering lessons below (the permission-mode contract-test gap, the declarative-adapter-registration idea, the matcher n-gram fix) are all still real and still landed; only the specific adapter that motivated them is gone.

**Authors:** HENRY (Claude/claude-code), HULK (Google Antigravity / `agy`), PETER (opencode) — three concurrent agents, coordinating over Agent Comms itself, working on the AgentComms project.

**Date:** 2026-08-06

**Basis:** A single day's live session: shipping full native mouse support and minimum-size handling for the TUI, building and hardening the `agy` (Antigravity CLI) worker adapter end-to-end, and running three simultaneous `interactive-serve` PTY sessions (HENRY, HULK, PETER) that dispatched real, signed invocations to each other throughout. Every item below is tied to something that actually happened today, not a hypothetical — this is the same discipline [`docs/agent-comms-feedback.md`](agent-comms-feedback.md) (PRISM, 2026-07-14) established, applied to AgentComms dogfooding itself rather than an external project. Read that doc first — most of its items are already resolved, and two of the findings below are corrections to it, not new reports.

---

## Priority summary

| # | Issue | Severity | Source |
|---|-------|----------|--------|
| 1 | Working-directory lock exists but is coupled to the Task lifecycle, invisible for ad hoc work — directly caused a real incident today | **High — caused real duplicate/divergent commits** | HENRY |
| 2 | The 2026-07-14 doc's "no wake/notification" item (#9) is now stale and understates what shipped | Medium — misleading status, wrong scope | HENRY |
| 3 | No contract test catching "an adapter validates a config value with no corresponding CLI argument" | High — shipped exactly this bug in `agy` today | HENRY |
| 4 | Adapter CLI/env-var assumptions are never checked against the real installed binary | Medium — required manual `strings`-ing a binary to catch a real bug | HENRY |
| 5 | CLI output is unconditionally raw JSON, even for humans | Medium — constant `python3 -m json.tool`/`jq` friction | HENRY |
| 6 | Raw-PTY text injection has no structured acknowledgment of exact content | Medium — two independent, different symptoms hit live today | HENRY + HULK |
| 7 | Adding a CLI provider requires editing Go source across three files with no declarative path | Medium — direct build cost of adding `agy` | HULK |
| 8 | `echoed()`'s matching is closer to "does this substring exist" than genuine sequence matching | Medium — false-timeout risk across TUI layouts | HULK |
| 9 | Dead `interactive-serve` sockets from an ungraceful exit block the next launch | Medium — operational reliability | HULK |
| 10 | New `INTERACTIVE` runtimes need manual human policy approval before peer invocations work | Medium — onboarding friction for new agents | HULK |
| 11 | No way for a human to watch a live agent's PTY output from inside the TUI | Medium — UX gap for the operator | HULK |
| 12 | `agent register` and identity resolution are silent-on-success | Medium — confusing until you go looking | PETER |
| 13 | `invocation request` has a read-after-commit race against daemon processing | Low–Medium — confusing, self-resolving | PETER |
| 14 | `interactive-serve` silently auto-registers an unrecognized `--id` | Medium — typo becomes a phantom runtime | PETER |
| 15 | TUI row-action footer hides overflow actions with no indicator | Low — discoverability | PETER |
| 16 | Double-click on a row runs the *first* action, not a safe default | Low–Medium — surprising for destructive actions | PETER |
| 17 | `devBuildID()` hashes mtime, non-deterministic across machines/checkouts | Low — spurious daemon restarts on CI cache restore | PETER |
| 18 | TUI silently shows stale data through repeated read failures | Medium — no staleness signal to the operator | PETER |

---

## Part 1 — Agent workflow (HENRY)

### 1. The working-directory lock exists — but nothing nudges an agent to use it, and it cost us today

**What happened:** Earlier in this same session, a fix I'd made and committed (row-list/settings click-accuracy work) ended up on a branch tip that had diverged from `dev` — a separate concurrent line of ~20 commits had landed on the same branch, in the same working directory, reworking overlapping TUI code, with no coordination between the two. Nothing in the normal flow of "the user asks you to fix a bug, you fix it" surfaced that anyone else was mid-edit in the same repo. It resolved cleanly (the other line's fixes turned out to be equivalent-or-better, confirmed by diff), but that was luck, not a property of the system — exactly the outcome [PRISM's item #1](agent-comms-feedback.md#1-no-working-directory-leaselock-enforcement) warned about.

**Why it matters more now than it did in July:** The fix for #1 already shipped — `task claim --repo <path>` / `--worktree <path>` genuinely acquires a time-boxed lock with real conflict detection (`internal/protocol/transitions.go:571`, `"worktree %s is already leased by %s (task %s, expires %s)"`, confirmed by reading it). The mechanism is not the gap. The gap is that it's reachable only through the Task lifecycle — creating a Task, with a title and summary, to lock a directory before doing ad hoc conversational work is enough ceremony that it doesn't happen for the single most common shape of work in this exact setup: a human directly asking a live agent to "fix this bug," with no pre-existing Task. There's also no `CLAUDE.md` or project-local convention file establishing "claim the worktree before editing" as a norm — an agent has to already know the primitive exists and choose to reach for it unprompted.

**Proposed fix:** A lightweight lock that doesn't require a Task — closer to PRISM's original proposal of `agent-comms repo lock <path>` as an independent primitive, or a one-flag shortcut like `task claim --adhoc --worktree .` that auto-creates a minimal, throwaway task under the hood. Either way, it should be *cheap enough that reaching for it is less work than not bothering*, and worth a one-line convention note in a project-local `CLAUDE.md` (which does not currently exist for this repo) so an agent working on AgentComms is told to do it, not left to discover it.

### 2. Correction to PRISM's #9: the wake mechanism shipped, but the doc still says it didn't, and undersells its actual scope

**What happened:** All three of us — HENRY, HULK, PETER — were interrupted mid-turn today by a real, working notification the moment another agent dispatched an invocation to us (`internal/interactiveserve/interactiveserve.go`'s `NotifyInvocation`, "wake another's already-open interactive terminal session and deliver a notification about a pending signed invocation"). This is exactly the feature PRISM's #9 asked for. [`docs/agent-comms-feedback.md`](agent-comms-feedback.md) still marks it `⏳ Phase 5 — deferred; requires host-agent runtime integration` in its priority table.

**Why it matters:** A backlog/status doc that's silently gone stale is worse than no doc — it tells the next reader (human or agent) that a real gap still exists when it's actually closed, and it doesn't capture the one nuance that *does* still matter: this wake mechanism is real only for `INTERACTIVE`-kind runtimes (a live PTY session like ours). A `WORKER`-kind runtime (`runtime worker`, batch/poll-based) still has no equivalent — it finds out about new work only on its next poll. That distinction isn't written down anywhere.

**Proposed fix:** Update PRISM's table entry to `✅ Resolved for INTERACTIVE runtimes via interactiveserve.NotifyInvocation; WORKER runtimes remain pull-only`, and cross-reference from wherever `runtime worker`'s polling loop is documented, so a reader evaluating which runtime kind to use for a latency-sensitive agent gets a straight answer instead of having to read `interactiveserve.go` to find out. This is also a strong argument for a lightweight process to periodically re-verify a feedback doc's status column against the actual codebase, rather than trusting whoever last edited it.

### 3. The `agy` permission-mode bug I shipped and fixed today is a whole *class* of bug, not a one-off

**What happened:** While building the `agy` adapter, `validateAgyConfig` accepted `"acceptEdits"` (the default) as a valid `PermissionMode`, but `Arguments()` had no corresponding case for it — the default, most common configuration silently ran `agy` with neither `--mode` nor `--dangerously-skip-permissions` set at all, leaving it at an unspecified CLI default instead of the auto-accept-edits behavior the config clearly promised. I found this by manually comparing `validateAgyConfig`'s switch statement against `Arguments()`'s switch statement, by eye. It's exactly the kind of bug a table-driven, per-adapter test can catch without a human doing the comparison.

**Why it matters:** There are 9 registered adapters now (`internal/worker/adapter.go`'s `adapters` map: `agy`, `claude`, `codex`, `opencode`, `claude-acp`, `opencode-acp`, `codex-acp`, plus the `-live` variants), each hand-writing its own `Validate`/`Arguments` pair. Nothing currently proves that every `PermissionMode` value a given adapter's `Validate` accepts actually produces distinguishable, intentional CLI behavior in that same adapter's `Arguments()`. This is a structural gap, not specific to `agy` — I did not audit the other 8 for the same class of drift today, and I should have.

**Proposed fix:** A single shared test in `internal/worker`, table-driven over the `adapters` map, that (for every adapter implementing `cliAdapter`) enumerates every `PermissionMode` value its own `Validate` accepts and asserts `Arguments()` produces output that actually differs by mode wherever the adapter's own CLI has a real flag for that distinction — forcing every adapter's author to either wire the flag or explicitly document why a given mode is a no-op.

### 4. Verifying an adapter's assumed flags/env vars against the real binary is entirely manual today

**What happened:** `sessionbind.go` checked `ANTIGRAVITY_SESSION_ID` and `AGY_SESSION_ID` for `agy`'s session ID — neither is real. I found the actual variable (`ANTIGRAVITY_CONVERSATION_ID`) only by running `strings` on the installed `agy` binary and finding it embedded in a bundled JS sidecar script. Every unit test for this code passed the whole time, because nothing in the test suite exercises against the real binary — only synthetic `Config` structs and canned output.

**Why it matters:** This is the second time today a wrong-but-plausible assumption about an external CLI's real interface shipped and passed every test (see #3). Both times, the fix required a human-driven investigation (reading `--help`, `strings`-ing a binary) that a machine could do far more reliably and far more often.

**Proposed fix:** A lightweight `agent-comms adapter doctor --adapter agy` (or a `go generate`-style local tool, run manually or in CI when the adapter's binary is available) that runs `<executable> --help`, extracts the flag names it can find, and diffs them against the literal flag strings `Arguments()`/`Validate()` reference in source — flagging any assumed flag that doesn't appear in the real `--help` output. This wouldn't have caught the env-var issue (not visible in `--help`), but would have caught real flag drift automatically, and is a natural place to also grep the binary for the relevant env var patterns the way I did by hand.

### 5. CLI output is unconditionally raw JSON, even without `--json`

**What happened:** Every inspection command the user and I ran today to check on HENRY/HULK/PETER's runtime status, agent roster, or invocation history came back as a JSON blob requiring `python3 -m json.tool` or a Python one-liner to read — confirmed live: `agent-comms agent list` with no `--json` flag still prints raw JSON, not a table.

**Why it matters:** The TUI already solves this well (formatted tables, `fmtStatus` emoji indicators, column layout) — the CLI's default output path for the same underlying data doesn't benefit from any of that work. For a human operator doing ad hoc checks (not scripting), this is friction on every single command.

**Proposed fix:** A default human-readable table/summary format for list/inspect commands (reusing the same formatting logic the TUI already has, where reasonable), with `--json` continuing to mean "give me the machine-readable envelope" exactly as it does now.

### 6. Raw-PTY text injection has no structured acknowledgment of exact content — two different symptoms, same root cause, hit live today

**What happened:** I found and partially hardened one gap in `interactiveserve/matcher.go`'s `echoed()` (an invocation-ID substring fallback for when box-drawing/redraw artifacts break the full-text match). Independently, HULK's own brainstorm reply to this exact invocation arrived with several markdown code spans silently stripped in transit — almost certainly backtick command-substitution eating the enclosed text somewhere in the shell command that constructed HULK's `invocation request --instruction "..."` call, since HULK was composing the reply from inside its own live PTY session.

**Why it matters:** Both are symptoms of the same underlying design property: delivering a message by literally typing it into a terminal and reading the terminal's own rendering back has no error-correction layer and no acknowledgment of *exact* content, only "something matching arrived." This project already has a more robust answer for some adapters — ACP (Agent Client Protocol, used by `claude-acp`/`opencode-acp`/`codex-acp`) is a structured wire protocol, not text-into-a-pty. Interactive-serve sessions currently have no equivalent.

**Proposed fix:** Not a quick fix — flagging this as a real, structural direction worth a design pass: could interactive-serve-managed sessions eventually offer an ACP-style structured channel alongside (not necessarily replacing) raw PTY injection, at least for delivering the invocation payload itself? That would remove this whole class of transcription bugs rather than continuing to patch `echoed()`'s heuristics one observed failure at a time.

---

## Part 2 — From HULK (verbatim ideas, lightly formatted; live in `agy`/Antigravity via interactive-serve)

### 7. Declarative adapter registry
Adding a new CLI provider today means editing Go source across `worker/adapter.go`, `sessionbind.go`, and `app.go`. A declarative (JSON/YAML) registration path — executable lookup name, CLI flags template (e.g. `["--print", "--conversation", "{{.SessionID}}"]`), session environment variable names, TUI busy markers/prompt patterns — would let a new agent CLI be added without touching Go at all. (Directly informed by what it took to land `agy` today.)

### 8. Tokenized n-gram PTY echo matching
`interactiveserve/matcher.go`'s echo matching is a full-string containment check; TUIs (Bubbletea-based, like `agy`/`opencode`) interleave status headers and line-wrap borders that break it. The invocation-ID fallback (see #6 above) papers over one symptom; upgrading the underlying match to tokenized n-gram sequence matching would generalize the fix across TUI layouts instead of chasing individual failure modes one at a time.

### 9. Automatic stale-socket cleanup on startup
If an `interactive-serve` process is killed ungracefully, its unix socket file persists on disk and blocks the next launch for that runtime ID. Startup should perform a liveness check and unlink dead sockets automatically before binding, rather than requiring manual cleanup.

### 10. Registration policy defaults for interactive runtimes
Newly registered `INTERACTIVE` runtimes default to requiring manual human policy approval before they can process direct inter-agent invocations (`"approved invocation is required by target policy"`). A sane default (or an explicit opt-in flag at registration time) would remove friction from onboarding a new peer agent into an existing multi-agent setup.

### 11. Live PTY preview pane in the TUI
A human operator managing several `interactive-serve` runtimes can see their status (`ONLINE`/`OFFLINE`/health) but can't watch what they're actually doing without opening a separate terminal. A PTY-monitor view in `internal/tui` streaming live output over the same control socket would close that gap directly.

---

## Part 3 — From PETER (verbatim ideas, lightly formatted; live via `opencode`)

### 12. Registration is invisible-on-success, confusing-on-failure
`agent register` silently creates a user profile under `~/.config/agent-comms/config.json` with no mention in its own output — surprising later when `profile list` shows unexpected entries. The resolved actor source (a fresh agent process silently falling back to acting as the project owner) is likewise invisible. **Fix:** print the profile name and project root on registration; surface the resolved actor source explicitly.

### 13. `invocation request` has an eventual-consistency race
After `invocation request` commits its event, `invocationDeliveryOutcome()` reads state immediately — before the daemon may have finished processing it — producing a confusing "invocation not found after commit" warning for something that was *just* created. **Fix:** a short retry/poll before reading delivery outcome, or an honest warning message ("delivery outcome not yet available; check again with `invocation inspect`").

### 14. `interactive-serve` silently auto-registers an unrecognized runtime ID
If `--id` doesn't match an existing runtime, `runInteractiveServe` (`app.go:1310-1316`) silently registers a new one — a typo in `--id` creates a phantom runtime with no confirmation, and `MaxConcurrent` is hardcoded to 1 with no way to set it here. **Fix:** print `"Runtime X not found; auto-registering as INTERACTIVE"`, or gate it behind an explicit `--register` flag. *(Verified against the real code — accurate.)*

### 15. TUI row-action footer silently truncates on narrow terminals
`rowlist.go`'s footer keeps adding `[key] label` action hints only while they fit, then stops with no indicator — on a narrow terminal, later actions (e.g. `[x] revoke`, `[c] configure`) simply disappear with zero discoverability. **Fix:** show `"... +N more"` when hints are hidden.

### 16. Double-click runs the *first* action, not a safe default
Double-clicking an `ONLINE` runtime row fires `runtimeDrain` — the first action in that state's list, not necessarily the safest or most expected one. A `Confirm` prompt catches the destructive case, but "double-click = whatever's listed first" isn't intuitive. **Fix:** map double-click to an inspect/default action rather than positional first. *(Verified: `rowlist.go:462`, `triggerRowAction(actions[0], id)` — accurate.)*

### 17. `devBuildID()` hashes file mtime — non-deterministic across machines
`buildinfo.devBuildID()` hashes the executable's path, size, and modification time. Two byte-identical binaries at different paths or checkout times get different build IDs, which can trigger spurious daemon restarts when a CI cache restore changes mtimes without changing content. **Fix:** hash file content instead, or explicitly document the limitation. *(Verified: `internal/buildinfo/buildinfo.go:52-63` — accurate.)*

### 18. TUI `refreshSilent()` swallows read failures indefinitely
If the daemon crashes or becomes unreachable, the TUI keeps showing stale data with no staleness indicator — the toast notification only fires on genuinely new events, not on failed reads. **Fix:** after N consecutive read failures, surface a persistent "state read failing — data may be stale" indicator.

---

## What's already working well (preserve this)

Everything [PRISM's original doc](agent-comms-feedback.md) already said still holds — hash-chained signing, the registered-principal identity model, and the JSON envelope are all foundations we relied on heavily today, including for the very invocation round-trips (HENRY↔HULK, HENRY↔PETER, HULK↔PETER) that this document's evidence is built on. Worth adding from today specifically:

- **The invocation delivery evidence chain** (`PTY_TEXT_ECHOED` / `PTY_ENTER_SENT` timestamps on every interactive delivery) made every claim in this document independently checkable rather than trust-based — we could prove each round trip actually happened, not just that it was attempted.
- **The `runtime.list` / `invocation.list` JSON envelopes** carry enough detail (status, health, claim/lease timestamps, delivery evidence) to reconstruct exactly what happened after the fact, which is precisely what made today's three-way coordination auditable in the first place.
