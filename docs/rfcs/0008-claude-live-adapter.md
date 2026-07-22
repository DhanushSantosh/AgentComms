# RFC 0008: `claude-live` — a persistent, natively-attachable Claude worker adapter

## Status

Proposed — implementation plan for a builder agent; not yet built.

## Context

`opencode-live` (RFC-adjacent, see `internal/worker/adapter_opencode_live.go`)
gives an OpenCode-driven runtime genuine live visibility: one persistent
`opencode serve` instance stays alive across invocations, and any number of
`opencode attach` terminals watch the same session update in real time as a
completely separate process drives it.

Claude has no equivalent, and not for lack of trying. Investigated and ruled
out this session:

- **Claude Code Remote Control** (`claude remote-control`): real and native,
  but routes the session transcript through Anthropic's cloud relay, requires
  a claude.ai OAuth login (no API-key support), and — critically — is built
  around a human typing on one of three surfaces (local terminal, claude.ai
  web, mobile app). There is no documented way for *our own code* to inject a
  prompt into a Remote-Control-connected session; a program can't be the one
  "typing."
- **Agent Teams** (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`): genuinely
  native and fully local (teammates message each other through a JSON mailbox
  file), and split-pane mode gives real simultaneous live terminals. Ruled
  out because it's Claude-only (no non-Claude teammate, so it can't include
  an OpenCode-based agent) and requires one lead process to spawn every
  teammate up front — it has no notion of two independently pre-existing,
  independently registered, persistently-running agents like this project's
  `runtime register` model.
- **`claude-code-viewer`** (third-party, tails the session's JSONL file) and
  this project's own `agent-comms claude tail` (`internal/claudetail`, added
  this session, same tailing approach natively): both give read-only
  visibility into a session's history and new turns, but neither one can
  make an *already-open interactive* Claude terminal update, because the
  interactive TUI process never re-reads the file after it starts. Confirmed
  live: an interactive `claude --resume <id>` session sat open the entire
  time a separate headless worker process appended new turns to the same
  session file, and the open terminal never reflected any of it.

The missing piece all of the above lack is: **a single persistent Claude
process that our own code drives programmatically, with a live event stream
any number of viewers can subscribe to** — exactly opencode-live's shape, but
for Claude.

That primitive turns out to exist and was confirmed live this session, not
assumed:

```
claude --print --input-format stream-json --output-format stream-json \
  --permission-mode acceptEdits --dangerously-skip-permissions   # (skip-permissions only in the throwaway test below)
```

A Python test spawned this once, wrote one JSON user-turn line to stdin,
read the assistant's reply from stdout, then — **without restarting the
process** — wrote a second JSON user-turn line and got a reply that correctly
recalled state from the first turn (asked to remember "BANANA", then asked
"what was the word?", correctly replied "BANANA"). The process stayed alive
across both turns (confirmed via `proc.poll() is None`). This is a real,
structured, multi-turn, stdio-driven protocol — not the human-facing pty/TUI
that everything else in this space depends on.

## Decision

Build a new adapter, `claude-live`, structured exactly like `opencode-live`:
one persistent process per runtime that outlives any single invocation, an
HTTP+SSE broker of our own in front of it (since, unlike OpenCode, Claude
Code ships no server at all — we are building the equivalent from scratch),
and a real attach command that lets any number of terminals watch live.

### Package layout

New package: `internal/claudeserve` (name deliberately parallel to
`internal/opencodeclient`). Three files, mirroring that package's own
client.go / server.go split:

- **`process.go`** — wraps one long-lived `claude` subprocess.
- **`server.go`** — the HTTP+SSE broker: spawn-or-reuse-on-a-fixed-port,
  same shape as `opencodeclient.EnsureServer`.
- **`client.go`** — a small Go client other code uses to talk to *our own*
  broker (used by the new adapter to send prompts, and by the new `claude
  attach` command to subscribe to the live event stream).

New adapter: `internal/worker/adapter_claude_live.go` (+
`adapter_claude_live_test.go`), registered as `"claude-live"` in
`internal/worker/adapter.go`'s `adapters` map — a fifth adapter, alongside
`claude`, `codex`, `claude-acp`, `opencode-acp`, `codex-acp`, and
`opencode-live`. It does not replace `claude`; both stay available, the same
way `opencode-live` sits alongside `opencode-acp` without touching it.

New CLI command: extend `internal/app/app.go`'s existing `claudeCmd()`
(currently just `claude tail`) with two more subcommands:

- `agent-comms claude serve` — starts the broker in the foreground (the
  process a systemd unit or supervisor keeps running; this is the
  `claude-live` analogue of `opencode serve`). `EnsureServer` also auto-spawns
  this, detached, the same way `opencodeclient.spawnServer` does, so an
  operator does not have to run it by hand.
- `agent-comms claude attach --runtime <id> [--server <url>]` — connects to
  the broker's SSE stream for that runtime and renders turns live in a
  terminal. Reuse `claudetail.Format` for rendering — the JSON shape a
  `stream-json` process emits on stdout (`{"type":"assistant","message":
  {"content":[{"type":"text","text":"..."}]}}`) is close enough to the
  transcript-file shape `claudetail.Format` already parses that it should
  need no changes, or at most a small shared helper — verify this during
  implementation rather than assuming, and adjust `claudetail.Format`'s
  input type if the two shapes diverge in a way that needs reconciling.

### `process.go`: the persistent subprocess wrapper

```go
type Process struct { /* cmd, stdin io.WriteCloser, stdout scanner, subscribers, mu, ... */ }

func Start(ctx context.Context, config ProcessConfig) (*Process, error)
```

`ProcessConfig` carries everything the current `claudeAdapter.Arguments`
builds today: `WorkDir`, `PermissionMode`, `SystemPrompt` (reuse
`claudeSystemPrompt`), `AgentCommsPath` (for the `--allowedTools
Bash(...)` allow-rule), `Model`, `SessionID`. Build the argument list the
same way `claudeAdapter.Arguments` does, replacing `--print --output-format
text` with `--print --input-format stream-json --output-format stream-json`,
and reuse the **exact** create-vs-resume logic already built and tested in
`adapter_claude.go` (`claudeSessionExists` → `--session-id` on first boot,
`--resume` afterward) — this process needs the same self-bootstrapping
behavior a spawn-per-invocation adapter needs, just once per process
lifetime instead of once per invocation.

Responsibilities:

- **`Send(ctx context.Context, text string) (string, error)`**: writes
  `{"type":"user","message":{"role":"user","content":[{"type":"text",
  "text":text}]}}\n` to stdin, then reads stdout lines until a `{"type":
  "result",...}` line arrives, concatenating any `assistant` message's text
  blocks along the way as the returned output. **Single-flight**: reject or
  queue a second `Send` while one is already in progress for this process —
  the existing worker model already enforces one invocation at a time per
  runtime via leases, but this guarantee must also hold inside the broker
  itself, since the broker, unlike a spawn-per-invocation exec, is a shared
  resource that could otherwise be raced by anything else in the process.
- **Broadcast**: every parsed stdout line, in full (not just the result of
  one `Send`), goes out to every registered subscriber channel, so `claude
  attach` sees exactly what's happening in real time, not just the aggregated
  final answer `Send` returns.
- **Crash recovery**: if the process exits unexpectedly, mark it dead;
  the next `Send` (or the broker's own health check) respawns it with
  `--resume <session-id>` (session now definitely exists, so always resume,
  never re-create) and retries once. If the respawn also fails, return an
  error — do not silently swallow a dead process as an empty success, the
  same principle `acpResult` already enforces for the ACP adapters.
- **Verify, don't assume, during implementation**: whether `--permission-
  mode acceptEdits`/`auto`/`dontAsk` fully suppresses interactive approval
  prompts in `stream-json` mode the same way it does in one-shot `--print`
  mode, or whether stream-json mode surfaces permission requests as a
  distinct JSON event type that the broker must answer programmatically
  (there was no `control_request`-shaped line in the one confirmed test, but
  that test never attempted a governed action like Bash — re-test
  specifically with a Bash-triggering prompt before considering this
  adapter's governance story complete).

### `server.go`: the broker, and the orphan-port lesson

**This is the single most important lesson from this session to carry
forward, stated explicitly so it is not repeated**: `opencode-live`
originally spawned its persistent server on an OS-assigned port
(`--port 0`), and when its local cache file was later lost (a project
`.agent-comms` reset wiped it while the actual OS process kept running
detached), every subsequent invocation had no way to find that orphaned,
still-running, still-healthy server and silently spawned a *second* one on a
different random port — fragmenting all new invocation traffic onto a
duplicate a human's browser was never pointed at. The fix, already shipped:
bind a **fixed, well-known port**, and have `EnsureServer` **probe that fixed
port directly** whenever the cache file is missing or stale, not only trust
the cache file (`internal/opencodeclient/server.go`,
`resolveRunningServer`/`defaultServeBaseURL`).

`claude-live`'s broker must do the same from day one:

- Bind a fixed port, e.g. `4097` (chosen only to avoid colliding with
  OpenCode's `4096` — confirm nothing else on the target host already uses
  it, and make it configurable via a flag rather than hardcoded if that's
  cheap to do).
- `EnsureServer(ctx, projectRoot, workDir) (baseURL string, err error)`:
  check `.agent-comms/cache/claude-serve.json` (mirroring
  `opencodeclient.ServerInfoPath`'s exact convention and file format), health
  check it, and if that fails, **also directly probe the fixed port** before
  spawning a new broker process. Only spawn if neither responds.
- The broker itself is a small `net/http` server (this project already
  depends on nothing exotic for HTTP — see how `internal/mcp` and
  `internal/daemon` are built for the house style) exposing:
  - `POST /runtimes/{runtimeID}/prompt` — body `{"text": "..."}`, blocks
    until the turn completes, returns `{"output": "..."}`. Registers and
    starts a new `Process` for `runtimeID` on first use if none exists yet,
    using the `ProcessConfig` supplied in the request (or a prior
    registration call — decide which during implementation; a `POST
    /runtimes/{runtimeID}/register` call before first prompt, mirroring how
    `opencode-live` calls `CreateSession` once, is the cleaner shape).
  - `GET /runtimes/{runtimeID}/events` — Server-Sent Events, one event per
    raw JSON line the underlying process emits. Read-only: this endpoint
    must never accept input, keeping the same separation of concerns
    `opencode-live`'s deny-by-default governance already relies on (a viewer
    can watch, never act).
  - `GET /health` — plain liveness check, same convention as
    `opencodeclient.Client.Health`.
- Detached spawn: `agent-comms claude serve` run as a background/detached
  process the same way `opencodeclient.spawnServer` launches `opencode
  serve` (`Setsid`/`CREATE_NEW_PROCESS_GROUP`, OS-specific build-tagged
  files — copy `internal/opencodeclient/detach_unix.go` /
  `detach_windows.go` verbatim rather than reimplementing).

### `client.go`

A thin wrapper other code uses to call the broker: `New(baseURL)`,
`Prompt(ctx, runtimeID, text) (string, error)`, `Subscribe(ctx, runtimeID)
(<-chan Event, error)` for SSE. Model this directly on
`internal/opencodeclient/client.go` and `events.go`'s existing shape rather
than inventing new conventions.

### `internal/worker/adapter_claude_live.go`

```go
type claudeLiveAdapter struct{}

func (claudeLiveAdapter) Validate(config *Config) error {
    // identical body to claudeAdapter.Validate: permission-mode bypass
    // check, budget check (decide during implementation whether budget is
    // still meaningful as a per-invocation cap against a long-lived process
    // — it may need to become a cumulative session-level concept instead;
    // this is a genuine open design question, not a mechanical port).
}

func (claudeLiveAdapter) Execute(ctx context.Context, config Config, invocation model.Invocation) (string, error) {
    baseURL, err := claudeserve.EnsureServer(ctx, config.WorkDir, config.WorkDir)
    // register the runtime's process on first use (session create-or-resume,
    // same as opencode-live's CreateSession-or-GetSession branch)
    // report Status() with the attach command, not a bare URL:
    config.Status("watch this runtime's Claude activity live in a terminal: " +
        "agent-comms claude attach --runtime " + config.RuntimeID)
    client := claudeserve.New(baseURL)
    output, err := client.Prompt(ctx, config.RuntimeID, claudeUserPrompt(invocation))
    // reuse claudeUserPrompt/claudeSystemPrompt exactly as opencode-live does
    ...
}
```

Register it in `internal/worker/adapter.go`'s `adapters` map as
`"claude-live"`. `RequiresExecutable("claude-live")` should return `false`
automatically the same way it does for `opencode-live` (it doesn't implement
the `cliAdapter` sub-interface), but verify this rather than assume — add the
same kind of test `TestRequiresExecutableExcludesOpenCodeLive` has, adapted.

### Session continuity story

Exactly the fix already shipped for the plain `claude` adapter
(`claudeSessionExists`), reused rather than reinvented: first boot of a
runtime's process creates the session at a caller-chosen UUID with
`--session-id`; every later boot (including crash-recovery respawns) resumes
it with `--resume`. Because the process now stays alive across many
invocations instead of one, this only fires at process (re)start, not per
invocation — a meaningfully smaller surface for the "which flag do I use"
bug than the exec-per-invocation adapters have.

### CLI flags

`runtime worker --adapter claude-live` should accept the same flags the
plain `claude` adapter does today (`--session-id`,
`--claude-permission-mode`, `--claude-max-budget-usd`,
`--claude-allow-agent-comms`) and reject `--model` for now unless it turns
out to be easy to pass through to `Start` — match `opencode-live`'s current
`--model` rejection precedent (`errors.New("... does not yet support --model
overrides")`) if there's any doubt, rather than half-support it.

## Testing plan

- Unit tests for `claudeserve.process.go`'s stdout-line parsing and
  turn-boundary detection (`{"type":"result",...}`), driven against a fake
  script standing in for `claude` (this codebase's existing convention for
  testing exec-based adapters is to point `Executable` at
  `filepath.Abs(os.Args[0])`, i.e. the test binary itself, reading a
  controlled env var to decide what to print — follow that pattern, or use a
  small literal shell/Python fixture script if that ends up cleaner for
  multi-turn stdin/stdout interaction).
- Unit tests for `claudeserve.server.go`'s fixed-port-probe-before-spawn
  logic — directly port
  `TestResolveRunningServerPrefersCache`/`...FallsBackToDefaultPort`/
  `...ReportsNoneWhenNeitherResponds` from
  `internal/opencodeclient/server_test.go`; the shape of the fix is
  identical, only the package and default port differ.
- A manual smoke test gated behind `AGENTCOMMS_ACP_SMOKE=1` (matching every
  other adapter's `smoke_manual_*_test.go` convention in
  `internal/worker/`) that runs a real invocation through `claude-live`
  against the actual `claude` binary and confirms both (a) the invocation
  completes with real output, and (b) a second invocation on the same
  runtime correctly recalls context from the first (the same "remember
  BANANA" / "what was the word" pattern already proven live this session,
  as a permanent regression test rather than a one-off manual check).
- **I (the reviewing agent) will test this live once it's built**: real
  AXIOM-equivalent runtime on `claude-live`, a real invocation, and a real
  `agent-comms claude attach` terminal left open across it — confirmed the
  same way `opencode-live`'s live-update behavior was confirmed for FIXER,
  by sending a fresh invocation from a separate process while the attach
  terminal is already running and open, and checking the new turn appears
  without restarting anything.

## Documentation

Once built and verified, update the same three places every prior adapter
change in this project has touched, in the same style already established:

- `docs/agent-invocations.md`: add `claude-live` to the adapter comparison
  table (Live-viewable: **Yes** — `agent-comms claude attach`), and a
  `### claude-live: watching a runtime's Claude activity as it happens`
  subsection mirroring the existing `opencode-live` one, including the fixed
  port and the orphan-avoidance behavior.
- `CHANGELOG.md`: an `### Added` bullet for the adapter and the `claude
  serve`/`claude attach` commands.
- Rebuild and reinstall the CLI binary
  (`go build -o /tmp/agent-comms-rebuild ./cmd/agent-comms && mv
  /tmp/agent-comms-rebuild /home/dhanush/.local/bin/agent-comms` — `mv`, not
  `cp`, onto a binary that may currently be loaded by a running process) —
  this project has been bitten twice this session by testing against a
  stale installed binary; don't skip this step.

## Consequences

A `claude-live` runtime gains exactly what `opencode-live` already gives an
OpenCode runtime: one persistent process outliving any single invocation,
and a real terminal (`agent-comms claude attach`) that any number of people
can have open at once, showing genuinely live activity — not a file-tailer
working around the gap, not a cloud relay, not a feature that only fits a
different orchestration shape.

The plain `claude` adapter remains unchanged and the default. `claude-live`
is an additional, opt-in adapter for exactly the case where a human needs to
watch a Claude-driven runtime work as it happens, the same framing already
established for `opencode-live` relative to `opencode-acp`.

Open questions the builder should resolve rather than guess past: whether
`--claude-max-budget-usd` still means "per invocation" or becomes cumulative
per process lifetime; and whether `stream-json` mode's permission-prompt
behavior under `acceptEdits`/`auto`/`dontAsk` needs any handling beyond what
one-shot `--print` mode already does, specifically re-tested against an
action that actually requires approval (the one live test run this session
only exercised a plain text turn, never a governed tool call).
