# Agent invocation protocol

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
agent-comms invocation wait --actor reviewer --id inv-123 --reason "Waiting for CI"
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
| `opencode-live` | OpenCode | persistent `opencode serve` + REST/SSE | `opencode` binary | **Yes** — run the reported `opencode attach` command in a terminal while it runs | The server it starts outlives the invocation and is reused by later ones; every other adapter's process ends with the invocation |

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
and is rejected at startup. Turn completion is detected from an
`item/completed` notification whose item is an `agentMessage` in its
`final_answer` phase — confirmed live across two sequential turns on one
process ("remember PICKLE" → "PICKLE"), though this was verified only
against plain text turns, not yet against a turn that triggers an actual
governed tool call.

### `opencode-live`: watching a runtime's activity as it happens

Every adapter above runs its provider process only for the duration of one
invocation. `opencode-live` is the exception: it starts a persistent
`opencode serve` instance the first time it's needed, records its address at
`.agent-comms/cache/opencode-server.json`, and reuses that same instance for
every later invocation on this runtime — that persistence is what lets a
terminal stay attached to one session and watch activity happen live, instead
of only being able to read a result once an invocation completes. This
persistent server always binds a fixed port (4096), not an OS-assigned one,
and every invocation also probes that port directly before spawning a new
instance — even with the cache file missing entirely, a server already
running there is found and reused rather than orphaned behind a duplicate on
a different port.

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter opencode-live \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

The worker's `Status` output reports the exact command to run, e.g. `watch
this runtime's OpenCode activity live in a terminal: opencode attach
http://127.0.0.1:4096 --dir /srv/project --session ses_...`. Run that in a
second terminal while the worker is running. `--dir` and `--session` both
matter: attaching with only the bare server URL lands on whatever session
happened to be "current" on the server rather than this runtime's own —
confirmed live, since a long-lived server ends up handling many unrelated
sessions across however many projects and runtimes have used it over time.
This was verified live: a session titled with the invocation's own
instruction shows up in that server's own session list and is fully
watchable with `opencode attach` while the invocation runs. Use this
specifically when a human needs to watch a runtime work, not as the
default choice for routine automation — `opencode-acp` has no persistent
process to manage and is the better fit when nobody needs to watch.

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
- `opencode-live`: `--session-id` is optional. OpenCode also mints its own
  session IDs, but the worker persists whichever one it creates at
  `.agent-comms/cache/opencode-live-session-<runtime-id>.json` and reuses it
  automatically on every later invocation of that runtime — no flag or manual
  ID capture needed. Pass `--session-id` explicitly only to point the runtime
  at a specific pre-existing OpenCode session instead of its own cached one.
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
Use `--codex-ignore-user-config` for a deterministic worker that resumes the
conversation history without loading unrelated user MCP servers or tool
configuration. Codex authentication and repository instructions remain
available.

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

Delivery is daemon-coordinated after the invocation is durably recorded. It
first commits `invocation.delivery-attempt`, then dials the selected runtime's
control socket. Success is recorded only after the PTY echoes the complete text
and accepts Enter. Busy, foreign-host, offline, or otherwise unavailable
sessions produce `delivery.outcome=UNAVAILABLE` and an actionable warning
while the request itself still exits successfully. Use
`agent-comms invocation redeliver --id <invocation-id> --runtime <runtime-id>`
for an explicit retry of an open, unclaimed invocation.

This intentionally does not extend to the instruction content itself: the
delivered notification only ever says an invocation is pending and how to
look it up. The target's own interactive session is expected to read the
instruction back through the normal, auditable
`invocation list`/`claim`/`start`/`complete` sequence — the same as every
other adapter — and reply, if it wants to, with its own
`invocation request`. That keeps terminal injection limited to "wake up and
look," never a second, unaudited channel for instruction content to reach a
runtime.

`runtime interactive-session --id <runtime>` reports whether a runtime
currently has a live session. This mechanism is unix-only —
`github.com/creack/pty` doesn't support Windows — which is not a regression;
its tmux-based predecessor never worked on Windows either.

**Two failure modes were confirmed live and are actively guarded against, not
just documented — carried over from an earlier tmux-based iteration of this
same mechanism, now implemented against the owned pty instead of shelling
out:**

1. A second invocation arriving while the target is still busy on its own
   long tool-calling turn used to land glued onto an earlier, not-yet-
   submitted delivery with no separator — confirmed live with two
   agent-to-agent messages that arrived back to back mid-turn. `Deliver`
   checks readiness first: it waits (up to 90s) for the target to stop
   showing a busy marker (`esc to interrupt` / `esc again to interrupt`,
   the status-line hint both codex's and opencode's TUIs show while working
   and omit once idle, read from an in-process tee of the child's own raw
   output) before sending anything at all, rather than injecting and
   hoping. This is a heuristic, not a protocol-level signal — neither
   provider exposes one.
2. Firing "send text" and "send Enter" back to back, with no gap, let Enter
   arrive before the target registered the preceding text as input and
   silently dropped it — confirmed live against a real codex session, where
   the message sat typed-but-unsubmitted until something pressed Enter
   again. `Deliver` waits (up to 10s) for the pty to visibly echo the sent
   text back before pressing Enter, rather than trusting a fixed sleep to
   be long enough. Owning the raw byte stream (instead of tmux's
   already-rendered screen grid) introduced one further, genuinely new risk
   here: stale, pre-clear content could otherwise sit in the match buffer
   and produce a false echo/busy match after a real screen repaint — the
   buffer resets whenever a clear or alternate-screen-buffer escape
   sequence is observed, specifically to close that gap.

Both checks fail closed as `invocation.delivery-failed`, never as a blind
Enter-press or false notification. `invocation inspect --id <id>` shows each
attempt, its transport evidence, and the separate target acknowledgement.

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

What remains a genuine, structural limit rather than something more testing
would find: nothing here can distinguish a human directly using a live
session's own terminal from the runtime it belongs to being idle — a
terminal running `interactive-serve` has to be dedicated to that purpose.
This was hit live in exactly this form under the earlier tmux-based
iteration (a demo pane doubled as a personal chat session) and there is no
code fix for it, only the operating discipline `--session-id`'s
documentation already states. `interactive-serve` prints a one-line banner
before handing control to the wrapped command as a visibility nudge — it
reduces, but cannot eliminate, the chance of forgetting a terminal is
dedicated this way; it is not a detection mechanism.

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
