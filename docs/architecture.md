# Architecture

CLI, TUI, and stdio MCP are adapters around one transport-neutral application
service. A project runs in personal mode or authoritative service mode; the
managed bootstrap records the selected authority.

## Personal mode

Personal mode is the default for agents and clients on one machine. A
per-project daemon is the sole writer to an authoritative SQLite WAL database.
Every mutation verifies the actor-signed command, checks idempotency, reloads
and validates current state, appends an event, updates the projection and head,
and stores a project-scoped service-signed receipt in one transaction.

CLI, TUI, and stdio MCP use the daemon's local socket. The daemon starts on
demand and reports `PERSONAL_AUTHORITATIVE` consistency with `LOCAL`
connectivity. It requires no listening TCP port, container runtime, or database
server. SQLite database files and the signing key are user-private; the key is
stored in the platform credential store.

The daemon also owns invocation wake-up delivery. It reserves an authoritative
delivery attempt before touching a connector and records success only after
bounded connector or PTY evidence exists. Request commitment, transport
delivery, target claim, and completion remain separate facts.

Personal mode coordinates concurrent processes on one machine. It does not
claim multi-host availability or PostgreSQL service-mode load targets.

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

Interactive runtimes are host-local supervised sessions. A random
per-installation host ID prevents one host from treating another host's PTY as
local. Cross-host terminal relay is intentionally not provided.

The HTTP authority applies request-size limits, bounded admission, per-actor
and per-project rate limits, opaque pagination cursors, database statement
timeouts, and graceful shutdown. Health, readiness, Prometheus metrics,
structured logs, and audit verification are exposed without placing secrets
in diagnostics.

## Integrity

Actor signatures attest intent before sequencing; authority signatures attest
the committed sequence and head. Verification can check actor intent,
authority receipts, ranges, or the full chain. Actor public-key history and
rotation boundaries remain part of the audit record.

In both modes, private actor keys live in platform keyrings. The target
repository receives a compact `.agents` bootstrap; credentials are never
stored in project history.

A HUMAN principal may additionally hold a second, distinct "elevated" key
(`agent-comms agent elevate-key`), stored under its own keyring entry
(`internal/identity.ElevatedActor`) and encrypted at rest with a passphrase
(Argon2id + AES-256-GCM) — unlike the everyday key above, this one isn't
usable without the passphrase, never written anywhere. It's required, in
place of the everyday key, for granting the Orchestrator role, approving a
HUMAN-tier approval, revoking an Orchestrator or HUMAN principal, and
deleting any revoked principal (see docs/governance.md).

Every newly committed event also records the fingerprint of the exact actor
key whose signature the authority verified. That fingerprint is part of the
event hash, so identity reuse and ordinary key rotation remain
tamper-evidently distinguishable in history.
