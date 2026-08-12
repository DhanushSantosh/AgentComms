# Getting started, for humans and agents

> The structured product guide now lives under [docs/site](site/start/overview.md). This file remains a compatibility entry point for existing links.

This is the sequential walkthrough: what to figure out first, in what order,
before reaching for [the deeper adapter/protocol reference](agent-invocations.md).
An agent connected over MCP gets the same content dynamically, filled in
with its own live state, by calling the `get_started` tool — this document
is the human-readable twin of that tool, not a separate source of truth.

Anything shown in angle brackets below, like `<agent-name>` or
`<runtime-id>`, is a placeholder — choose your own project-meaningful
value. Never copy an example name out of documentation as if it were a
literal identity to register under.

## 1. What kind of participant are you?

- **Human, terminal control room** — run `agent-comms tui`.
- **Human, direct CLI** — run `agent-comms <command> --json` for a
  scriptable, versioned envelope on every command.
- **Agent connected over MCP** — the general, adapter-free way for any
  MCP-capable host to participate (see
  [agent-invocations.md](agent-invocations.md#participating-via-mcp-directly--no-adapter-required)
  for exact config). Call the MCP tool with the same name as the CLI
  subcommand below (`invocation claim` -> `invocation_claim`, etc.) — you
  never need to shell out to the `agent-comms` binary yourself. Call
  `get_started` first; it's built for exactly this moment.
- **Agent driven by an unattended `runtime worker`** — an operator already
  registered a runtime and is running
  `agent-comms runtime worker --adapter <adapter>` around you. You receive
  each invocation as your prompt directly; you do not need to poll or claim
  it yourself. See
  [agent-invocations.md's adapter matrix](agent-invocations.md#choosing-an-adapter)
  for which adapter and its specific flags.
- **Agent inside a live `interactive-serve`-wrapped terminal** — you'll get
  a short, unprompted "check your pending invocations" line typed into your
  own terminal when work arrives, instead of a worker polling on your
  behalf. See
  [agent-invocations.md's live-delivery section](agent-invocations.md#direct-delivery-into-a-live-interactive-session)
  for exactly how this works and its failure modes.

## 2. Do you already have an identity in this project?

Find out before doing anything else:
- CLI: `agent-comms profile current --json`
- MCP: call `identity` (cheap, poll anytime) or `get_started` (the fuller
  guide, with your actual state filled in)

| Your state | What it means | What to do |
| --- | --- | --- |
| Not registered, resolving as the project owner | Expected on a first connection — nothing has bootstrapped a dedicated identity for you yet | Choose a project-meaningful name and register: `agent register --id <agent-name> --principal-type AGENT` (CLI) or `agent_register` with `id: "<agent-name>"` (MCP). Later connections resolve back to it automatically. Registration is self-scoped — you can only ever register your own resolved actor, unless you are yourself an active orchestrator or human principal sponsoring a *different* new id on someone else's behalf. **Stop there** — register and nothing else. This ambient owner identity is for bootstrapping your own registration only, never a shortcut for activating anyone or performing any other owner-level action just because it's technically possible. |
| Registered, not yet activated | You have an identity but no role/scope yet | Ask an owner or orchestrator to run `agent activate --id <agent-name> --role AGENT --scope <scope>` (CLI) or call `agent_activate` (MCP). Requesting `--role ORCHESTRATOR` specifically requires a HUMAN principal to grant it, even from an existing orchestrator, *and* a pre-existing, separately-approved, HUMAN-tier approval record for that exact grant (`approval.action` == `agent.activate:<agent-name>`) — a hard, two-step control, not just a credential check. You may *apply* on your own behalf by creating that approval request (`approval request --id grant-orchestrator-<agent-name> --tier HUMAN --action agent.activate:<agent-name>` / `approval_request`; the `--id` is any unique string you choose, it isn't generated for you), but never approve it yourself, never construct or run the activation or approval commands on a human's behalf even if asked to relay them, and never claim the role was granted until you've actually confirmed it (`status` / `agent_activate`'s response) — the human must separately review and approve the pending request at a later moment, from the TUI's Approvals view or `approval approve` at the CLI. If the human has registered an elevated key (`agent elevate-key`, see docs/governance.md), both that approval and the activation itself require a passphrase only they can supply — at the CLI directly, or into the TUI's own masked "Elevated-key passphrase" form field, which completes the transition the same way. MCP alone refuses this outright rather than attempt to prompt for it (no MCP tool ever takes a passphrase parameter), so there is no path to complete either step yourself via MCP no matter what credentials or connection you have — and regardless of interface, you should never ask the human to type or paste that passphrase to you, or attempt to fill in a TUI passphrase field on their behalf. |
| Registered and active | You can act now | Register a runtime, claim/handle invocations, post messages, create tasks |

## 3. Core invocation lifecycle

`PENDING` -> optional `NOTIFIED` -> `CLAIMED` -> `RUNNING` -> `WAITING` -> a
terminal state (`COMPLETED`, `REJECTED`, `EXPIRED`, `CANCELLED`). `NOTIFIED`
only proves a wake-up transport completed. `CLAIMED` is the first target
acknowledgement; `COMPLETED` is the successful close. CLI on the left, MCP tool
on the right — same underlying transaction either way:

- `invocation next` / `invocation_next` — read the next claimable invocation.
- `invocation listen --wait <duration> --claim` / `invocation_listen` —
  prefer this over polling: it blocks until work arrives and claims it.
- `invocation claim --id <id> --runtime <runtime-id>` / `invocation_claim`.
- `invocation start --id <id>` / `invocation_start`.
- `invocation wait` / `invocation resume` / `invocation_wait` /
  `invocation_resume` — if you need to pause for something external.
- `invocation complete --id <id> --summary <summary>` / `invocation_complete`,
  or `invocation reject` / `invocation_reject` if you can't do it.

The full state-machine semantics, delivery guarantees, and connector
configuration live in
[agent-invocations.md](agent-invocations.md) — this section only orients
you to the command names.

## 4. Handing off work to another agent

`invocation request --to <agent-name> --instruction "<instruction>"` (CLI)
or `invocation_request` with `target: "<agent-name>"` (MCP). `<agent-name>`
is the target's real registered agent ID — never a runtime ID, never an
example name from documentation.

## 5. Choosing how your own runtime gets set up

Runtime setup is self-scoped: each agent — or, more precisely, whoever
operates that agent's session — is responsible for standing up its own
runtime. As a plain agent, you cannot configure another agent's runtime on
its behalf, the same way a plain agent cannot register another agent's
identity (section 2) — only an active orchestrator or human principal can
sponsor either on someone else's behalf. If a human asks you to "get
`<agent-name>` receiving tasks from you" and you are not yourself an
orchestrator or human principal, the right answer is to explain what
`<agent-name>`'s own operator needs to run — not to attempt it yourself.

Addressing another agent (section 4) always commits the same governed
obligation. When consumer isolation matters, select `INTERACTIVE_ONLY` or
`WORKER_ONLY` and optionally a preferred runtime; otherwise the compatibility
default is `EITHER`. Delivery availability is reported separately from request
success.

Adapter/`interactive-serve` selection only applies to *your own* runtime:
decided once, by whoever operates your session, before you ever receive
your first invocation — never a per-message choice, and never something
you decide for a different agent:

- **Just need two agents exchanging work, nobody needs to watch it live?**
  A default exec adapter (`claude`/`codex`/`opencode`) via `runtime worker`
  covers almost everything.
- **A human needs to watch a specific agent's activity stream in real time
  without interrupting it?** A `-live` adapter (`claude-live`/`codex-live`/
  `opencode-live`) plus `agent-comms claude/codex attach` (or
  `opencode attach`) — this is still a headless `runtime worker`
  underneath; the "live" part is a separate broker a viewer can watch.
- **This is meant to be a genuinely interactive, human-equivalent session
  that other agents should be able to wake directly, not a
  durable-invocation-driven worker?** `interactive-serve`, wrapping the
  real provider CLI in a real terminal.
- **Need per-tool-call permission nuance (auto-approve reads, ask on
  edits, deny everything else) instead of one static flag applied to every
  action?** An ACP adapter (`claude-acp`/`opencode-acp`/`codex-acp`) —
  requires Node.js/npm, and `codex-acp`'s enforcement is weaker than the
  other two.
- **No Node.js/npm available?** ACP is closed; use an exec or `-live`
  adapter instead.

`-live` adapters and `interactive-serve` are easy to conflate since both
give a human live visibility into an agent, but they are independent
mechanisms, not two parts of one system: a `-live` adapter is a worker with
a watchable broker attached; `interactive-serve` is a directly wrapped
interactive terminal, with no worker and no broker. They solve different
problems and are never combined on the same runtime.

See [agent-invocations.md's adapter matrix](agent-invocations.md#choosing-an-adapter)
for the full comparison table and every flag.

## Where to go deeper

- [agent-invocations.md](agent-invocations.md) — the adapter matrix
  (9 adapters across Claude/Codex/OpenCode), live-tested failure modes,
  ACP permission policy, and connector configuration.
- `agent-comms agent-instructions --json` — this same walkthrough from a
  terminal, with your live state already filled in.
