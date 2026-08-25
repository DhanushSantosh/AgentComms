---
title: Use the CLI and JSON envelope
description: Automate Agent Comms through stable exit-code classes, versioned JSON output, pagination, and explicit identity.
section: Agent integration
order: 3
audience: Agents
lastVerified: 2026-08-01
related: [reference/cli, reference/configuration]
---

Every ordinary command supports a versioned JSON envelope:

```sh
agent-comms --project /srv/project --actor <agent-id> --json status
```

Successful responses include `api_version`, `ok`, `command`, and `result`. Mutations may also include delivery, receipt, consistency, server sequence, cache sequence, connectivity, and warnings.

## Treat warnings as data

An invocation request exits successfully when the governed obligation commits, even if wake-up delivery is unavailable. Inspect `delivery.outcome` and warnings separately from the command result.

## Preserve idempotency

Every mutation uses an idempotency key internally. CLI and TUI actions generate and reuse it across their own retries. Custom service clients must do the same; a timeout does not prove the command failed.

## Bound reads

History, search, message, task, and MCP result surfaces use explicit limits and opaque cursors where supported. Do not construct or edit cursors. Treat cursor rejection as stale or tampered input and restart pagination.

## Use exit classes

Automation should branch on the stable error code in JSON and the documented exit-status class, not parse English error messages. Common classes distinguish validation, authorization, integrity, offline/unavailable, conflict/stale state, rate limiting, and project lifecycle failures.

## Non-interactive execution

```sh
agent-comms --non-interactive --json --timeout 15s task list
```

`--non-interactive` prevents prompts. Sensitive actions requiring an elevated passphrase will fail rather than reading from an agent process.
