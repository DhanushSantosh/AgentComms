---
title: Personal and team modes
description: Choose local SQLite coordination for one machine or PostgreSQL authority for a multi-host team.
section: Start here
order: 4
audience: Everyone
lastVerified: 2026-08-01
related: [operations/deploy, security/integrity]
---

Both runtime modes use the same protocol and interfaces. The difference is where authoritative state lives and how many hosts may coordinate safely.

## Personal mode

Personal mode is designed for one user coordinating agents on one machine.

- A per-project daemon owns the write path.
- SQLite in WAL mode stores authoritative state.
- CLI, TUI, and MCP clients share the daemon rather than competing for filesystem locks.
- Reads remain local and governed writes require the local authority.
- No account, server container, or PostgreSQL installation is required.

This is the default created by `agent-comms init`.

## Team mode

Team mode is for multiple users or machines that require one consistent project head.

- PostgreSQL is authoritative.
- Each mutation is revalidated and committed in one short database transaction.
- The service appends the event, updates projections, advances the head, and writes an outbox record atomically.
- Local daemons maintain rebuildable SQLite caches and resumable cursors.
- Governed writes prefer correctness over availability during a network partition.

Team mode requires the service deployment and database described in [Deploy the service](/operations/deploy/).

## Comparison

| Concern | Personal | Team |
|---|---|---|
| Intended topology | One user and host | Multiple users or hosts |
| Authority | Local SQLite | PostgreSQL service |
| Local daemon | Yes | Yes, with projection cache |
| Offline reads | Local state | Cached state with freshness metadata |
| Offline governed writes | No | No |
| Git in mutation path | No | No |
| Operational setup | None beyond installation | Service, database, keys, monitoring |

Do not select team mode just because several agents run on the same workstation. Personal mode already coordinates concurrent local clients.
