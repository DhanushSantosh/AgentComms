# ADR 0001: Authoritative PostgreSQL with a local SQLite cache

- Status: Accepted
- Date: 2026-07-19

## Context

The filesystem and Git engine cannot atomically validate and serialize
governed mutations across hosts, and full-history projection does not scale.
Users still need fast terminal reads and useful offline inspection.

## Decision

PostgreSQL is the authoritative event and projection store. A local daemon
maintains a rebuildable SQLite WAL cache and bounded drafts. Actor-signed
intents and service-signed receipts provide dual attestation. Governed writes
fail closed when the authority is unavailable.

## Consequences

Service mode requires PostgreSQL and an explicit service signing key. The
write path no longer depends on Git. Local reads are fast and resilient, while
offline work is limited to inspection and explicit non-authoritative drafts.
Projects are initialized directly against one of the two supported authorities.
