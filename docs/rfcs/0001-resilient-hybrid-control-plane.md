# RFC 0001: Resilient hybrid control plane

- Status: Accepted
- Owner: Project owner
- Accepted: 2026-07-19

## Problem and desired outcome

The schema-v2 filesystem engine validates commands before acquiring its
cross-process lock, reconstructs and verifies the complete event history for
ordinary reads, and performs a Git commit for every mutation. It is suitable
for cooperative single-host use but cannot safely coordinate concurrent hosts
or sustain the target load.

Agent Comms will support 100 concurrent agents, 100 sustained writes per
second, and millions of retained events per project without weakening leases,
authorization, auditability, or offline inspection.

## Proposed design

An authoritative Go service backed by PostgreSQL owns mutation ordering and
current projections. Each command carries an idempotency key and actor-signed
intent. One database transaction locks the project head, validates against
current projection rows, appends the event, updates projections, advances the
head, and writes an outbox record. The service returns a signed receipt.

A per-user daemon owns a rebuildable SQLite WAL cache. CLI, TUI, and MCP use a
transport-neutral client. Cached reads remain available offline, but governed
mutations require the authority. Explicit local drafts are not durable events
or current truth.

Git becomes optional asynchronous audit export and is removed from the write
transaction. Existing schema-v2 history is verified and imported
idempotently, preserving original bytes and signatures.

## Alternatives considered

- Optimizing the filesystem engine retains a single-host lock boundary and
  cannot provide multi-host authority.
- Git-based coordination cannot safely serialize concurrent writers.
- Server-only signatures weaken actor accountability.
- Optimistic offline governed writes permit split-brain ownership.

## Compatibility and migration

Existing commands, the `agent-comms/v1` JSON envelope, exit classes, TUI
workflows, and MCP tool names remain compatible. Responses gain consistency,
sequence, receipt, cache, and connectivity metadata. New stable errors cover
offline, conflict, rate limiting, stale preconditions, and unavailability.

Migration verifies and locks the legacy runtime, uploads preserved events in
resumable batches, compares projections, obtains an import receipt, and
atomically activates service mode. The legacy engine becomes read-only after
cutover and remains available as evidence and for bounded rollback.

## Security and privacy

Actors sign canonical command intents. The authority verifies the intent and
adds a service-signed receipt and event head. Service keys must come from an
explicit file or secret-provider implementation in production. PostgreSQL is
authoritative; SQLite contains a rebuildable local cache and bounded drafts.
Network partitions favor correctness over governed-write availability.

## Test and rollout

Tests cover conflicting concurrent commands, idempotent retries, signatures,
transaction failure points, outbox recovery, cache rebuild/resumption,
offline behavior, migration equivalence, limits, pagination, and compatibility.
Load acceptance uses 100 concurrent agents, 100 writes per second for fifteen
minutes, and a project with at least one million events.

Rollout is staged behind explicit service-mode configuration. Legacy projects
continue using the existing engine until an explicit verified migration.

## Unresolved questions

Cross-region active-active authority is deferred. The first scalable release
uses one authoritative PostgreSQL deployment.
