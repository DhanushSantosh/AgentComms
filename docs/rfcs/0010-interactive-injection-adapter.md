# RFC 0010: terminal-injection adapters — driving a real, already-open interactive session

## Status

Proposed — implementation plan for a builder agent; not yet built. This is
a genuinely different, harder problem than RFC 0008/0009, and this
document deliberately leaves the central design tension open rather than
resolving it, because resolving it is the actual implementation work.

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

**Recommendation, not a mandate**: start with Option A. It's a smaller,
fully-contained change (one new adapter, zero changes to
`internal/worker/worker.go`), and it's testable in isolation the same way
every other adapter in this codebase is. Only move to Option B if Option
A's polling proves too slow or too fragile in practice — and if so, that
decision belongs in a follow-up RFC of its own, not folded silently into
this one.

## Decision (assuming Option A)

### Reading the answer back

Since injection has no return channel the way an HTTP response or a
process's stdout does, `Execute` needs another way to know the turn
finished and what it said. Two sub-options, and this is explicitly left
for the builder to pick based on what proves reliable, not decided here:

1. **Poll the pane's rendered output** (`tmux capture-pane -p`) for the
   prompt marker returning to idle (e.g. Claude's own `❯` prompt with no
   "Thinking…"/working indicator), then parse the visible text since the
   injected prompt. Fragile across Claude Code/Codex UI changes and across
   terminal widths (text can wrap), but requires nothing from the agent
   itself.
2. **Have the injected prompt instruct the agent to also write its answer
   to a bounded local file** (e.g. `.agent-comms/tmp/injected-result-
   <invocation-id>.txt`) as its very last action, and have `Execute` poll
   for that file's existence rather than parsing rendered terminal output.
   More robust than option 1, but requires the interactive agent to
   reliably follow that one extra instruction every time, and requires a
   real, bounded write location + cleanup story (don't leave these files
   accumulating forever).

Whichever is chosen, `Execute` must have a real timeout (matching
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
- **Option B from the start**: rejected as the default for this RFC (not
  forever) because it requires a `Worker.Run` lifecycle change before a
  single adapter exists to prove the injection mechanism itself works.
  Prove Option A works first; consider Option B only if polling turns out
  to be the actual bottleneck.

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

If Option A works reliably, this gives Claude and Codex runtimes the one
thing `claude-live`/`codex-live` structurally cannot: the actual native
chat UI, live, the same category of experience `opencode attach` already
provides for OpenCode. It comes with a narrower operating envelope than
every other adapter in this project — a single shared terminal that
nothing else may touch while an invocation is running — which is a real,
ongoing operational cost, not a one-time implementation detail to get past.

Open questions the builder must resolve, not guess past: whether pane-
output polling or a self-written result file is the more reliable way to
read the answer back (Section "Reading the answer back"); whether a
single-flight mutex is sufficient protection against concurrent access to
the shared pane in practice, given this project's own workers already run
as long-lived background processes that a human could plausibly also
`tmux attach` to and start typing into without realizing an invocation is
in flight.
