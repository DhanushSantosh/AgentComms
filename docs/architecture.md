# Architecture

CLI, TUI, and stdio MCP are adapters around one transport-neutral application
service. A project runs in either legacy mode or authoritative service mode;
the managed bootstrap records the selected runtime.

## Authoritative service mode

The Go authority verifies an actor-signed canonical command and performs each
mutation in one short PostgreSQL transaction. The transaction locks the
project head, reloads current projections, revalidates authorization and
resource conflicts, appends the event, updates normalized projections,
advances the hash-chain head, and writes an outbox row. The returned receipt
binds the project, sequence, event hash, actor-intent hash, and commit time to
the service signing key.

Events are append-only and hash-partitioned. PostgreSQL uniqueness constraints
protect event IDs and idempotency keys. Agents, tasks, active resources,
messages and recipients, approvals, decisions, documents, artifacts, and
environment entries have normalized current-state projections. Git never
participates in mutation success.

A per-user daemon is the only local cache writer. It resumes bounded event
pages into a SQLite WAL database and exposes cached freshness, connectivity,
server sequence, and cache sequence over a Unix socket or Windows named pipe.
Governed mutations require the authority. Offline documents, message bodies,
and artifact metadata are explicitly bounded drafts, not events or current
truth.

The HTTP authority applies request-size limits, bounded admission, per-actor
and per-project rate limits, opaque pagination cursors, database statement
timeouts, and graceful shutdown. Health, readiness, Prometheus metrics,
structured logs, and audit verification are exposed without placing secrets
in diagnostics.

## Integrity and migration

Actor signatures attest intent before sequencing; service signatures attest
the committed sequence and head. Verification can check actor intent, service
receipts, ranges, or the full chain. Actor public-key history and rotation
boundaries remain part of the audit record.

Migration locks and verifies the complete schema-v2 runtime, records its head
and Git commit, uploads original event bytes in resumable idempotent batches,
compares deterministic projections, stores a server-signed import receipt,
and atomically switches the bootstrap. The legacy runtime then becomes
read-only migration evidence.

## Legacy mode

Unmigrated projects retain the schema-v2 filesystem engine. Writers hold one
cross-process runtime lock across validation and publication, fsync a
temporary event, atomically rename it, and commit the checkpoint to the
isolated runtime Git repository. This mode is retained for single-host
compatibility and recovery, not high-contention coordination.

In both modes, private actor keys live in platform keyrings. The target
repository receives a compact `.agents` bootstrap; credentials are never
stored in project history.
