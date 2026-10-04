# RFC 0041: Event-derived entity timestamps and useful list order

## Status and owners

**Accepted, 2026-10-02.** Owner: the project maintainer. Drafted by codex-main and
reviewed by claude-main. The owner accepted the design through the governed
Agent Comms decision `msg-rfc0041-accepted-20261002`; the review decisions in
document `rfc0041-review-20261002` are incorporated below.

## Problem and desired outcome

`message inbox` sorts lexicographic IDs and then applies `--limit`. IDs may be
generated or supplied by callers, so a recent custom-ID message can be hidden
by an older generated ID. The state also lacks a posting time for messages;
other entities expose timestamps inconsistently. This makes the CLI, MCP, and
TUI present activity in misleading order and prevents users from distinguishing
creation from later status changes.

The outcome is a truthful, deterministic chronological view based on the
signed authoritative event stream, without trusting client-supplied times or
changing the meaning of existing event payloads.

## Proposed design

1. Project `created_at` and `updated_at` from the creating and latest relevant
   signed event's `Time`. Persist corresponding creation and update sequence
   numbers in projection storage. Use sequence for causal ordering when clock
   times tie or move backward; show the signed time to users. Do not derive
   time from entity IDs or the local process clock. Existing per-recipient
   message response `at`, invocation lifecycle times, runtime `registered_at`
   and `last_seen_at`, and local draft timestamps keep their meanings.
2. Add times to messages, tasks, agents, approvals, documents (including
   historical decisions), artifacts, runtimes, and invocations. An artifact is
   immutable, so only `created_at` applies. Add `created_at` to environment
   entries and invocation policies while retaining their existing
   `updated_at`; a delete followed by a new set begins a new lifetime.
   Project settings already have `updated_at` and do not need a fictitious
   creation time. Local drafts already have both times in their own store;
   they are not signed events and must not be described as such.
3. For each transition, update only the entity actually changed. Cascades
   such as task unblock on message resolution, agent-revoke runtime cascade,
   approval consumption, document supersession, and task archival must update
   affected entities too. Creation times never change within an entity
   lifetime; re-creation after deletion gets a new creation time.
4. `message inbox` orders posted messages by creation sequence descending,
   then stable ID, before applying `--limit`. A recipient's later ack does not
   make an old message appear newly posted. Other collection lists default to
   latest relevant update sequence descending, then stable ID, to surface
   recent activity. Preserve existing filters before ordering and limits
   after ordering. Do not claim that JSON object-map key order conveys list
   order: CLI list envelopes retain their existing result map and add an
   optional top-level `order []string` (`omitempty`) containing exactly the
   displayed IDs after filtering and limiting. MCP list results retain their
   structured-content map and add the same IDs at `_meta.order`. Never add a
   synthetic `order` entry inside the entity map, where a real ID could
   collide. The CLI and MCP order arrays must be equal for equivalent calls.
5. Expose `created_at` and `updated_at` as RFC 3339 timestamps in CLI
   detail and narrow-safe table views, JSON, MCP results, and TUI detail
   panels. The TUI may abbreviate list cells but must retain full values in
   inspection. Empty or legacy values must never be rendered as year 0001.
6. Rebuild pre-existing projections from signed events in sequence order,
   atomically per project before returning timestamped state. PostgreSQL v7
   is an automatic additive schema change for nullable time/sequence fields
   and indexes. PostgreSQL v8 is a disruptive Go replay of each project's
   signed events through the same projection logic under the existing
   migration advisory lock, gated by `allowDisruptive` and operator
   confirmation. The authority refuses timestamped list requests until v8
   completes; an older binary must reject a newer schema. Personal authority
   backfill uses the confirmation-required lifecycle upgrade with backup.
   The local projection cache can be invalidated and replayed through that
   lifecycle. The WASM demo projection must likewise be rebuilt. A project
   with missing, corrupt, or unverifiable history fails migration rather than
   publishing guessed timestamps. No signed event is rewritten. This is a
   derived-state migration, not a new event payload version.

## Implementation notes (2026-10-02)

Refinements made while implementing, within the accepted design:

- **One PostgreSQL migration, not two.** Entity state is stored as JSONB, so
  new events already persist the new fields and list ordering happens on the
  loaded state; the planned additive v7 (columns and indexes) had nothing to
  add. Migration 7 is the replay backfill. It is disruptive (requires
  `agent-comms-server migrate apply --yes --allow-disruptive`, and normal
  startup refuses until it has run) only when there is history to replay; a
  fresh database applies it automatically. It locks every project row for
  its duration, verifies each project's full hash chain, refuses when the
  stored entities differ from what history produces, and rewrites only the
  JSONB state of entities whose times changed. The server now also refuses a
  database migrated by a newer binary.
- **MCP gained `message_inbox`.** MCP had no list tools, so `_meta.order` had
  no result to attach to. `message_inbox` is actor-bound (unread, from,
  limit), returns the message map as structured content and the order in
  `_meta.order`, and shares its filtering with the CLI so both agree.
  `status` is unchanged.
- **Personal authority and cache.** The personal authority database moves to
  version 2 and the projection cache to 4. `project upgrade` (after its
  existing backup) replays each project's verified events from an empty
  state. An unverifiable authority history fails the upgrade and leaves the
  snapshot untouched; an unverifiable cache project is dropped so the daemon
  refetches it. The personal authority now refuses to open an older database
  instead of re-stamping it, which would have skipped the backfill.
- **TUI.** Lists follow the same order and keep the selected entity selected
  when rows reorder. The messages list is strictly newest first, matching
  `message inbox`; messages needing a response remain surfaced in the
  overview's attention panel, which is now also ordered by recency.

## Alternatives considered

- Sort IDs or parse timestamps from generated IDs. Rejected: custom IDs and
  caller control make both methods incorrect.
- Add only message times. Rejected: it fixes the incident but leaves other
  collection views misleading and forces later parallel migrations.
- Use process-local `time.Now()` on read. Rejected: it is not durable,
  authoritative, or replay-stable.
- Add cursor pagination in this RFC. Deferred: it addresses a separate
  unpaginated-state-list finding but changes the listing interface and remote
  service contract substantially. This RFC must not imply that ordering alone
  makes a large list bounded.

## Compatibility and rollout

The event log and command signatures are unchanged. New projection fields and
structured result fields are additive, but displayed list order changes
intentionally. Existing programs that assume ID order must adapt. Projection
backfill can be expensive for large projects; it must have progress/error
reporting and be tested against both personal and shared authorities. Older
binaries must not serve a newer projection as if its schema were current.
Document the one-time upgrade and event-count-proportional downtime in release
notes and generated CLI reference. A later v2 list interface may use the
ordered `{items, next_cursor}` shape with cursor pagination, but that is not
part of this RFC.

## Security and privacy implications

Times reveal activity cadence to principals already authorized to view the
project. They must not bypass existing state access controls. Signed event
time and sequence avoid spoofable client time and preserve audit integrity.
Migration must verify event continuity and use the established transactional
backup/recovery path before replacing derived state. No raw message body or
secret environment value is added to list displays.

## Test and rollout plan

- Start with a red regression: mixed generated/custom IDs where the latest
  message would previously be omitted by `message inbox --limit 1`. Assert
  human rows, structured ordered result, and recipient filtering.
- Projection tests: creation/update/cascade times, deterministic replay,
  same-time and backward-clock event sequences, delete/recreate lifetimes.
- Migration tests: old personal snapshot, old PostgreSQL normalized state,
  old local cache, interruption/restart, and corrupted history. Compare all
  backends after replay with the same event sequence.
- CLI/MCP/TUI parity: verify every listed entity's times and ordering, plus
  narrow TUI layouts and timestamp inspection. Extend the release parity
  test rather than asserting only one transport.
- Run focused package tests and race checks, then `go test ./...`,
  `go vet ./...`, and the PostgreSQL integration suite before release.

## Unresolved questions

None. The two review questions were resolved before owner acceptance: additive
CLI envelope and MCP metadata order, plus PostgreSQL v7/v8 migration split.
