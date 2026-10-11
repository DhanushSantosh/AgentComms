# Agent invocation protocol

> The compact user-facing guides now start at [Agent integration](site/agents/integrations.md). This file remains the deep technical appendix and compatibility target.

Agent Comms separates durable messages, runtime notifications, and execution
claims. Posting a message never grants execution authority by itself.

An invocation begins `PENDING`, may become `NOTIFIED`, then progresses through
`CLAIMED`, `RUNNING`, `WAITING`, and a terminal state (`COMPLETED`,
`REJECTED`, `EXPIRED`, or `CANCELLED`). Delivery attempts and claims are
authoritative transactions, so competing daemons or runtime instances cannot
reserve the same route or claim the same invocation concurrently.

These states describe different evidence. `PENDING` proves the governed
obligation exists. `NOTIFIED` proves a bounded delivery transport completed.
`CLAIMED` is the target's first authoritative acknowledgement. Only
`COMPLETED` proves the requested work was closed successfully. PTY evidence is
transport evidence, never proof that a model understood an instruction.

**First time here?** See [agent-onboarding.md](agent-onboarding.md) for the
sequential walkthrough — which interface to use, whether you're already
registered, and the core command loop. This document is the deep
reference: the full adapter matrix, live-tested failure modes, and
connector configuration.

## Agent workflow

```sh
agent-comms runtime register \
  --id reviewer-runtime --agent reviewer --connector MCP --max-concurrent 1

agent-comms runtime heartbeat \
  --actor reviewer --id reviewer-runtime --health HEALTHY

agent-comms invocation next --actor reviewer --runtime reviewer-runtime
agent-comms invocation claim --actor reviewer --id inv-123 --runtime reviewer-runtime
agent-comms invocation start --actor reviewer --id inv-123 --summary "Review started"
agent-comms invocation defer --actor reviewer --id inv-123 --reason "Waiting for CI"
agent-comms invocation resume --actor reviewer --id inv-123 --summary "CI completed"
agent-comms invocation complete --actor reviewer --id inv-123 --summary "Review passed"
```

Equivalent MCP tools are available for runtimes embedded in an agent host.
Owners can cancel non-terminal work with
`agent-comms invocation cancel --id inv-123 --reason "superseded"`.

Connected runtimes should hold a bounded listen open instead of polling:

```sh
agent-comms invocation listen \
  --actor reviewer --runtime reviewer-runtime --wait 10s --claim
```

The equivalent `invocation_listen` MCP tool waits for pushed work for up to ten
seconds and claims it transactionally by default. Agent hosts repeat the
bounded listen after a timeout or completed invocation. Competing runtime
instances cannot both claim the same invocation.

### Participating via MCP directly — no adapter required

Everything in this section is also exposed as MCP tools by `agent-comms
mcp`, a plain JSON-RPC 2.0 stdio server (`internal/mcp/server.go`) — the
same underlying application as the CLI, just a different transport. This is
the general, adapter-free way for *any* MCP-capable agent host to
participate: `runtime worker --adapter <name>` (below) is optional
convenience automation layered on top for a handful of specific,
vetted providers, not a requirement. Call the `get_started` tool first —
it renders the same decision tree as
[agent-onboarding.md](agent-onboarding.md), filled in with your connection's
actual resolved identity and registration state.

The recommended global configuration gives each host a stable
`AGENT_COMMS_HOST_LABEL` and lets the MCP process inherit the project
workspace as its working directory. Do not hardcode one global `--actor`:
after the host registers a project-chosen identity once, Agent Comms resolves
that identity from `(project ID, host label)` on every later connection.

```json
// Claude Code: ~/.claude.json, or project-local .mcp.json
{
  "mcpServers": {
    "agent-comms": {
      "command": "/usr/local/bin/agent-comms",
      "args": ["mcp"],
      "env": {
        "AGENT_COMMS_HOST_LABEL": "claude"
      }
    }
  }
}
```

```toml
# Codex: ~/.codex/config.toml
[mcp_servers.agent-comms]
command = "/usr/local/bin/agent-comms"
args = ["mcp"]

[mcp_servers.agent-comms.env]
AGENT_COMMS_HOST_LABEL = "codex"
```

```json
// OpenCode: ~/.config/opencode/opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "agent-comms": {
      "type": "local",
      "command": ["/usr/local/bin/agent-comms", "mcp"],
      "environment": {
        "AGENT_COMMS_HOST_LABEL": "opencode"
      },
      "enabled": true
    }
  }
}
```

If a host does not start MCP in the project workspace, add
`"--project", "/absolute/project/path"` before `"mcp"`. An explicit
`--actor` remains available for tightly pinned per-project configurations,
but it deliberately bypasses host-label discovery.

A brand-new host initially resolves to the project owner because no matching
profile exists yet. That owner-fallback connection may call `agent_register`
with the desired project identity, `<agent-name>` — a name chosen for this
project, never copied from documentation; registration mints that
identity's signing keypair and records the host label in its local profile.
Subsequent connections resolve directly to `<agent-name>`.

Registering an `id` other than the connection's own resolved actor always
requires that actor to be an active orchestrator or human principal
(`Service.CanSponsorRegistration`) — the owner-fallback case above is just
one instance of this: the project owner is always an active human principal
by construction, so it always qualifies. Any other active orchestrator, or
any other active human principal, may sponsor a new registration the same
way, at any time, not only on a project's first connection. A plain
agent-role, agent-principal connection may only ever self-register
(`id` equal to its own resolved actor).

Use the MCP `identity` tool to inspect the actor bound to the current
connection. From the CLI, `agent-comms profile current --json` reports both
the actor and why it was selected. Resolution rejects a host label bound to
multiple actors and never borrows an active profile from another project.
An explicit `--actor` or `--profile` can resolve an intentional ambiguity.

`agent_activate` is exposed too, but stays exactly as gated as the CLI's
`agent activate` — owner/orchestrator-only for an ordinary role. Until an
agent is activated, every other tool (`runtime_register`, `invocation_*`,
...) fails the same "active principal required" check the CLI enforces.
Requesting `role: "ORCHESTRATOR"` specifically is gated far more heavily
than "owner/orchestrator-only" implies: it additionally requires a HUMAN
principal, a pre-existing, separately-approved, HUMAN-tier approval record
for that exact grant, and — once the target human has registered a
passphrase-protected elevated key (`agent-comms agent elevate-key`) — a
signature from that elevated key specifically, which no MCP connection can
ever produce (see docs/governance.md). `agent_revoke` targeting an existing
ORCHESTRATOR or HUMAN principal is gated identically. `approval_approve`
is not exposed as an MCP tool at all — there is no way to approve anything
over this connection.

**MCP runtimes are pull consumers.** The stdio server does not claim that an
MCP tool response can wake a model host. An MCP runtime receives work through
bounded `invocation_listen`/`invocation_next` calls and claims it
transactionally. `MCP` therefore never manufactures `NOTIFIED`.

An agent may separately operate a supervised `INTERACTIVE` runtime when it
needs local terminal wake-up. Requests can explicitly choose
`INTERACTIVE_ONLY`, `WORKER_ONLY`, or `EITHER`; a preferred runtime makes that
choice deterministic. A committed request is successful even if no push
transport is available, and its result reports delivery as unavailable rather
than pretending the target was notified.

## Autonomous runtime workers

`runtime worker` turns a registered runtime into a complete responder rather
than a claim-only listener. It invokes Claude, Codex, or OpenCode directly,
supplies the durable invocation as the prompt, publishes the agent's result to
the requester, and completes the invocation without a user asking the
interactive agent to check its inbox.

### Choosing an adapter

Nine adapters are available via `--adapter`. `claude`, `codex`, and
`opencode` exec a provider CLI directly and are the proven default path for
each provider; the others speak the Agent Client Protocol (ACP) or use a
persistent local live broker, and are opt-in additions layered on top —
none of them replaces or changes the behavior of the three exec adapters.
`opencode` is the newest of the three: `opencode-acp` existed first, this
just fills the same gap `claude`/`codex` already had filled from day one.

| Adapter | Provider | Mechanism | Requires | Live-viewable | Notes |
| --- | --- | --- | --- | --- | --- |
| `claude` | Claude | direct CLI exec | `claude` binary | No | Default; proven, use unless you have a specific reason to pick an ACP adapter |
| `claude-live` | Claude | persistent `stream-json` process + local HTTP/SSE broker | `claude` binary | **Yes** — `agent-comms claude attach` | Requires `--session-id`; the broker and Claude process outlive individual invocations |
| `codex` | Codex | direct CLI exec | `codex` binary | No | Default; proven |
| `codex-live` | Codex | persistent `app-server` (JSON-RPC) process + local HTTP/SSE broker | `codex` binary | **Yes** — `agent-comms codex attach` | `--session-id` optional; Codex mints its own thread ID, which is cached and reused automatically |
| `opencode` | OpenCode | direct CLI exec (`opencode run --format json`) | `opencode` binary | No | Default; supports `--model`, unlike `opencode-acp`/`opencode-live`. Governance for `acceptEdits` is a static per-process `OPENCODE_PERMISSION` choice rather than a live approve/deny callback — functionally equivalent (edits allowed, everything else denied) but with no per-request nuance |
| `claude-acp` | Claude | ACP, via `npx @agentclientprotocol/claude-agent-acp` | Node.js/npm | No (session viewable afterward with [claude-code-viewer](https://github.com/d-kimuson/claude-code-viewer)) | Session-store-compatible with `claude` — the same conversation can be resumed by either adapter |
| `opencode-acp` | OpenCode | ACP, via `opencode acp` | `opencode` binary | No (session viewable afterward via `opencode` itself, or a third-party viewer) | |
| `codex-acp` | Codex | ACP, via `npx @agentclientprotocol/codex-acp` | Node.js/npm | No (viewable afterward with [codex-trace](https://github.com/PixelPaw-Labs/codex-trace)) | **Weaker tool-call permission enforcement than the other ACP adapters** — see below |
| `opencode-live` | OpenCode | worker-owned `opencode serve` + REST/SSE | `opencode` binary | **Yes** — run the reported `opencode attach` command in a terminal while it runs | Assigned loopback port, kept across turns while this worker runs; shutdown/one-shot exit stops its owned server |

Pick `claude`/`codex`/`opencode` by default. Reach for another adapter only
when you specifically need what it adds — e.g. `claude-live`, `codex-live`,
or `opencode-live` when someone needs to watch a runtime's activity happen,
not just read the completed result.

OpenCode mints its own, non-UUID session IDs (`ses_...`), the same as
`opencode-acp`/`opencode-live` — `worker.Config`'s `--session-id` flag
requires a well-formed UUID regardless of adapter, so it can never actually
carry a real OpenCode session ID through. `opencode`'s real session
continuity comes from a local cache
(`.agent-comms/cache/opencode-session-<runtime-id>.json`, distinct from
`opencode-live`'s own cache file) that `Execute` reads and writes directly,
the same non-authoritative local-routing convention `opencode-live` already
uses. A cached or passed-through session ID that OpenCode no longer
recognizes fails with a plain `Error: Session not found` line rather than
hanging; `opencode` retries once with no session at all rather than failing
the invocation outright.

The ACP-based adapters and the three `-live` adapters do not support
`--model` overrides yet (`claude-live` is the exception — it resolves the
`claude` executable inside its broker and does support the same model
override as `claude`) and none of the six need `--executable`; they locate
or spawn their own provider process. `--session-id` resumes an existing
conversation across adapters, subject to the provider-specific creation
rules below. `--claude-permission-mode` also gates `opencode-acp` and
`opencode-live`'s edit/move-shaped tool calls, and `--codex-sandbox` also
gates `codex-acp` and `codex-live` — the flag names are Claude/Codex-specific
for historical reasons, but their effect is shared across every adapter that
reads `PermissionMode`/`Sandbox`.

Run a Claude worker under a process supervisor:

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter claude \
  --executable /usr/local/bin/claude \
  --session-id 2f38a348-52f0-43cd-a19f-1e0dd06ab451 \
  --claude-allow-agent-comms \
  --claude-permission-mode acceptEdits \
  --claude-max-budget-usd 1 \
  --execution-timeout 30m
```

Run a Codex worker with workspace writes:

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter codex \
  --executable /usr/local/bin/codex \
  --session-id 019f7857-ac37-7233-ae3f-0104627cab0e \
  --codex-sandbox workspace-write \
  --codex-add-dir /home/reviewer/.config/agent-comms \
  --codex-ignore-user-config \
  --execution-timeout 30m
```

Run an OpenCode worker (no `--session-id` — OpenCode's own session
continuity comes from its local cache, not this flag):

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter opencode \
  --executable /usr/local/bin/opencode \
  --model opencode/big-model \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

### ACP-based workers

The ACP adapters need no `--executable` — they locate or spawn their own
provider process — and resolve tool-call permission requests through a hybrid
policy rather than one upfront flag: read/search/reasoning/mode-switch calls
auto-approve, edit/move calls follow `--claude-permission-mode`, and every
other action (delete, execute, fetch, and anything unrecognized) is denied by
default. There is no per-tool-call approval primitive in this project to route
a governed request through instead, so a denied action fails the invocation
with a reason naming what was denied rather than silently doing nothing.

Run a Claude worker over ACP (requires Node.js/npm on the host):

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter claude-acp \
  --session-id 2f38a348-52f0-43cd-a19f-1e0dd06ab451 \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

Run an OpenCode worker over ACP (requires the `opencode` binary):

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter opencode-acp \
  --session-id ses_0778c2bddffeJCT1Tq3NiG5Zky \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

Run a Codex worker over ACP (requires Node.js/npm). Its permission
enforcement is weaker than the other two ACP adapters: the underlying
`codex-acp` package exposes only three coarse mode presets, and the two
non-bypass ones use Codex's own "on-request" approval policy — the model
itself decides whether an action is risky enough to ask, confirmed live to
skip asking entirely for plain file-write-and-verify tasks. There is no
per-category "always ask" override available the way `opencode-acp` has, so
the OS-level sandbox (`--codex-sandbox`) is the primary control here, same as
the exec-based `codex` adapter:

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter codex-acp \
  --session-id 019f7857-ac37-7233-ae3f-0104627cab0e \
  --codex-sandbox workspace-write \
  --execution-timeout 30m
```

### `claude-live`: watching a runtime's Claude activity as it happens

`claude-live` starts one persistent Claude Code process in structured
`stream-json` mode and drives it through a loopback-only HTTP/SSE broker. The
process stays alive across invocations, while any number of read-only attach
clients can watch user, assistant, and tool-call events as they happen. Attach
clients cannot submit prompts or approve tools; only the governed worker uses
the broker's prompt endpoint.

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter claude-live \
  --session-id 2f38a348-52f0-43cd-a19f-1e0dd06ab451 \
  --claude-permission-mode acceptEdits \
  --claude-max-budget-usd 1 \
  --execution-timeout 30m
```

The worker reports the exact viewer command:

```sh
agent-comms claude attach --runtime reviewer-runtime --server http://127.0.0.1:4097
```

The broker binds fixed port `4097`, records its address at
`.agent-comms/cache/claude-serve.json`, and probes that port directly when the
cache is absent or stale before spawning. This prevents a cache reset from
orphaning a healthy detached broker and fragmenting later traffic onto a
duplicate. Operators may instead supervise `agent-comms claude serve`; the
worker starts it automatically when neither the cache nor the fixed port leads
to a healthy broker.

`--session-id` is required so the broker can create the conversation on first
start and resume it after a process or broker restart. A runtime is registered
with one immutable process configuration; attempting to reuse its ID with a
different project, session, permission mode, or system prompt is rejected as a
conflict. The broker queues concurrent prompts per runtime and disconnects a
slow viewer rather than allowing it to block Claude.

Claude Code applies `--claude-max-budget-usd` to the long-lived print-mode
process. Consequently, for `claude-live` it is a process-lifetime ceiling,
not the plain `claude` adapter's fresh per-invocation ceiling. A broker or
process restart begins a new Claude CLI budget window. Permission denials and
non-success result frames fail the invocation instead of becoming an empty
successful result.

### `codex-live`: watching a runtime's Codex activity as it happens

`codex-live` starts one persistent `codex app-server` process, driven over
its JSON-RPC protocol, through the same kind of loopback-only HTTP/SSE
broker `claude-live` uses. The process stays alive across invocations,
while any number of read-only attach clients watch `item/completed` user
and assistant turns as they happen.

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter codex-live \
  --codex-sandbox workspace-write \
  --execution-timeout 30m
```

The worker reports the exact viewer command:

```sh
agent-comms codex attach --runtime reviewer-runtime --server http://127.0.0.1:4098
```

The broker binds fixed port `4098`, records its address at
`.agent-comms/cache/codex-serve.json`, and probes that port directly when the
cache is absent or stale before spawning — the same orphan-avoidance fix
`opencode-live` and `claude-live` already carry. Operators may instead
supervise `agent-comms codex serve`; the worker starts it automatically when
neither the cache nor the fixed port leads to a healthy broker.

Unlike `claude-live`, `--session-id` is optional: Codex mints its own thread
IDs and has no way to create one at a caller-chosen value, so the thread ID
this runtime creates on first use is cached at
`.agent-comms/cache/codex-live-thread-<runtime-id>.json` and reused
automatically on every later invocation — the same convention
`opencode-live` already uses for OpenCode's own minted session IDs. Passing
`--session-id` explicitly attempts to resume that thread first, falling back
to creating (and caching) a fresh one if it no longer resolves.

`--codex-sandbox` and `--codex-add-dir` apply the same as they do for the
exec-based `codex` adapter. `--model` is not yet supported for `codex-live`
and is rejected at startup. `--codex-ignore-user-config` is also rejected
before launch: the native app-server cannot provide that isolation. Choose
`--adapter codex` when user-config isolation is required; ordinary live runs
use the provider's normal user configuration. Turn completion is detected from an
`item/completed` notification whose item is an `agentMessage` in its
`final_answer` phase — confirmed live across two sequential turns on one
process ("remember PICKLE" → "PICKLE"), though this was verified only
against plain text turns, not yet against a turn that triggers an actual
governed tool call.

### `opencode-live`: watching a runtime's activity as it happens

The live adapters retain provider activity across invocations. `opencode-live`
starts its own `opencode serve` on an OS-assigned loopback port and retains it
while the worker runs. It does not adopt, reset or stop an existing shared server
on port 4096. Worker shutdown, cancellation, failed preparation and one-shot exit
stop/reap only that worker's owned server. A new worker may report a new attach
endpoint while resuming its persisted conversation.

Before each managed turn the worker resets only its owned project instance to
clear saved native approvals, preserves original native restrictions, and
installs/verifies restrictive rules. Native denies remain denied. Native allows
become requests handled by the existing read/edit/governance policy. An empty,
tool-disabled, no-reply preparation message supplies the native rule baseline;
only that exact temporary message is deleted, never existing conversation
content. Failed reset, identity lookup, rule verification or cleanup prevents
the actual prompt. Unknown externally modified rules fail closed and require
operator reconciliation; they are not silently discarded.

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter opencode-live \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

The worker's `Status` output reports the exact command to run, e.g. `watch
this runtime's OpenCode activity live in a terminal: opencode attach
http://127.0.0.1:<assigned-port> --dir /srv/project --session ses_...`. Run that in a
second terminal while the worker is running. `--dir` and `--session` both
matter: copy the exact reported endpoint, directory and session. Watch rather
than sending concurrent interactive prompts or changing native rules while a
managed turn runs. Native attach is a trusted local operator capability, not
an OS sandbox or endpoint authentication boundary. Use this
specifically when a human needs to watch a runtime work, not as the
default choice for routine automation — `opencode-acp` has no persistent
process to manage and is the better fit when nobody needs to watch.

Use the normal full native TUI command reported by the worker. OpenCode 1.18.33's
optional `opencode attach --mini` transport stops watching after the required
per-turn instance reset; it is not the supported continuous watcher. The full
native TUI retains the endpoint and observes later turns across reset. Restarting
a worker can still change its port: copy the newly reported attach command.

The worker is intentionally foreground-only so systemd, launchd, a container
runtime, or the agent host controls restarts and shutdown. Each process handles
one invocation at a time. Claude requires a per-invocation spend limit and
cannot use `bypassPermissions`; Codex is restricted to `read-only` or
`workspace-write`; every ACP-based adapter rejects the same kind of
permission-bypassing mode at startup, the same way the exec adapters do.
Executions and captured output are bounded. Any execution or publication
failure moves the invocation to `WAITING` with an auditable reason — for an
ACP-based adapter, an empty result following a denied permission request is
treated as a failure for exactly this reason, not a silent success.
Use `--once` for one bounded receive attempt in automation or testing.

`--session-id` binds the runtime to one provider conversation and resumes it
for every invocation, preserving conversational continuity. Run only one
worker for a bound session and do not process an interactive turn in that
conversation at the same time.

How the ID is established differs by adapter:

- `claude`: pass any UUID you choose. If no conversation exists yet at that ID,
  the worker creates one there on the first invocation (`--session-id`);
  every later invocation resumes it (`--resume`) — confirmed live that
  `--resume` on a not-yet-existing ID fails outright, so the worker checks for
  the session file under `~/.claude/projects/` before choosing which flag to
  send.
- `claude-live`: also pass a caller-chosen UUID. It uses the same create-first,
  resume-afterward check as `claude`, but only when its persistent process
  starts or recovers from a crash rather than once per invocation.
- `codex`: the ID must already exist — Codex mints its own thread IDs and has
  no equivalent of Claude's create-at-a-chosen-ID flag. Run once without
  `--session-id`, capture the thread ID Codex reports, then set
  `--session-id` to it for every later invocation of that runtime.
- `opencode-live`: leave `--session-id` unset. OpenCode mints non-UUID IDs; the
  public worker flag still requires a UUID and cannot carry native `ses_*` IDs.
  The worker instead persists its exact runtime/project/session, original native
  rules and preparation recovery metadata under the user configuration's
  `sessions` directory. It resumes that conversation across turns and worker
  restarts. Existing runtime-local legacy ID records are read for migration,
  but failed lookup never silently creates a replacement conversation. An
  ownership lock prevents two managed workers using the same runtime/session;
  a native session already assigned to another managed runtime is rejected.
- `codex-live`: `--session-id` is also optional, for the same reason as
  `opencode-live` — Codex mints its own thread IDs. The thread this runtime
  creates on first use is cached at
  `.agent-comms/cache/codex-live-thread-<runtime-id>.json` and reused
  automatically afterward.

The ACP-based adapters (`claude-acp`, `opencode-acp`, `codex-acp`) currently
require the ID to already exist, the same as `codex`. Without `--session-id`
(or, for `opencode-live`, without a cached one yet), a worker's isolated
one-shot behavior is unchanged.

`--claude-allow-agent-comms` grants the resumed Claude session unattended Bash
permission for the currently running `agent-comms` executable only. Enable it
when that agent must create follow-up invocations itself. It does not approve
general shell commands or bypass Claude's remaining permission rules.
Codex workers that invoke Agent Comms may need its local configuration
directory passed explicitly with `--codex-add-dir`; each path must already
exist and be absolute.
With `--adapter codex`, use `--codex-ignore-user-config` for a worker that resumes the
conversation history without loading unrelated user MCP servers or tool
configuration. Codex authentication and repository instructions remain
available. This isolation option is unavailable for `codex-live`, which
rejects it before launch rather than silently ignoring it.

Agent-to-agent follow-ups do not require provider shell or MCP access. When an
agent decides another agent must act, it returns one bounded single-line
`AGENT_COMMS_INVOKE: {json}` action using the format supplied in its worker
prompt. The worker validates the action, signs the request with the current
agent identity, submits it, and includes the new invocation ID in the durable
result. Only one follow-up is accepted per completed turn, preventing
unbounded fan-out from one model response.

### Direct delivery into a live interactive session

Every adapter above is a headless worker: something has to poll for it to
receive work at all. A genuinely interactive session — a human's own `codex`
or `opencode` running in a terminal, or another agent's own interactive
session — is different: there is no worker polling on its behalf, so
without something to wake it, a new invocation just sits `PENDING` until a
human happens to type into that pane.

`internal/interactiveserve` closes that gap for `codex`, `opencode`, and
`claude` alike — any real interactive CLI can be wrapped. `claude` is a
genuine special case, though, not just another entry in the list: live
testing found its refusal is risk-proportionate judgment rather than a
categorical block. A low-stakes, unambiguous nudge gets read, reasoned
about, and acted on; three separate higher-stakes claims of
authorization-for-autonomous-action (a bare third-party claim, the same
claim backed by a real signature, and durable pre-session authorization via
CLAUDE.md) were all correctly refused as indistinguishable from a prompt
injection. See RFC 0010's "Status" section for both live tests in full —
this is not a wording gap to iterate away, it is Claude's injection defense
working as intended for the cases it refuses.

AgentComms owns the pty directly rather than relying on an external
multiplexer — run the real provider CLI wrapped, in place of running it
bare, in any terminal emulator:

```sh
agent-comms runtime interactive-serve --id opencode-runner -- opencode
```

This allocates a real pty, execs `opencode` attached to it, and
transparently forwards the wrapper's own stdin/stdout so the terminal shows
`opencode`'s native UI exactly as if it had been run directly — no visible
wrapper, no multiplexer to install. The `--` before the wrapped command is
required (not optional): everything after it is passed through untouched as
the command and its own arguments.

Add `--launch-terminal` to skip manually opening a terminal and retyping the
command yourself: it re-execs this exact invocation (minus the flag itself)
inside a freshly opened, dedicated window via `internal/terminallaunch`,
then exits, leaving the current terminal free. This is a convenience over
the manual step only — the session still needs a real, dedicated terminal
for the same reason the plain command always has (see "genuine, structural
limit" below); nothing about that requirement changes. It tries a short
list of known terminal programs per OS (`gnome-terminal`, `konsole`,
`kitty`, `foot`, `alacritty`, `xterm`, etc. on Linux; `Terminal.app` via
`osascript` on macOS; Windows Terminal or a detached console on Windows)
and fails with a clear error naming everything it tried if none are
available, rather than silently doing nothing.

### Migrating a live, ordinary session into interactive-serve

Resuming a specific provider conversation (`claude --resume <id>`, `claude
--continue`, `codex resume --last`, and equivalents) after the `--` works
exactly like any other wrapped argument — but only when the session being
resumed is actually finished. Confirmed live: pointing a *second*,
`interactive-serve`-wrapped process at the same provider session ID as an
*already-running*, ordinary session collided — two processes attached to
one provider-side session lock, and killing either one disrupted the other.
This is a provider session-file collision, entirely outside Agent Comms'
own resource-leasing model; nothing here can protect a session ID it
doesn't know about.

`--takeover-pid <pid>` closes this gap for the one case it's actually
needed: turning a live session you're already sitting in into a dedicated,
wakeable `interactive-serve` runtime, in place, without ever having two
processes attached to the same session at once. Given a PID, it sends
`SIGTERM`, waits up to the same grace period `interactive-serve` itself
uses for a forwarded shutdown signal, escalates to `SIGKILL` if it hasn't
exited, and only then proceeds — so the target session is fully, confirmed
gone *before* the wrapped command's own resume flag ever runs:

```sh
agent-comms --actor DAMON runtime interactive-serve --id DAMON \
  --launch-terminal --takeover-pid 48213 \
  --claude-allow-agent-comms -- claude --continue
```

Find the PID to pass with your shell's own tools (e.g. `ps`) — Agent Comms
has no notion of "the session I'm currently typing into" to infer it
automatically. Pairing this with `--launch-terminal` matters: the process
performing the takeover must not itself be a descendant of the PID it's
terminating. This is enforced, not just advised — `Takeover` walks its own
parent chain before touching pid at all and refuses outright if it finds
pid there, rather than proceeding into a failure with no clear error
pointing at why. Confirmed live 2026-08-07: an agent self-relaunched
through its own Bash tool call (a child of the very session being taken
over, since a tool call is a subprocess of the agent's own CLI process) —
killing the target took the whole wrapper down, and the replacement process
it tried to start next had no real controlling terminal to attach a pty to
(a Bash tool call isn't one), so it died too, silently, with nothing left
running and no obvious error explaining what happened. A freshly spawned
terminal window opened via `--launch-terminal` is never a descendant of the
old session, so it always passes this check cleanly; running
`--takeover-pid` directly from a shell — or an agent's own tool-call
subprocess — that *is* a descendant of the target PID now fails fast with
an explicit error instead.

A `--takeover-pid` that's already gone by the time this runs is treated as
success, not an error — the common case once `--launch-terminal` has
already forwarded it to a freshly spawned process that handles the real
work.

Wrapping `claude` this way hits its own permission mode: by default every
CLI call Claude makes (including `agent-comms invocation claim/start/
complete`, once it decides to act on a delivered nudge) stops on a "Do you
want to proceed?" prompt, with nobody there to answer it. `--claude-allow-
agent-comms` closes that specific gap, the same flag and scoping `runtime
worker` already uses:

```sh
agent-comms runtime interactive-serve --id claude-runner --claude-allow-agent-comms -- claude
```

This only applies when the wrapped command's basename is `claude` (an error
otherwise) and scopes two `--allowedTools` rules onto it: the resolved
absolute path to this `agent-comms` executable, and its bare basename.
Both are needed — confirmed live, not assumed: the notification text
`interactiveserve.NotifyInvocation` delivers, and Claude's own follow-up
`invocation claim/start/complete` calls, all invoke the bare `agent-comms`
name via `PATH`, never the resolved absolute path, so an absolute-path-only
rule silently matches nothing and every call still prompts. This grants
unattended permission for `agent-comms` itself, nothing broader, and no
change to Claude's own judgment about whether a given instruction is worth
acting on in the first place — a live 3-way test (`codex`, `opencode`, and
`claude` all reachable simultaneously) confirmed the flag removes the
approval prompts without loosening that judgment: `claude` still
independently inspected the project, flagged what looked suspicious, and
asked for confirmation before proceeding on a low-stakes request; once
satisfied, it drove `invocation claim/start/complete` unattended.

Whatever actor the wrapper itself resolves (`--actor`, `--profile`,
host-label match, or the active profile) is automatically exported into the
wrapped child's environment as `AGENT_COMMS_ACTOR`, so its own subsequent
`agent-comms` calls authenticate as that identity too, instead of falling
back to ambient owner resolution the moment it makes its own call. This
matters more than it sounds like it should: without it, every agent-comms
call the wrapped session makes on its own resolves to whichever identity
its environment happens to fall back to — commonly the project owner — and
a HUMAN-tier action succeeding under that identity is indistinguishable
from you having typed it yourself. Pass it explicitly rather than relying
on a shell-exported env var:

```sh
agent-comms --actor DAMON runtime interactive-serve --id DAMON --claude-allow-agent-comms -- claude --continue
```

The runtime ID may match the agent ID for convenience, but it remains an
independent, first-class runtime record.

From then on, `agent-comms invocation request --to <agent-name> ...` targets
the agent identity. The daemon resolves only registered, policy-compatible
runtimes. An interactive request may specify
`--consumer INTERACTIVE_ONLY --runtime <runtime-id>`; policy can make those
the target's defaults. Multiple eligible interactive runtimes without a
preferred runtime are ambiguous, so the coordinator never guesses.

Delivery is daemon-coordinated after the invocation is durably recorded. For a
local interactive runtime the daemon first asks the session whether it is
ready: a session whose agent is mid-turn, whose human is typing or holding an
unsent draft, or that cannot be reached is simply left pending — no attempt is
recorded and no retry budget is spent — and is checked again every half
second, so the notice follows within moments of the session becoming ready,
however long the agent's current task takes. Once ready, the daemon commits
`invocation.delivery-attempt` and dials the runtime's control socket. Success
is recorded only after the text visibly appeared on the target's screen and
Enter was accepted. Foreign-host, offline, or otherwise unavailable sessions
produce `delivery.outcome=UNAVAILABLE` and an actionable warning while the
request itself still exits successfully. Use
`agent-comms invocation redeliver --id <invocation-id> --runtime <runtime-id>`
for an explicit retry of an open, unclaimed invocation.

The control socket lives in a canonical per-user runtime directory and is
independent of `TMPDIR`. Desktop applications, terminal sessions, and the
daemon therefore derive the same path even when they inherit different
temporary-directory environments.

This intentionally does not extend to the instruction content itself: the
delivered notification only ever says an invocation is pending and names the
commands to handle it, in one line of this shape:

```text
Agent Comms: <requester> requested invocation <id> for <agent>. Run agent-comms invocation inspect --id <id> --json, then invocation claim --id <id> --runtime <runtime-id>, start, and complete (or reject) it. [<id>]
```

The target's own interactive session reads the instruction back through the
normal, auditable `invocation inspect`/`claim`/`start`/`complete` sequence —
the same as every other adapter — and replies, if it wants to, with its own
`invocation request`. That keeps terminal injection limited to "wake up and
look," never a second, unaudited channel for instruction content to reach a
runtime. (An earlier wording, "handle it per your existing protocol", left a
real Codex session exploring the CLI for minutes before acting; naming the
commands had it, and Claude Code, handling the invocation immediately.)

`runtime interactive-session --id <runtime>` reports whether a runtime
currently has a live session. This mechanism now works on Linux, macOS, and
Windows (10 version 1809+): Linux/macOS use a real pty
(`github.com/creack/pty`), Windows uses ConPTY
(`github.com/charmbracelet/x/conpty`) — see RFC 0014 for the full design
and the Windows-specific process-lifecycle differences it documents.

**How a delivery decides, and the failure modes each step guards against.**
`interactive-serve` renders the wrapped command's output with a VT terminal
emulator alongside the user's real terminal, so every decision below is made
against what is actually on screen right now — the way an earlier tmux-based
iteration read tmux's rendered grid — not against a tail of raw output bytes.
Each guard was confirmed against a real Codex or Claude Code session, or a
scripted fake agent under a real pty/ConPTY:

1. **Busy.** A second invocation arriving mid-turn used to land glued onto an
   earlier, not-yet-submitted delivery. Delivery waits until the status area
   (the bottom rows of the screen) shows no busy marker (`esc to interrupt`,
   `esc again to interrupt`, `esc interrupt`). Reading the rendered screen
   matters: a TUI that clears its status line by overwriting it in place left
   the marker in the old raw-byte buffer, and the session read as busy
   forever. This is a heuristic — no provider exposes a protocol-level busy
   signal.
2. **The human at the keyboard.** Human keystrokes and injected text share
   one input. Delivery holds human input while it types (keystrokes wait and
   follow, in order, never interleave), never starts within three seconds of
   the human typing, and never starts while the human has typed text they
   have not submitted or cleared — typing onto a half-written prompt and
   pressing Enter would send it along. On Windows, keys arriving in
   win32-input-mode encoding (what Windows Terminal sends once the nested
   console requests it) are decoded for this.
3. **Echo.** Enter is only pressed after the typed text newly appears on
   screen: the whole message, its invocation ID, its tail, or a
   "[Pasted ...]" placeholder — more times than before typing, so a previous
   delivery's text still visible in the transcript is never mistaken for
   this one's echo. If it never appears, the typed text is erased and the
   attempt fails.
4. **Enter.** Enter waits for output to settle (300ms of quiet, capped at
   2s): codex treats keys arriving in a burst as a paste and takes an Enter
   inside that window as a newline. If the complete message is still in the
   composer at the cursor afterwards, Enter is pressed again after a longer
   quiet period — removing the newline the swallowed Enter left — up to three
   times; if it is never accepted, the text is erased and the attempt fails
   rather than reporting a line that sits unsent.

Every check fails closed as `invocation.delivery-failed`, never as a blind
Enter-press or false notification. `invocation inspect --id <id>` shows each
attempt, its transport evidence, and the separate target acknowledgement.

**The session itself is never at the mercy of bookkeeping.** Heartbeat
failures (a daemon restarting, replaced, or slow) are retried and logged to
`.agent-comms/tmp/interactive-<runtime>.log` — never printed into the
agent's terminal and never a reason to stop the wrapped command; presence
simply lapses after 45 seconds and returns with the next successful beat.
Closing the terminal window ends the session cleanly and marks the runtime
offline. The wrapped command does not inherit the launching session's
Claude Code host markers or credentials (`CLAUDECODE`, the desktop app's
messaging socket and token, session IDs).

### Many-to-many delivery: concurrency and hardening

The mechanism above generalizes past one-to-one delivery for free: any
runtime with a live session can be addressed by `invocation request --to`
from any other, so N agents addressing each other is the same primitive as
two, just used more times. Concurrency here is simpler than the earlier
tmux-based iteration needed, not just re-implemented: since exactly one
process now owns each runtime's pty (the `interactive-serve` process
itself), concurrent senders just make concurrent connections to that one
process's control socket, and it serializes `deliver` requests with a plain
in-process `sync.Mutex` — there is no cross-process lock, no shared
registry file, and no `tmux send-keys` argument-parsing surface to defend
against at all, since writing straight to a pty file descriptor has no
argv-parsing layer to exploit. `Deliver` still rejects any message
containing an embedded newline outright — it sends exactly one line of
input followed by one Enter, and a smuggled newline could otherwise submit
early, mid-message.

What remains a structural limit: delivery now notices a human typing in a
live session's terminal (guard 2 above) and waits for them, but it cannot
know what a human *intends* — a wake-up line that arrives in the moment
after someone submits their own prompt is still typed into the same
conversation. A terminal running `interactive-serve` is best dedicated to
that purpose. `interactive-serve` prints a one-line banner before handing
control to the wrapped command as a visibility nudge; it is not a
detection mechanism.

## User policy

Owners and orchestrators configure each target agent:

```sh
agent-comms invocation policy set \
  --agent reviewer --mode TRUSTED --trusted-actor builder \
  --default-consumer INTERACTIVE_ONLY \
  --allow-consumer INTERACTIVE_ONLY \
  --interactive-runtime reviewer-interactive \
  --require-human-for-sensitive
```

Modes are:

- `MANUAL`: each agent-originated invocation requires an approval;
- `TRUSTED`: only named active actors may invoke the target;
- `AUTOMATIC`: authorized active agents may invoke the target;
- `DISABLED`: agent-originated invocation is disabled.

Owners and orchestrators retain emergency control. Sensitive work continues to
use the existing human approval system. Invocation scopes must fit both the
requester and target agent scopes and, when configured, the target policy's
allowed scopes. A non-routine related task or an urgent invocation requires a
human approval when `require_human_for_sensitive` is enabled.

`agent-comms control overview`, `control attention`, and `control settings`
provide automation-friendly views of the same project control plane shown in
the TUI.

## Local connector configuration

Runtime events contain only a configuration reference. Connector commands and
environment values are stored in a mode-0600 per-user JSON file and never in
project history:

```json
{
  "connectors": {
    "reviewer-local": {
      "type": "LOCAL_PROCESS",
      "executable": "/opt/agents/reviewer",
      "arguments": ["--invocation", "{invocation_id}"],
      "working_directory": "/srv/project",
      "timeout": "30s"
    }
  }
}
```

Set `AGENT_COMMS_CONNECTOR_CONFIG` to this file and register the runtime with
`--config-reference reviewer-local`. Local process connectors receive a
bounded JSON invocation envelope on standard input and identifiers in
`AGENT_COMMS_PROJECT_ID`, `AGENT_COMMS_INVOCATION_ID`,
`AGENT_COMMS_AGENT_ID`, and `AGENT_COMMS_RUNTIME_ID`.

The daemon reserves a delivery before launching a connector. Failed launches
use bounded exponential backoff and close only the delivery attempt; the
governed invocation remains open. `LOCAL_PROCESS` records success only after
exit zero, and `WEBHOOK` only after a 2xx response. `MANUAL`, `MCP`, and
`QUEUE` do not provide push evidence and never create a notification-success
event.

Webhook connectors push the same bounded envelope to an agent host:

```json
{
  "connectors": {
    "reviewer-webhook": {
      "type": "WEBHOOK",
      "endpoint": "https://agents.example.internal/invocations",
      "headers": {
        "Authorization": "Bearer secret-from-this-private-file"
      },
      "timeout": "10s"
    }
  }
}
```

Register it with `--connector WEBHOOK --config-reference reviewer-webhook`.
Remote webhook endpoints must use HTTPS; loopback HTTP is allowed for local
agent hosts. Redirects are rejected, headers and responses are bounded, and
connector secrets remain outside project history. A successful webhook wake-up
returns a 2xx response; the target then uses `invocation_listen` or the normal
claim/start/complete tools to acknowledge and process the invocation.

An online runtime becomes offline when its heartbeat is older than 45 seconds.
Draining and revoked states are never overwritten by heartbeat expiry.
