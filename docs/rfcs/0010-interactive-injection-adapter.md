# RFC 0010: terminal-injection adapters — driving a real, already-open interactive session

## Status

**Built, in a narrower form than originally scoped, and only for `codex` and
`opencode`.** Live-tested this session: `claude` was tried three separate
ways — a bare third-party-attributed claim, the same claim backed by a real
verifiable signature in this project's own signed event log, and durable
pre-session authorization via CLAUDE.md — and Claude correctly refused all
three, reasoning each time that verified integrity of a claim is not the
same as the human having authorized autonomous action in this session, and
that a file or injected message cannot grant itself that authorization. That
is not a wording gap to keep iterating on; it is Claude's injection defense
working as intended against exactly the shape of thing this RFC needs to
deliver. `claude-interactive` is dropped, not deferred.

**Update, re-tested live against the shipped pty-owning `interactive-serve`
(not the earlier tmux prototype): the refusal above is risk-proportionate
judgment, not a categorical block.** A genuinely independent `claude` session
(no `CLAUDE_CODE_CHILD_SESSION` marker — a real top-level session, the same
as a human would get launching it themselves) was sent a low-stakes,
unambiguous invocation ("reply with the word PONG") through the exact same
mechanism `codex`/`opencode` use. It read `AGENT_INSTRUCTIONS.md`, reasoned
about the request out loud ("this is a legitimate test harness... the
pending invocation is trivial and non-sensitive"), explicitly declined to
act on unrelated unverified files sitting in the same directory, and then
ran the real `claim → start → complete` lifecycle itself and replied
`PONG`. So Claude *can* be reached as a delivery target and *can* complete
work this way — the three refusals above stand as accurate for what they
tested (claims of authorization for autonomous action), not as "Claude will
never act on anything delivered this way."

A separate, more mundane blocker showed up in the same test: this account's
default Claude Code permission mode is `manual`, so every CLI call Claude
made (`status`, `invocation claim`, `start`, `complete`, ...) stopped on a
`Do you want to proceed?` prompt — five separate approvals, by hand, to get
through one trivial exchange. In a real unattended deployment this hangs
forever on the first prompt regardless of what Claude decides about the
instruction. `runtime interactive-serve --claude-allow-agent-comms` closes
this specific gap, like `runtime worker`'s existing flag of the same name —
but the first implementation of it, built by analogy with the worker flag
and not yet live-tested, turned out to be a no-op: it scoped a single
`--allowedTools "Bash(<resolved absolute path> *)"` rule, but the
notification text `NotifyInvocation` delivers (and Claude's own follow-up
`invocation claim/start/complete` calls) all invoke the bare `agent-comms`
name via `PATH`, never the resolved path, so the rule silently matched
nothing and every call still prompted. A live 3-way test with `codex`,
`opencode`, and `claude` all reachable simultaneously caught this — the fix
scopes two rules, the resolved absolute path and the bare basename, and the
same test confirmed the fixed version actually removes the approval prompts
(one manual approval to reassure Claude the environment was legitimate, then
`invocation claim/start/complete` ran unattended). It does not and cannot
change the judgment-based refusal above — that ceiling is real, by design,
and orthogonal to permission mode.

**3-way delivery, confirmed live:** `codex-runner`, `claude-runner`, and
`opencode-runner` were run simultaneously (three separate `interactive-serve`
processes, three different provider CLIs) and each completed a direct
invocation. To confirm this is genuine N-way delivery and not three
coincidentally-co-located pairwise tests, `codex-runner` was also given an
invocation instructing it to issue its own `agent-comms invocation request
--to opencode-runner` — a real, agent-initiated cross-invocation, not one
fired externally — and `opencode-runner` received and completed it
(`requested_by: codex-runner` in the signed event). The concurrency/locking
design already claimed this generalizes past pairwise "for free"; this is
the first time it was actually exercised with three distinct, real provider
processes live at once, closing the "only two providers proven" gap from
this project's own known-limitations list.

**Full 3-agent collaborative build, confirmed live, in three real Alacritty
windows (not a headless test harness):** `opencode-runner`, `claude-runner`,
and `codex-runner` were each run under `interactive-serve` in their own
visible terminal, then given a single kickoff invocation describing a
three-step build with explicit handoff instructions for each hop.
`opencode-runner` built a small stats CLI and committed it, then — on its
own initiative, not externally triggered — issued the invocation handing
step 2 to `claude-runner`. `claude-runner` wrote and ran a pytest suite
against it (with real conditional logic to send a fix-request back to
`opencode-runner` and retry if any test failed, though in this run every
test passed on the first try, so that branch never had to fire), then
itself handed step 3 to `codex-runner`. `codex-runner` wrote documentation,
independently re-ran the tool to verify the reported output, committed, and
reported the whole build complete. Verified externally afterward: real git
history with one commit per agent, and the full pytest suite passing when
re-run independently. The user then ran their own separate live regression
pass across all three runtimes directly in the terminals and confirmed the
mechanism as solid.

What shipped — originally scoped to `codex`/`opencode` only, before the
"Update" sections above extended endorsed support to `claude` as well — is
simpler than the Option A design below: rather than a new adapter type
registered in `adapter.go` and
driven through `Worker.Run`/`Adapter.Execute`, delivery is a direct,
synchronous side effect of `agent-comms invocation request` itself
(`internal/interactiveserve`, wired into the `request` command in
`internal/app/app.go`). This was a deliberate scope choice made live with the
user in place of building the full worker-driven Option A machinery, once it
was clear the interesting, previously-unsolved part was "wake a live session
directly," not "make an adapter fit the claim/start/complete lifecycle" —
every other adapter already does the latter.

**The delivery mechanism itself went through two iterations, and the first
was explicitly rejected by the user, not just superseded.** The first
shipped version used tmux: a runtime registered its live tmux pane
(`runtime interactive-register --tmux-session <name>`), and delivery shelled
out to `tmux send-keys`/`tmux capture-pane`. The user rejected this
outright — *"i dont want that it will force users to install and use only
tmux - i just want a simple use case where i can open 2 sessions on any
terminal within a certain project and have them communicate with each
other"* — and confirmed replacing it entirely rather than keeping it as an
option alongside something new.

**What replaced it: AgentComms owns the pty directly, instead of relying on
an external multiplexer to.** `agent-comms runtime interactive-serve --id
<runtime> -- <command>` allocates a real pty (`github.com/creack/pty`),
execs `codex`/`opencode` attached to it, and transparently forwards the
wrapper's own stdin/stdout so *any* terminal emulator — not tmux
specifically — shows the child's real native UI unmediated. The owning
process listens on a control socket at a path deterministic in (project
root, runtime ID); "is a runtime live" is simply "can I dial its socket," so
there is no separate local socket registry file. The stabilized invocation
path treats `--to` as an agent ID and resolves registered runtimes to their
owning agent; a same-ID runtime remains the zero-registration fallback.
When multiple eligible runtimes are live, delivery requires an explicit
`invocation redeliver --runtime` choice instead of guessing. This is
unix-only (creack/pty doesn't support Windows) — not a regression, since
tmux itself never worked on Windows either.

Every lesson from the tmux iteration carried over, just re-implemented
against an owned pty instead of shelling out: **readiness/idle detection**
(`Deliver` waits for a busy-marker heuristic — `esc to interrupt` / `esc
again to interrupt` — to clear before sending anything, fixing the same
mid-turn text-gluing failure found live) and **echo verification** (waits
for the pty to visibly echo sent text before pressing Enter, fixing the
same silently-dropped-Enter failure found live) both still apply, now
reading from an in-process tee of the child's raw output instead of `tmux
capture-pane`. One genuinely new risk came with owning the raw byte stream
instead of tmux's already-rendered screen grid: stale, pre-clear content
could sit in a naive tail buffer and produce a false match after a real
screen repaint — mitigated by resetting the match buffer on a
clear/alternate-screen-buffer escape sequence. **Concurrency got simpler,
not just re-implemented**: with one process now owning each runtime's pty,
concurrent senders just make concurrent socket connections, serialized by a
plain in-process mutex — no cross-process file lock, no shared bindings-file
race, and no `tmux send-keys` argument-injection surface to defend against
at all (writing straight to a pty fd has no argv-parsing layer to exploit).
See `docs/agent-invocations.md`'s "Direct delivery into a live interactive
session" and "Many-to-many delivery: concurrency and hardening" sections for
the full, current writeup, and `internal/interactiveserve/serve.go` for the
implementation.

The original plan below (Option A, `Execute` blocks and polls, a
self-written result file as read-back, a genuine `claude-interactive`/
`codex-interactive` adapter pair) was **not** built as written and is kept
here as considered-and-superseded context, not a to-do list.

## Context

`claude-live` and `codex-live` (RFC 0008, RFC 0009) both solve "a human can
watch this runtime's activity live" by building our own broker around a
persistent, headless process, with our own attach command rendering our
own plain-text view of it. That is not Claude's or Codex's own real chat
UI — confirmed explicitly this session when the user pushed back: "not
Claude Code's actual chat window" is what `claude attach` shows, and there
is no way to make Claude's or Codex's actual closed-source interactive TUI
refresh when driven by a separate headless process. That fact hasn't
changed and this RFC doesn't change it either.

What *is* different, and was the user's own insight that prompted this
RFC: an **already-open, real, interactive** `claude`/`codex` session —
the actual native UI, not our broker's text feed — genuinely does update
live when new input arrives through its own controlling terminal. This
was proven empirically, repeatedly, all session: every time this
conversation injected text into an open `tmux` pane running an interactive
`claude` session via `tmux send-keys`, that pane's real UI rendered the new
turn immediately, because it was never a separate process — it was the
same one process receiving real keystrokes on its own stdin/pty.

So the idea this RFC captures: instead of building another headless
broker, keep one real interactive `claude`/`codex` session open (in a
tmux pane this project manages), and build an adapter that delivers each
invocation by injecting it as terminal input into that session, rather
than by spawning a process or calling an HTTP API. The payoff is exactly
what `claude-live`/`codex-live` couldn't give: the actual native chat UI,
live, the same way `opencode attach` already is for OpenCode.

## The central design tension — read this before implementing anything

Every adapter in this project today (`claude`, `codex`, `claude-acp`,
`opencode-acp`, `codex-acp`, `opencode-live`, `claude-live`, and (per RFC
0009) `codex-live`) shares one lifecycle: `Worker.Run` claims an
invocation, calls `Adapter.Execute(ctx, config, invocation) (string,
error)` **synchronously**, and completes the invocation itself with
whatever `Execute` returns. The adapter's only job is to answer the
question; the worker owns claim/start/complete around it.

An injection-driven adapter cannot fit that shape cleanly, and pretending
it can is the mistake to avoid. Two real options, not a third — pick one
deliberately, don't half-do both:

**Option A — `Execute` blocks and polls, worker lifecycle unchanged.**
`Execute` injects the prompt into the tmux pane, then polls
`agent-comms invocation list`/status for *this invocation's own ID* to
reach a terminal state, itself completed some other way (see below), and
returns once it does. This keeps `Worker.Run`'s claim/start/complete loop
completely untouched — the adapter just has an unusually slow, unusually
indirect way of producing its return value. The catch: something still
has to actually call `invocation complete` for this invocation. If the
worker's normal post-`Execute` completion step also fires, you get a
double-completion attempt. The clean resolution: the interactive agent
itself must **not** call `invocation complete` — the worker still does
that, using the text the polling loop extracted (see "reading the answer
back," below) as `Execute`'s return value. In this option, injection only
ever *delivers the prompt*; completion still flows through the existing,
unmodified worker path.

**Option B — the agent self-completes, worker lifecycle changes.**
The injected prompt instructs the running interactive agent to call
`agent-comms invocation complete --id <id> --summary ...` itself via
Bash once it's done, reusing the exact same self-invocation convention
`--claude-allow-agent-comms` already grants for follow-ups. This means the
*worker* must not also try to complete the invocation after `Execute`
returns — `Execute` would need to signal "already completed, do nothing
further," which does not exist as a concept anywhere in `internal/worker`
today and would require a real, deliberate change to `Worker.Run`, not
just a new adapter file. Do not build this option by accident by writing
an `Execute` that happens to also shell out to `invocation complete` --
that produces exactly the double-completion race this paragraph exists to
warn about.

**Decision: Option A, not a starting point to revisit — a deliberate,
long-run choice.** This project's completion model is deterministic and
worker-owned everywhere, for every one of the six adapters that exist
today: `Execute` returns, the worker completes the invocation, and any
failure becomes an auditable `WAITING` reason through one single,
well-tested code path. That uniformity is a real asset, not an accident of
how the code happened to grow. Option B trades it away permanently: it
requires `Worker.Run` to special-case "this invocation might complete
itself," it opens a real race (the worker's own timeout firing while the
agent is mid-way through calling `invocation complete` by hand, or the
model simply forgetting to), and it hands a governance-relevant state
transition — whether work counts as done — to a Bash command the model has
to type correctly under real conditions, which is a strictly less
controlled mechanism than the worker deciding it in code. It also sets a
precedent that erodes the very thing that makes this system auditable: if
one adapter self-completes, "why not all of them" becomes a reasonable
question with no good answer. None of that is worth trading for a
plumbing-level simplification. Do not revisit Option B unless Option A is
first built, measured, and found to have a real, specific problem Option A
cannot fix — and if that happens, it belongs in a new RFC, not a
retroactive change to this one.

## Decision

### Reading the answer back

Since injection has no return channel the way an HTTP response or a
process's stdout does, `Execute` needs another way to know the turn
finished and what it said. **Decided: a self-written result file**, not
pane-output scraping. Have the injected prompt instruct the agent to write
its final answer to a bounded local file (e.g.
`.agent-comms/tmp/injected-result-<invocation-id>.txt`) as its very last
action, and have `Execute` poll for that file's existence. This is still an
instruction the model has to follow, but it fails closed the same way
everything else in this design does: no file within the timeout means
`Execute` returns an error and the invocation goes to `WAITING`, the same
as a governed action being denied does elsewhere in this codebase — no
different in kind from the model failing to answer at all. Pane-output
scraping was rejected: it's fragile across Claude Code/Codex UI version
changes and terminal-width-dependent text wrapping, and a UI redesign
upstream would silently break it with no error at all, which is a worse
failure mode than "the model didn't write the file this time." A bounded
write location plus a cleanup step (delete the file once read, and sweep
any left over past a TTL) closes the one real cost of this approach.

`Execute` must have a real timeout (matching
`ExecutionTimeout`, the same bound every other adapter already respects)
and must return a clear error — not an empty success — if it can't
determine the answer within that bound, the same principle
`acpResult`/`claude-live`'s permission-denial handling already enforce
elsewhere in this codebase.

### Session lifecycle: ensure a real interactive session is open

New package, e.g. `internal/interactiveserve` (name at the builder's
discretion, but keep it distinct from `claudeserve`/`codexserve` — this is
a genuinely different mechanism, not a variant of either):

- `EnsureSession(ctx, runtimeID, provider, workDir, sessionID string) error`
  — checks whether a tmux session (name derived from `runtimeID`, e.g.
  `agent-comms-live-<runtimeID>`) already exists and is running the
  expected interactive process; if not, creates one
  (`tmux new-session -d -s <name> ...`) and starts the real interactive
  `claude --resume <id>` or `codex resume <id>` (or the provider's
  equivalent "continue this conversation interactively" invocation) inside
  it. This is conceptually the same "ensure a persistent thing exists,
  reuse if healthy, create if not" shape `opencodeclient.EnsureServer` and
  `claudeserve.EnsureServer` both already have — but the "thing" here is a
  tmux session, not an HTTP server, so don't force-fit the HTTP client
  code from either of those packages; this needs its own, tmux-native
  health check (does the session exist? is its pane's current command the
  expected `claude`/`codex` process, not a dead shell?).
- **Readiness/idle detection before injecting**: before calling `tmux
  send-keys`, confirm the pane isn't already mid-turn (otherwise the new
  input interleaves with whatever's currently running). A single-flight
  mutex per runtime, held for the duration of one `Execute` call, is
  suf­ficient to prevent *this adapter* from double-injecting — it does
  not protect against a human simultaneously typing into the same pane by
  hand, which the existing `--session-id` documentation already warns
  against for exactly this reason ("do not process an interactive turn in
  that conversation at the same time").
- **Reliable injection**: this session's own experience matters here —
  `tmux send-keys "<text>" Enter` was observed to be unreliable for at
  least one pane tonight (text stayed queued, unsent, until a *separate*
  `tmux send-keys Enter` call landed). Send the text and the Enter
  keystroke as two distinct `send-keys` calls, not one combined call, and
  verify the text was actually submitted (e.g. by confirming the pane's
  visible prompt line no longer shows the raw injected text sitting
  unsent) before considering the turn "started."

### Adapter

```go
type claudeInteractiveAdapter struct{}   // and a codexInteractiveAdapter twin

func (claudeInteractiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
    if err := interactiveserve.EnsureSession(ctx, config.RuntimeID, "claude", config.WorkDir, config.SessionID); err != nil {
        return "", fmt.Errorf("claude-interactive: ensure session: %w", err)
    }
    config.Status("watch this runtime's real Claude session live: tmux attach -t agent-comms-live-" + config.RuntimeID)
    return interactiveserve.InjectAndAwait(ctx, config.RuntimeID, claudeUserPrompt(invocation), config.ExecutionTimeout)
}
```

Register as `"claude-interactive"` / `"codex-interactive"` in
`adapter.go`'s `adapters` map — two more adapters, alongside everything
that already exists, replacing none of it. `config.Status` here points the
operator at a real `tmux attach`, not a custom `agent-comms ... attach`
command — there is no broker to attach to in this design; the terminal
*is* the thing.

## Alternatives considered

- **Building this as an extension of `claude-live`/`codex-live` instead of
  a new adapter**: rejected. Those two are correctly scoped around a
  headless, our-own-UI broker; bolting tmux-pane injection onto them would
  conflate two different mechanisms (an HTTP broker vs. a pty) inside one
  package, and would force every consumer of `claudeserve`/`codexserve` to
  understand a code path that has nothing to do with HTTP or SSE.
- **Option B (agent self-completes via Bash)**: rejected, and not as a
  placeholder — it trades away the deterministic, worker-owned completion
  model every other adapter in this project relies on, for a plumbing-level
  simplification that isn't worth that cost. See "The central design
  tension" above for the full reasoning.

## Testing plan

- Unit tests for `interactiveserve`'s tmux-session-exists/health check
  logic, using a real (throwaway) tmux server the test spawns and tears
  down — this project doesn't have a fake-tmux convention yet, so this
  will need real `tmux` invocations in tests, gated the same way manual
  smoke tests already are if that proves flaky in CI.
- A manual smoke test, gated behind an env var matching the existing
  per-adapter convention, that: starts a real interactive `claude`
  session in a tmux pane, injects a "remember X" invocation, injects a
  "what was X" invocation, and confirms the second answer is correct —
  the same continuity check every other adapter's smoke test already
  performs, just via keystroke injection instead of an API call.
- **I will test this live once it's built**, watching the actual tmux pane
  (not a viewer command, since none exists in this design) update in real
  time as a separate process delivers an invocation — the literal
  scenario the user originally asked for and that `claude-live` could not
  provide.

## Documentation

Same three places as every prior adapter, once built and verified. Be
explicit in `docs/agent-invocations.md` that this adapter's "live view" is
the real native UI itself (no separate attach command, no broker to run),
which is the one meaningful advantage it has over `claude-live`/
`codex-live` — and be equally explicit about its cost: a human (or another
process) must never type into that same tmux pane while an invocation is
in flight, and only one invocation can be in flight at a time per runtime,
same as every other adapter already enforces, but here the consequence of
violating it is silent corruption of a shared terminal, not a clean error.

## Consequences

Built as decided (Option A, result-file read-back), this gives Claude and
Codex runtimes the one thing `claude-live`/`codex-live` structurally cannot: the actual native
chat UI, live, the same category of experience `opencode attach` already
provides for OpenCode. It comes with a narrower operating envelope than
every other adapter in this project — a single shared terminal that
nothing else may touch while an invocation is running — which is a real,
ongoing operational cost, not a one-time implementation detail to get past.

Open question the builder must still resolve, not guess past: whether a
single-flight mutex is sufficient protection against concurrent access to
the shared pane in practice, given this project's own workers already run
as long-lived background processes that a human could plausibly also
`tmux attach` to and start typing into without realizing an invocation is
in flight. The read-back mechanism and the Option A/B question are both
decided above, not open.
