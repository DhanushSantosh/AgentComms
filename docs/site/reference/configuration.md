---
title: Configuration and errors
description: Find global flags, connector boundaries, runtime metadata, consistency fields, and stable failure classes.
section: Reference
order: 3
audience: Everyone
lastVerified: 2026-08-01
related: [agents/cli-json, guide/maintenance]
---

## Global CLI flags

| Flag | Meaning |
|---|---|
| `--project` | Target project root instead of the working directory. |
| `--profile` | Select a named user identity profile. |
| `--actor` | Select an actor only when its credential matches. |
| `--json` | Emit the versioned JSON envelope. |
| `--non-interactive` | Refuse prompts. |
| `--timeout` | Bound transaction and connection waits. |
| `--no-color` | Disable ANSI output. |
| `--quiet`, `-q` | Suppress non-essential output. |

## Local connector configuration

Runtime events contain a configuration reference, not connector secrets. `LOCAL_PROCESS` and `WEBHOOK` references must resolve through the official daemon's mode-0600 user configuration. Dispatch revalidates type, executable, permissions, working directory, or webhook configuration every time.

`INTERACTIVE` endpoints are ephemeral and host-local. `MANUAL`, `MCP`, and `QUEUE` connectors never create delivery-success evidence by themselves.

## Result metadata

Interfaces may return `consistency`, `server_sequence`, `receipt`, `cache_sequence`, and `connectivity`. Treat those fields as part of the result's truth boundary, particularly for cached team-mode reads.

## Stable failure classes

| Code | Exit | Meaning |
|---|---:|---|
| `VALIDATION` | 2 | Command, payload, state transition, or cursor is invalid. |
| `AUTHORIZATION` | 3 | Identity, role, scope, or human authority is insufficient. |
| `INTEGRITY` | 5 | Signature, receipt, hash, or chain verification failed. |
| `EXTERNAL` | 7 | A connector, process, or remote dependency failed. |
| `OFFLINE` / `UNAVAILABLE` | 8 | Required authority or runtime cannot currently serve the action. |
| `CONFLICT` / `STALE_PRECONDITION` | 9 | Current governed state conflicts with the attempted mutation. |
| `RATE_LIMITED` | 10 | Admission control rejected the request. |
| Lifecycle errors | 11–12 | Project upgrade is required, incompatible, unsupported, or failed. |

Automation should read the JSON `error.code`; English messages remain actionable but are not a parsing contract.
