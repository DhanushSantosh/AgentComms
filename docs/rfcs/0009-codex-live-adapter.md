# RFC 0009: `codex-live` — a persistent, natively-attachable Codex worker adapter

## Status

Proposed — implementation plan for a builder agent; not yet built.

## Context

`claude-live` (RFC 0008) gave Claude the same live-broadcast shape
`opencode-live` already had: one persistent process per runtime, a broker
of our own in front of it, and a real attach command any number of
terminals can watch. Codex has neither `opencode-live`'s native server nor
`claude-live`'s equivalent yet — only `codex` (spawns fresh per invocation,
no live view) and `codex-acp` (ACP-based, also spawns per invocation, no
live view, and already documented as having weaker permission enforcement
than the other ACP adapters).

The relevant primitive was investigated twice this session, with two
different, non-contradictory findings that a builder must not conflate:

- **Earlier this session**: a from-scratch Go WebSocket JSON-RPC test
  against `codex app-server --listen ws://...` used a driver connection to
  start a thread and a turn, then a *separate* observer connection did
  `thread/resume` on the same thread mid-turn. Across a confirmed
  in-progress turn, the observer received **zero** live notifications
  (`item/agentMessage/delta`, `turn/started`, `turn/completed`,
  `item/completed`) — only generic events. This proved Codex's app-server
  does not broadcast a thread's activity to a second, independent client
  connection the way OpenCode's server does. This finding is still correct
  and still relevant: it is exactly why a `codex-live` broker cannot simply
  let multiple raw JSON-RPC clients attach to the same app-server instance
  directly and expect to see each other's activity.
- **Just now, for this RFC**: `codex app-server` defaults to `--listen
  stdio://` (confirmed via `codex app-server --help`) — the same
  stdio-driven shape `claude --input-format stream-json` has. A Python
  test spawned one `codex app-server` process, did the `initialize` /
  `initialized` handshake, called `thread/start` once, then called
  `turn/start` twice on the **same** thread ID from the **same** process
  connection: turn 1 ("remember PICKLE, reply OK") got "OK"; turn 2 ("what
  word did I ask you to remember?"), sent after turn 1's `item/completed`
  had already arrived, correctly replied **"PICKLE"** — real conversational
  continuity within one process, one connection, driven entirely over
  stdio JSON-RPC.

Read together: Codex's app-server does not solve multi-viewer broadcast for
us (same conclusion as before), but it does give us the same thing Claude's
`stream-json` mode gave `claude-live` — **one persistent, programmatically
drivable process holding a real multi-turn conversation** — which is the
actual primitive `codex-live`'s own broker needs. We are not relying on
Codex's app-server to broadcast to multiple clients any more than
`claude-live` relies on Claude to do that; in both cases, *our own broker*
owns the one real process and rebroadcasts its output itself.

One loose end from tonight's verification, left for the builder to run
down rather than silently assumed: across the two `turn/start` calls in
the test, only **one** `turn/completed` notification was observed in total
(the second one, referencing a turn ID that also appeared attached to the
first turn's completed `agentMessage` item) — plausibly because the second
`turn/start` was issued immediately after the first turn's `item/completed`
event rather than after an explicit `turn/completed`, and the app-server
folded them together. **Do not build turn-completion detection around
`turn/completed` alone without re-verifying this.** The safer, empirically
observed signal is: an `item/completed` notification whose `item.type ==
"agentMessage"` and `item.phase == "final_answer"`, carrying the turn's
full final text in `item.text`. Cross-check against `turn/completed` when
present, but treat the `agentMessage`/`final_answer` item as the
authoritative "this turn's answer is ready" signal.

## Decision

Build `codex-live`, structured the same way as `claude-live`
(`internal/claudeserve`), reusing exactly as much of that package's design
as applies, with the differences the JSON-RPC protocol and Codex's own
session-identity model actually require — do not paper over those
differences to force a closer parallel than is real.

### Package layout

New package: `internal/codexserve` (parallel to `internal/claudeserve` and
`internal/opencodeclient`).

- **`process.go`** — wraps one long-lived `codex app-server` subprocess.
- **`server.go`** — the HTTP+SSE broker: spawn-or-reuse-on-a-fixed-port,
  copied structurally from `claudeserve/server.go`'s
  `resolveRunningServer`/`EnsureServer` shape — same orphan-avoidance
  lesson, still non-negotiable: **fixed port** (suggest `4098`, since `4096`
  is OpenCode's and `4097` is Claude's — confirm nothing else on the host
  already uses it), and probe that fixed port directly whenever the cache
  file is missing or stale, never spawn a second instance underneath an
  already-running one.
- **`client.go`** — Go client other code uses to talk to *our own* broker.

New adapter: `internal/worker/adapter_codex_live.go` (+
`adapter_codex_live_test.go`), registered as `"codex-live"` in
`internal/worker/adapter.go`'s `adapters` map — a sixth adapter alongside
`claude`, `codex`, `claude-acp`, `opencode-acp`, `codex-acp`,
`opencode-live`, and `claude-live`. Does not replace `codex` or
`codex-acp`.

New CLI commands, extending `internal/app/app.go`'s pattern from
`claudeCmd()`/`claude serve`/`claude attach`:

- `agent-comms codex serve` — foreground broker, auto-spawned detached by
  `EnsureServer` the same way `claudeserve`/`opencodeclient` do.
- `agent-comms codex attach --runtime <id> [--server <url>]` — subscribes
  to the broker's SSE stream and renders turns live. **Cannot reuse
  `claudetail.Format`** the way `claude attach` did — Codex's JSON-RPC
  notification shape (`{"method":"item/agentMessage/delta","params":
  {...}}`, `{"method":"item/completed","params":{"item":{"type":
  "agentMessage","text":"..."}}}`) is a different wire format from Claude's
  transcript-line shape. Write a small renderer in `codexserve` (or a
  `codextail`-equivalent, name at the builder's discretion) that recognizes
  `item/completed` for `userMessage` and `agentMessage` item types and
  prints them the same `--- USER ---` / `--- ASSISTANT ---` style
  `claudetail.Format` already established, for a consistent operator
  experience across both attach commands — but implement it as its own
  parser, not a forced reuse.

### `process.go`: the persistent subprocess wrapper

```go
type Process struct { /* cmd, stdin io.WriteCloser, stdout scanner, subscribers, mu, threadID, nextRequestID, pending map[int64]chan json.RawMessage, ... */ }

func Start(ctx context.Context, config ProcessConfig) (*Process, error)
```

Differences from `claudeserve.Process` that matter, not cosmetic:

- **JSON-RPC framing, not a bare line protocol.** Every request needs a
  correlated ID and a response-matching table, closer to the shape below
  (confirmed working in this session's own verification script) than to
  `claudeserve.Process`'s simpler scan-for-a-`"type":"result"`-line loop:

  ```go
  type pendingCall struct{ result chan json.RawMessage }

  // On the reader goroutine, for each decoded line:
  //   if msg["id"] present and known -> deliver to that pending call's channel
  //   if msg["method"] present and no "id" -> it's a notification, broadcast it
  //
  // call() writes {"jsonrpc":"2.0","id":N,"method":...,"params":...}\n,
  // registers a channel under N in a mutex-guarded map, and blocks on that
  // channel (with a timeout) for the matching response.
  ```

  This is standard JSON-RPC 2.0 client bookkeeping — build it directly
  rather than searching for a prior scratch file, since none is guaranteed
  to still exist by the time this is implemented.
- **Startup handshake is mandatory and stateful**: `initialize` (blocking,
  wait for its response) then a fire-and-forget `initialized` notification,
  *before* `thread/start`. `claudeserve.Process` has no equivalent
  handshake step.
- **Session identity is Codex's to mint, not ours to choose.** Claude's
  `--session-id` lets a caller create a conversation at a chosen UUID.
  Codex's `thread/start` always returns a server-minted thread ID; there is
  no "create at this exact ID" call. This makes `codex-live`'s continuity
  story structurally like `opencode-live`'s, not like `claude-live`'s: on
  first use for a runtime, call `thread/start` and **persist the returned
  thread ID locally** (mirror `openCodeLiveSessionPath`/
  `loadOpenCodeLiveSessionID`/`saveOpenCodeLiveSessionID` in
  `internal/worker/adapter_opencode_live.go` almost verbatim, substituting
  a Codex-appropriate cache filename); on later invocations, load the
  cached thread ID and call `thread/resume` (not `thread/start` again) to
  attach to the existing thread before issuing `turn/start`. Do **not**
  copy `claude-live`'s `--session-id`-is-required-upfront design for this
  adapter — it doesn't fit Codex's identity model.
- **Turn completion detection**: as established in Context above, key off
  `item/completed` where `item.type == "agentMessage"` and `item.phase ==
  "final_answer"`, using `item.text` as the aggregated result. Re-verify
  live with an actual governed action (a Bash call, similar to the
  permission-denial verification `claude-live`'s builder ran) before
  trusting this as the sole completion signal in production — tonight's
  test only exercised plain text turns, never a tool call.
- **Crash recovery**: same shape as `claudeserve.Process.restart` — on an
  unexpected process exit, respawn and call `thread/resume` (thread
  definitely exists by then) before retrying the failed turn once.
- **Broadcast**: every raw JSON-RPC line (both responses and
  notifications) goes to every subscriber, same as
  `claudeserve.Process.broadcast` — `codex attach` needs the full
  notification stream, not just the one aggregated answer a `Send`-style
  call would return to the adapter.

### `internal/worker/adapter_codex_live.go`

```go
type codexLiveAdapter struct{}

func (codexLiveAdapter) Validate(config *Config) error {
    // Reuse codexAdapter.Validate's body: sandbox check, add-dir
    // validation. --session-id is NOT required upfront here, unlike
    // claude-live -- codex-live mints and persists its own thread ID the
    // way opencode-live persists its own session ID.
}

func (codexLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
    baseURL, err := codexserve.EnsureServer(ctx, config.WorkDir, config.WorkDir)
    codexExecutable, err := exec.LookPath("codex")
    client := codexserve.New(baseURL)
    // register-or-reuse this runtime's process, resolving thread ID via
    // the cached-thread-ID pattern described above, not via config.SessionID
    config.Status("watch this runtime's Codex activity live in a terminal: " +
        "agent-comms codex attach --runtime " + config.RuntimeID + " --server " + baseURL)
    output, err := client.Prompt(ctx, config.RuntimeID, codexPrompt(config.Actor, invocation))
    // reuse codexPrompt exactly as the exec-based codex adapter does
    ...
}
```

Register as `"codex-live"` in `adapter.go`'s `adapters` map.
`RequiresExecutable("codex-live")` should return `false` automatically
(doesn't implement `cliAdapter`) — verify with a test the same shape as
`TestRequiresExecutableExcludesOpenCodeLive`/the `claude-live` equivalent,
don't just assume the pattern holds without checking.

### CLI flags

`runtime worker --adapter codex-live` should accept `--codex-sandbox` and
`--codex-add-dir` the same as `codex`/`codex-acp` do today. `--session-id`
is optional here (unlike `claude-live`, where it's required) — if supplied,
treat it as a thread ID to attempt `thread/resume` against on first boot,
falling back to `thread/start` (and caching the newly-minted ID) if that
resume fails, mirroring `opencode-live`'s own "configured or cached ID that
no longer resolves falls back to creating a fresh one" behavior rather
than hard-failing.

## Testing plan

Same shape as `claude-live`'s, adapted:

- Unit tests for `codexserve.process.go`'s JSON-RPC request/response
  correlation and turn-completion detection, driven against a fake `codex`
  script (same `AGENTCOMMS_FAKE_CLAUDE_PROCESS`-style self-exec trick
  `claudeserve`'s tests use, adapted for JSON-RPC framing and a `thread/
  start` → `turn/start` → `item/completed` sequence instead of a bare
  `type":"result"` line).
- Unit tests for the fixed-port-probe-before-spawn logic — port
  `TestResolveRunningServerPrefersCache`/`...FallsBackToFixedPort`/
  `...ReportsNone` directly, same as `claudeserve`'s did from
  `opencodeclient`'s.
- A manual smoke test gated behind an env var (match the existing
  per-adapter convention — `AGENTCOMMS_CODEX_LIVE_SMOKE=1` or similar),
  running a real "remember X" / "what was X" two-turn test against the
  actual `codex` binary, the same regression-test shape
  `TestManualSmokeClaudeLive` already established.
- **I will test this live once it's built**, the same way `claude-live` was
  verified: a real runtime, a real invocation, a real `codex attach`
  terminal already open across it, confirmed to update without a restart —
  and, given tonight's flagged uncertainty, specifically a governed
  Bash-calling turn, not just a plain text one, to settle the open question
  about whether `item/completed`/`final_answer` alone is a sufficient
  completion signal under governance.

## Documentation

Once built and verified, the same three places every prior adapter touched:
`docs/agent-invocations.md`'s adapter table and a `### codex-live` section
mirroring `opencode-live`'s and `claude-live`'s; a `CHANGELOG.md` bullet;
rebuild and reinstall the binary with `mv`, not `cp`, onto a possibly-loaded
binary.

## Consequences

A `codex-live` runtime gets the same live-broadcast parity `opencode-live`
and `claude-live` already give their providers: one persistent process,
one broker, one real attach command. The plain `codex` and `codex-acp`
adapters are unaffected and remain the default choices for ordinary
automated invocations.

Open questions the builder must resolve rather than guess past: whether
`item/completed`/`agentMessage`/`final_answer` remains a reliable, sole
completion signal once a turn involves an actual tool call and governed
permission decision (untested so far — tonight's verification only
exercised plain text turns); and whether Codex's `--sandbox` /
`--dangerously-bypass-approvals-and-sandbox` interact with `app-server`
mode any differently than they do with `codex exec`, since `app-server` is
explicitly marked `[experimental]` in its own `--help` output and may not
share every guarantee `codex exec` documents.
