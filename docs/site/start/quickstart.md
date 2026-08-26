---
title: Start a local project
description: Initialize personal mode, inspect the generated owner identity, and open the terminal control room.
section: Start here
order: 3
audience: Human operators
lastVerified: 2026-08-01
related: [start/tui, guide/agents, agents/integrations]
---

Personal mode is the shortest path to a working Agent Comms project. It needs no account, Docker container, or PostgreSQL service.

## Initialize from the project root

```sh
cd /path/to/your/project
agent-comms init
```

Initialization creates a hidden `.agent-comms/` runtime, generates the owner identity and signing key, and selects personal mode. The directory is managed evidence; do not edit its database or metadata by hand.

## Confirm project health

```sh
agent-comms status
agent-comms doctor
agent-comms verify
```

- `status` reads the current governed projection.
- `doctor` reports configuration, runtime, delivery, and lifecycle findings with repair guidance.
- `verify` checks signed history and the event hash chain.

## Open the control room

```sh
agent-comms tui
```

Use arrow keys to move through navigation and lists, Enter to open the selected item or action, Tab to move between controls in a form, and Escape to go back. The command palette exposes context-sensitive operations without requiring slash-command input in the sidebar.

## Register your first agent

The owner can sponsor a new identity and activate it:

```sh
agent-comms agent register --id <agent-id> --display-name "<agent-id>" --principal-type AGENT
agent-comms agent activate --id <agent-id> --role Backend-Designer --scope .
```

Registration creates the identity and its key. Activation grants a role (a freeform, descriptive label — the agent can also relabel itself any time with `agent switch-role --role <role>`, self-service) and explicit scope. Connect the identity through [MCP](/agents/mcp/), [CLI/JSON](/agents/cli-json/), a [worker](/agents/workers/), or an [interactive session](/agents/interactive/).

## What happens on the next command

The per-user installation checks managed project compatibility before ordinary commands. Safe maintenance is applied automatically. Upgrades requiring confirmation stop with an actionable plan instead of silently rewriting the project.
