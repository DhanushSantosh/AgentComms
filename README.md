# Agent Comms

[![Release](https://img.shields.io/github/v/release/DhanushSantosh/AgentComms)](https://github.com/DhanushSantosh/AgentComms/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

**Let your AI agents work as a real team — not a pile of scripts hoping not to collide.**

The moment you run more than one coding agent on the same project — Claude Code and Codex, two Claude sessions, an agent and you — you hit the same wall: nobody knows what anyone else is doing. Files get silently overwritten. An agent quietly grants itself more permission than it should have. When something breaks, there's no real record of who did what, or why.

Agent Comms is the coordination and governance layer that fixes this. It gives every human and every agent in your project a signed identity, a shared task list, a real conversation channel, and a cryptographically verifiable record of every action — so you can actually trust a team of agents with real work, not just watch them nervously.

## Why it's different

Most tools solve half of this problem. Agent Comms is built around three things nothing else in the space combines:

- **Cross-vendor, not walled-in.** Claude Code, Codex, and OpenCode talk to each other and to you as equal, signed participants. Compare that to Claude Code's own Agent Teams, where every teammate has to be another Claude Code session — you can't bring in a different agent, and its internal mailbox isn't documented as producing a cryptographically signed record the way Agent Comms' events are.
- **Governance the underlying protocols don't provide.** Academic research on MCP, A2A, and ACP — the wire protocols agents actually speak — has found they're explicitly *not* designed to express authorization, audit, or approval workflows. Agent Comms sits on top of that gap: every mutation is signed, and destructive, irreversible, or credential-touching actions require an explicit human approval before they happen — enforced by the system, not by hoping the agent's instructions were followed.
- **Delivery you can actually trust.** When Agent Comms wakes up an agent, it doesn't just drop a message in a queue and hope. For a live interactive session, it types the message directly into that session and cryptographically confirms it was received before doing anything else — a real proof-of-receipt, not fire-and-forget.

## What you get

- **No more silent collisions** — work leases mean two agents (or an agent and you) can never touch the same code path without knowing about it first.
- **A trail you can actually audit** — every task, message, and decision is a signed event with the exact key that produced it. When something goes wrong, you know exactly what happened.
- **Nothing risky happens unsupervised** — deleting an agent, escalating to orchestrator, touching credentials or production data: all of it requires a human, by default, not by convention.
- **Real conversations, not just task queues** — typed messages (FYI, ACTION, CONTRACT, BLOCKER, DECISION) carry real per-recipient obligations, and a full terminal control room to see it all happen live.
- **Zero setup to start, room to grow** — one command gets a solo project running locally with no server. Add a shared PostgreSQL authority only when you actually have a team.
- **No lock-in, no telemetry** — works with the agent tools you already run; nothing phones home.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.sh | sh   # Linux / macOS
```

```powershell
irm https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.ps1 | iex        # Windows
```

Releases are signed and verified with SHA-256 and Sigstore. See [release verification](docs/release-verification.md) for how to check that yourself, and [source-build instructions](docs/development-workflow.md) if you'd rather build from source.

## Try it in two minutes

```sh
agent-comms init      # no target Git repo needed — sets up a local project right where you are
agent-comms tui        # open the control room
```

From there, `agent-comms agent register` brings an agent into the project, and `agent-comms task create` / `message post` get real work moving between humans and agents. The full walkthrough — including wiring up an actual Claude Code, Codex, or OpenCode agent as a live participant — is in [getting started](docs/agent-onboarding.md).

## How it compares

|  | Agent Comms | Claude Agent Teams | AutoGen / CrewAI / LangGraph | Jira + Rovo / Monday.com |
|---|---|---|---|---|
| Works across different agent vendors | Claude, Codex, OpenCode | Claude Code only | Depends on your own app | Any, via integrations |
| Signed, tamper-evident audit trail | Yes | Not documented | Not documented | Enterprise logs, not agent-signed |
| Human approval enforced by the system | Yes, built in | Not documented | Bolt-on, custom code | Human task approvals, not agent-action gates |
| Verified live delivery into a running session | Yes, cryptographically confirmed | Internal mailbox | In-process message passing | N/A |
| Setup | One command, local, free | Requires Claude Code | Self-hosted framework | Paid cloud SaaS |

*(This is our honest read of publicly documented behavior as of mid-2026, not hands-on testing of every competitor — see [CREDITS.md](CREDITS.md) for the protocols and prior work we build on.)*

## Runtime modes

**Personal mode** is the default: one user, one machine, zero setup. A per-project daemon owns an authoritative SQLite database and starts automatically on the first command — no PostgreSQL, no Docker, nothing to configure.

**Team mode** adds a shared PostgreSQL authority for multi-host coordination: mutations are serialized and receipt-signed centrally, while each user still keeps a fast local cache. See the [team service deployment guide](docs/service-deployment.md).

## Governed by default

- Leases last four hours and require real, progress-bearing renewal — a heartbeat alone never keeps ownership.
- Shared writes, takeovers, and scope changes require orchestrator-level governance; granting orchestrator itself requires a separate, explicitly human-approved decision.
- Destructive, irreversible, external, production-data, and credential actions require a human approver — gated behind a second, passphrase-protected signing key for exactly those transitions.
- Completed work stays active for seven days, then archives without deleting history.
- No telemetry, ever. Update checks are explicit and opt-in.

## Interfaces

- `agent-comms tui` — the full terminal control room.
- `agent-comms mcp` — a stdio MCP server for editors/agents that speak MCP.
- `--json` on any command — a versioned, scriptable envelope for automation.
- `agent-comms doctor` — a health check that explains exactly what's wrong and how to fix it.

## Learn more

[Getting started](docs/agent-onboarding.md) · [Agent invocation protocol](docs/agent-invocations.md) · [Architecture](docs/architecture.md) · [Governance](docs/governance.md) · [Threat model](docs/threat-model.md) · [Development workflow](docs/development-workflow.md) · [Contributing](CONTRIBUTING.md) · [Release process](docs/releasing.md) · [Changelog](CHANGELOG.md)

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

Worker runtimes speak the open [Agent Client Protocol](https://agentclientprotocol.com), originally published by [Zed Industries](https://zed.dev) — see [CREDITS.md](CREDITS.md).

Licensed under [Apache-2.0](LICENSE).
