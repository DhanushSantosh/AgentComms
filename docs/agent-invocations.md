# Agent invocation protocol

Agent Comms separates durable messages, runtime notifications, and execution
claims. Posting a message never grants execution authority by itself.

An invocation progresses through `PENDING`, `NOTIFIED`, `CLAIMED`, `RUNNING`,
`WAITING`, and a terminal state (`COMPLETED`, `REJECTED`, `EXPIRED`, or
`DEAD_LETTER`). Notification and claim reservations are authoritative
transactions, so competing daemons or runtime instances cannot both deliver
or claim the same invocation.

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

## Autonomous runtime workers

`runtime worker` turns a registered runtime into a complete responder rather
than a claim-only listener. It invokes Claude, Codex, or OpenCode directly,
supplies the durable invocation as the prompt, publishes the agent's result to
the requester, and completes the invocation without a user asking the
interactive agent to check its inbox.

### Choosing an adapter

Six adapters are available via `--adapter`. `claude` and `codex` exec a
provider CLI directly and are the proven default path; the other four speak
the Agent Client Protocol (ACP) or, for `opencode-live`, OpenCode's own native
server API, and are opt-in additions layered on top — none of them replaces
or changes the behavior of `claude`/`codex`.

| Adapter | Provider | Mechanism | Requires | Live-viewable | Notes |
| --- | --- | --- | --- | --- | --- |
| `claude` | Claude | direct CLI exec | `claude` binary | No | Default; proven, use unless you have a specific reason to pick an ACP adapter |
| `codex` | Codex | direct CLI exec | `codex` binary | No | Default; proven |
| `claude-acp` | Claude | ACP, via `npx @agentclientprotocol/claude-agent-acp` | Node.js/npm | No (session viewable afterward with [claude-code-viewer](https://github.com/d-kimuson/claude-code-viewer)) | Session-store-compatible with `claude` — the same conversation can be resumed by either adapter |
| `opencode-acp` | OpenCode | ACP, via `opencode acp` | `opencode` binary | No (session viewable afterward via `opencode` itself, or a third-party viewer) | |
| `codex-acp` | Codex | ACP, via `npx @agentclientprotocol/codex-acp` | Node.js/npm | No (viewable afterward with [codex-trace](https://github.com/PixelPaw-Labs/codex-trace)) | **Weaker tool-call permission enforcement than the other ACP adapters** — see below |
| `opencode-live` | OpenCode | persistent `opencode serve` + REST/SSE | `opencode` binary | **Yes** — open the reported URL in a browser while it runs | The server it starts outlives the invocation and is reused by later ones; every other adapter's process ends with the invocation |

Pick `claude`/`codex` by default. Reach for an ACP adapter only when you
specifically need what it adds — e.g. `opencode-live` when someone needs to
watch a runtime's activity happen, not just read the completed result.

None of the four ACP-based adapters support `--model` overrides yet (`runtime
worker` rejects the flag for them at startup) or need `--executable` (they
locate or spawn their own provider process). `--session-id` resumes an
existing conversation for all six adapters the same way. `--claude-permission-
mode` also gates `opencode-acp` and `opencode-live`'s edit/move-shaped tool
calls, and `--codex-sandbox` also gates `codex-acp` — the flag names are
Claude/Codex-specific for historical reasons, but their effect is shared
across every adapter that reads `PermissionMode`/`Sandbox`.

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

### `opencode-live`: watching a runtime's activity as it happens

Every adapter above runs its provider process only for the duration of one
invocation. `opencode-live` is the exception: it starts a persistent
`opencode serve` instance the first time it's needed, records its address at
`.agent-comms/cache/opencode-server.json`, and reuses that same instance for
every later invocation on this runtime — that persistence is what lets a
browser stay pointed at a stable URL and watch activity happen live, instead
of only being able to read a result once an invocation completes.

```sh
agent-comms --project /srv/project --actor reviewer runtime worker \
  --id reviewer-runtime \
  --adapter opencode-live \
  --claude-permission-mode acceptEdits \
  --execution-timeout 30m
```

The worker's `Status` output reports the URL to open, e.g. `watch this
runtime's OpenCode activity live at http://127.0.0.1:4096`. This was verified
live: a session titled with the invocation's own instruction shows up in that
server's own session list and is fully browsable while the invocation runs.
Use this specifically when a human needs to watch a runtime work, not as the
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

`--session-id` binds the runtime to one existing provider conversation and
resumes it for every invocation, preserving conversational continuity. The
value must be an exact UUID; the worker never guesses the most recent session.
Run only one worker for a bound session and do not process an interactive turn
in that conversation at the same time. Without this flag, the worker retains
isolated one-shot behavior.

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

## User policy

Owners and orchestrators configure each target agent:

```sh
agent-comms invocation policy set \
  --agent reviewer --mode TRUSTED --trusted-actor builder \
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
use exponential backoff, stop after ten attempts, and become `DEAD_LETTER`.
MCP connectors must be online; manual connectors create an auditable
notification for the human control room.

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
