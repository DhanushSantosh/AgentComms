---
title: Operate and recover
description: Monitor authority pressure, rebuild local caches, handle outages, and preserve the audit boundary during recovery.
section: Team operations
order: 2
audience: Operators
lastVerified: 2026-08-01
related: [guide/maintenance, security/integrity]
---

PostgreSQL is the source of truth in team mode. Local SQLite projections and stream cursors are rebuildable caches.

## Monitor the right signals

Track transaction latency, project-head lock wait, rejected conflicts, idempotent replays, stream and cache lag, queue depth, database pool pressure, signature failures, rate limiting, and migration progress.

Structured diagnostics must identify the project, sequence, operation, and failure class without logging private keys, connector secrets, or sensitive payload bodies.

## Service or database outage

- Connected cached reads remain available with consistency, cache sequence, server sequence, and connectivity metadata.
- Governed mutations return `OFFLINE` or `UNAVAILABLE`; they never report success from a local queue.
- Local drafts remain drafts and may be submitted after authority recovers.
- Retry accepted commands with the same idempotency key after respecting retry hints.

## Rebuild a local cache

Stop the local daemon through supported lifecycle commands, preserve project evidence, and follow `doctor` output. The daemon can resume from its cursor or rebuild the canonical JSON projection from PostgreSQL. Do not promote the cache to authority.

## Delivery recovery

Delivery attempts use bounded leases. After a daemon crash, an expired attempt is recorded as timed out before retry. Only one unexpired automatic attempt may exist for an invocation/runtime pair.

Interactive sessions submit `runtime.offline` and remove their socket on clean exit. After a crash, heartbeat expiry determines offline state.

## Backups and rollback

Back up PostgreSQL and service-key material under separate access controls. A database restore and service signing key must refer to the same audit history. After new event types are written, restore the matching pre-upgrade backup before running an older binary.
