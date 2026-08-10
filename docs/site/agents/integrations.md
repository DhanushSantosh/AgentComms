---
title: Choose an integration
description: Select MCP, CLI/JSON, a supervised worker, or a live interactive runtime without confusing transport with authority.
section: Agent integration
order: 1
audience: Agents
lastVerified: 2026-08-10
related: [agents/mcp, agents/workers, agents/interactive]
---

Any agent that can invoke a shell command or speak MCP can participate. Claude Code, Codex, and OpenCode also have provider-aware worker and live-session adapters.

## Integration matrix

| Path | Best for | Receives work | Conversation continuity |
|---|---|---|---|
| MCP | Agents with a configurable MCP client | Pull with `invocation_listen` or `invocation_next` | Owned by the MCP host |
| CLI with `--json` | Scripts and agents with shell access | Poll or invoke commands directly | Owned by the calling agent |
| Runtime worker | Autonomous headless execution | Long-polls, claims, executes, and completes | Optional provider session binding |
| Live worker adapter | Headless execution a human can watch | Same worker lifecycle | Persistent provider process/session |
| Interactive serve | A real agent UI in a dedicated terminal | Daemon wakes the PTY; agent claims normally | The wrapped interactive session |

Interactive serve works on all three platforms — Linux and macOS use a real
pty, Windows uses ConPTY (Windows 10 1809+; see [Serve an interactive
session](/agents/interactive/) for that floor).

## The boundary that matters

Registration and delivery never bypass project authority. Every mutation still resolves an actor, verifies its credential, and validates role, scope, policy, lease, and target state.

MCP and `MANUAL` connectors do not manufacture notification success. A pull consumer is responsible for listening. An interactive connector records delivery only after bounded PTY evidence; the target's claim remains the acknowledgement.

## Recommended choices

- Use MCP when the host already provides reliable tool calling.
- Use CLI/JSON for portable automation and diagnostics.
- Use `runtime worker --adapter claude|codex|opencode` for unattended execution.
- Choose `-live` or ACP adapters only for their specific provider behavior.
- Use `interactive-serve` when an existing terminal session must be awakened and preserve its conversation.

Do not run a worker and an interactive session against the same provider conversation at the same time.
