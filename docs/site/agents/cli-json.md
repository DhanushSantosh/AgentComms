---
title: Use the CLI and JSON envelope
description: Automate Agent Comms through stable exit-code classes, versioned JSON output, pagination, and explicit identity.
section: Agent integration
order: 3
audience: Agents
lastVerified: 2026-10-02
related: [reference/cli, reference/configuration]
---

Every ordinary command supports a versioned JSON envelope:

```sh
agent-comms --project /srv/project --actor <agent-id> --json status
```

Successful responses include `api_version`, `ok`, `command`, and `result`. Mutations may also include delivery, receipt, consistency, server sequence, cache sequence, connectivity, and warnings.

`--json` is a compatibility alias for `--output json`. Bounded commands emit
exactly one JSON document. Natural streams opt into JSONL explicitly:

```sh
agent-comms watch --output jsonl
agent-comms invocation listen --runtime runtime-builder --output jsonl
```

Each JSONL record includes `api_version`, `command`, `event`, `timestamp`, and
typed `data`. Bounded commands reject JSONL instead of silently changing their
document contract. MCP, shell completion, exports, and provider attachment
retain their native protocol or pass-through output.

## Read list order from `order`

List commands (`message inbox`, `task list`, `agent list`, `approval list`, `document list`, `invocation list`, `runtime list`, `env list`) keep `result` as a map keyed by ID, which carries no order. The envelope adds a top-level `order` array with exactly the displayed IDs, after filtering and `--limit`, in display order. It is empty (`[]`) when nothing matches and absent on commands that are not lists.

`message inbox` lists the newest posted message first; the other lists put the most recently changed entity first. Order comes from the signed event sequence, never from IDs, which callers can choose freely. Entities carry `created_at` and `updated_at` (RFC 3339, from the signed event that created or last changed them) plus `created_sequence` and `updated_sequence`. A timestamp is omitted, never shown as year 1, when it is unknown.

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
