# RFC 0011: Managed project lifecycle and one-step upgrades

- Status: Accepted
- Owners: Agent Comms maintainers

## Problem and desired outcome

Replacing the Agent Comms binary does not currently reconcile an already
initialized project. The event schema and product version are overloaded as
compatibility signals, generated files can silently drift, SQLite and
PostgreSQL use unjournaled `CREATE TABLE IF NOT EXISTS` initialization, and a
daemon from the previous binary can remain alive.

Updating through Agent Comms should normally require one command:

```text
agent-comms update apply
```

That command must install the verified binary and hand every project recorded
in the user profile registry to the newly installed binary for inspection,
backup, migration, daemon restart, cache synchronization, and verification. A
binary installed by another method must reconcile compatible registered
projects once on its first normal use. A user-level completion marker keyed by
build ID and registry hash prevents repeated scans. Disruptive
changes require one explicit `agent-comms project upgrade` confirmation.

## Proposed design

### Independent compatibility contracts

The product version and immutable build ID are independent from the signed
event schema, project format, managed-file format, personal-authority schema,
draft-store schema, projection-cache schema, PostgreSQL schema, daemon
protocol, and authority API. Databases store their own schema versions. The
project manifest stores its format, managed-file version, creating toolkit
build, minimum compatible toolkit, and generated-file hashes.

Normal manifest decoding is strict. A lifecycle-only tolerant decoder can read
supported older manifests and canonicalize retired fields. A project newer
than the binary is never changed.

### One lifecycle reconciler

A transport-neutral reconciler runs before opening the application service,
authority database, actor identity, or daemon. It:

1. validates the project root and acquires an OS-backed lifecycle lock;
2. inspects all component versions and any interrupted journal;
3. creates an ordered plan from immutable, checksummed migrations;
4. obtains one confirmation if any step is classified as disruptive;
5. stops the old daemon and records a durable reconciliation journal;
6. backs up durable data and modified managed files;
7. applies database migrations transactionally;
8. atomically publishes managed files and the canonical manifest;
9. rebuilds disposable cache state, restarts the daemon, and synchronizes it;
10. verifies storage, managed files, daemon health, and signed history.

Every stage is idempotent. The same command resumes an interrupted journal.
Safe compatible plans run automatically. Non-interactive callers receive
`UPGRADE_REQUIRED` before mutation when confirmation is required.

### Storage

Personal-authority SQLite and the local draft store use ordered migrations and
`PRAGMA user_version`. Existing drafts move from the projection cache into a
dedicated durable database before the cache becomes fully disposable.

PostgreSQL uses an ordered `schema_migrations` journal protected by an advisory
lock. Transactional compatible migrations run at service startup. Disruptive
migrations require `agent-comms-server migrate apply --yes
--allow-disruptive`. Service readiness remains false until verification.

Backups are stored below `.agent-comms/backups/<timestamp>-<upgrade-id>`.
They include changed managed files and durable databases, but never signing
keys. Three completed backups and the one active failed backup are retained.
Projection caches are rebuilt rather than backed up.

### Interfaces

The normal project command is:

```text
agent-comms project upgrade [--yes] [--all-known]
```

It owns inspection, planning, backup, application, resume, restart, and
verification. Optional read-only diagnostics are:

```text
agent-comms project upgrade status [--all-known]
agent-comms project upgrade plan [--all-known]
```

`agent-comms update apply` reconciles all known projects by invoking the new
binary after atomic replacement. `--current-project-only` is the explicit
opt-out. Distinct project roots come from identity profiles; Agent Comms never
scans the filesystem.
`--skip-project-upgrade` is an explicit escape hatch.

The existing JSON envelope stays `agent-comms/v1`. New stable errors are
`UPGRADE_REQUIRED`, `PROJECT_TOO_NEW`, `UPGRADE_UNSUPPORTED`, and
`UPGRADE_FAILED`. Existing `verify` remains the audit-chain verifier.

Daemon health includes product version, build ID, protocol, project format,
cache schema, and draft schema. Daemons are reused only when those contracts
are compatible. The TUI presents lifecycle progress and one confirmation.
MCP automatically performs safe reconciliation and exposes read-only upgrade
status; it cannot approve disruptive maintenance.

## Known gaps

- The TUI does not yet present the one-confirmation flow described above.
  Today, a plan with `RequiresConfirmation: true` simply blocks the TUI from
  launching at all (the existing `PersistentPreRunE` gate returns
  `UPGRADE_REQUIRED` before the TUI ever starts), so a disruptive action is
  never applied without the CLI's explicit `--approve`. This is safe by
  omission -- no confirmation is bypassed -- but it is not the in-TUI
  confirmation UX this document describes. Building that UX is tracked as
  future work, not part of this implementation.

## Alternatives considered

- Mutating every project from an installer was rejected because installers do
  not safely know which projects exist or are active.
- Reinitializing projects was rejected because it risks signed history,
  identity, and draft loss.
- Product SemVer alone was rejected because storage and protocol components
  evolve independently.
- Multiple normal `status`, `plan`, `apply`, `repair`, and `verify` steps were
  rejected because they expose an internal state machine to users.
- Treating drafts as cache data was rejected because drafts are unique user
  data.
- Automatic downgrade and disruptive migration were rejected in favor of
  correctness.

## Compatibility and rollout

The current post-legacy runtime is the oldest supported baseline. Legacy
engines and migration paths stay removed. Automatic migrations must remain
readable by the immediately preceding supported release. Release metadata
declares supported minimum versions and migration safety.

Implementation lands in reviewable layers: compatibility contracts and
planning, local reconciliation and storage, binary handoff, PostgreSQL
migrations, then TUI/MCP surfaces.

## Security and privacy

Runtime paths are checked with `Lstat`; symlinks and unexpected hard links are
rejected. Runtime directories remain mode `0700` and sensitive files `0600`.
Credentials and signing keys are never copied. Maintenance is not represented
as a governed event. No success is reported before final verification, and no
authoritative backup is restored automatically.

## Test and rollout plan

Fixtures cover the current baseline, stale generated files, retired manifest
fields, every later schema, corrupt and too-new projects. Tests inject failure
after every journal, backup, transaction, publication, cache, and daemon
boundary. They prove concurrent clients serialize, old daemons are replaced,
drafts survive, signed event bytes remain unchanged, retries resume
idempotently, and PostgreSQL migrations apply once across multiple instances.

Acceptance requires one-command built-in update, zero commands for compatible
external installs, at most one explicit project command for disruptive
changes, and healthy doctor, audit, cache, and daemon state afterward.

## Unresolved questions

None.
