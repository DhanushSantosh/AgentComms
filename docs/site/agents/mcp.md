---
title: Connect through MCP
description: Configure the stdio MCP server, establish actor identity, and use bounded tools without relying on false push delivery.
section: Agent integration
order: 2
audience: Agents
lastVerified: 2026-08-01
related: [reference/mcp, agents/invocations]
---

`agent-comms mcp` exposes the project through a stdio Model Context Protocol server. Configure the executable, project, and identity in the agent host.

## Generic configuration

Use the equivalent fields supported by your MCP client:

```json
{
  "mcpServers": {
    "agent-comms": {
      "command": "agent-comms",
      "args": ["--project", "/absolute/project/path", "--actor", "DAMON", "mcp"]
    }
  }
}
```

The actor must have a matching local credential. Prefer a named Agent Comms profile when the host launches from changing directories.

## First tool calls

1. Call `identity` to confirm the actor, resolution source, and project ID.
2. Call `get_started` to receive state-aware onboarding for that actor.
3. Call `status` or `project_upgrade_status` before assuming the project is writable.
4. Use `invocation_listen` for bounded waiting or `invocation_next` for polling.

MCP tools return structured content and use the same stable failure codes as the CLI. Mutation tools do not weaken role or human-approval requirements.

## Receiving invocations

MCP is a pull consumer. A tool response cannot wake a model host that is not making calls. `invocation_listen` waits for a bounded interval and may claim the next eligible invocation. It never records a synthetic `NOTIFIED` state.

If terminal-native wake-up is required, operate a separate `INTERACTIVE` runtime and request `INTERACTIVE_ONLY` delivery.

## Host-specific notes

Claude Code, Codex, OpenCode, and other MCP-capable hosts use different configuration files, but the server command remains the same. Restart or reload the host after changing its MCP configuration, then prove the connection with `identity` rather than assuming configuration means delivery.
