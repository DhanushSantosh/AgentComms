# RFC 0029: Consolidate decision records into documents

## Status

**Implemented, 2026-09-02.** Owner: Dhanush Santosh. Implementation branch:
`review/feature-validity`. The project owner accepted this before
implementation began, per `docs/rfcs/README.md`.

Implemented: the `decision` group is gone, `document create --decision`/`--notify` replaces it, historical `decision.*` events project into `decision`-tagged documents, the projection-cache version bumped to 4 (local rebuild) and Postgres migration 6 folds the `decisions` table into `documents`. State schema 2.2.0.

Removes a public command group and a durable state collection; requires
review.

## Problem and desired outcome

Agent Comms has three overlapping ways to record a durable, signed,
supersedable statement:

| Mechanism | Shape | CRUD |
| --- | --- | --- |
| `decision create` / `decision supersede` | `model.Decision{ID, Title, Statement, Supersedes, Status, To}` | create + supersede only — **no `list`** |
| `document create/update/supersede/list/show` | `model.Document{ID, Title, Body, Tags, Status, Version, Author, Supersedes}` | full |
| `message post --kind DECISION` / `--kind CONTRACT` | a `Message` with an obligation | post + ack/resolve |

`Decision` is a strict subset of `Document` (Statement ≈ Body) plus one
field, `To` (principals expected to acknowledge). It has the weakest
surface: you can create a decision but not enumerate decisions from the
CLI. RFC 0027 added `decision show`; it did not fix the missing `list`
because this RFC removes the type instead.

Desired outcome: one governed-document type. A "decision" is a document
you tag as such; acknowledgement is what `message post --kind DECISION`
already does.

## Proposed design

1. **Represent decisions as documents.** `document create` gains an
   optional `--decision` flag (equivalent to `--tag decision`) and an
   optional `--notify <principal>` (repeatable) that, on create, also
   posts a `message --kind DECISION --to <principal>` referencing the new
   document — reproducing `Decision.To`'s acknowledgement semantics with
   the mechanism built for obligations.
2. **Remove the `decision` command group** (`create`, `supersede`, and
   the RFC-0027-added `show`) from `internal/app/cmd_message.go` and its
   registration.
3. **Model:** remove `State.Decisions`, `model.Decision`,
   `model.DecisionPayload`, and `decision.create` / `decision.supersede`
   from the payload registry.
4. **Protocol / projection:** remove the `decision.*` transitions and
   projection case.
5. **Authority backends:** remove decision persistence from
   `postgres.go`, `personalauthority`, `localcache`.
6. **Migration.** Two mechanisms, both idempotent:
   - **Projection** (`internal/projection/apply.go`): historical
     `decision.create` / `decision.supersede` events stay in the
     immutable log and now project into a `decision`-tagged
     `Document` -- so any fresh replay (new project, cache rebuild,
     `verify`) is already correct. The `DecisionPayload` type and its
     payload-registry entries are kept for decoding those historical
     events; only the `decision.*` *transition* is removed, so no new
     ones can be created.
   - **Snapshot** (`projectlifecycle.foldDecisionsIntoDocuments`):
     personal-authority and projection-cache SQLite DBs keep an
     incremental `state_json` snapshot that is not replayed on load.
     `migrateDatabases` rewrites each snapshot in place, moving the
     `decisions` map into `documents` as tagged entries. Triggered by
     the `model.SchemaVersion` bump; a no-op on a snapshot with no
     `decisions` key. `Author` is unknown from a snapshot decision and
     left empty.
   - **Postgres** (team mode): schema migration 6 folds the `decisions`
     table into `documents` and drops it.
7. **TUI:** delete `internal/tui/decisions.go`; the Documents view
   (`documents.go`) filters by the `decision` tag for an equivalent
   "decisions" listing.
8. **Schema:** bump `model.SchemaVersion`.
9. **Docs:** `docs/site/guide/governance.md` and `records.md` updated;
   generated reference regenerated.

## Alternatives considered

- **Keep `decision`, just add `decision list`.** Rejected: completing a
  redundant type entrenches the overlap. Three ways to say the same thing
  is the problem, not the missing verb.
- **Remove `document`, keep `decision`.** Rejected: `document` is the
  more capable type (body, tags, versioning, author) and has the
  complete surface.
- **Fold both into messages.** Rejected: messages are transient
  obligations with an inbox lifecycle; documents are durable reference
  state. They are genuinely different and both are needed.
- **Catch-up events** (the upgrade emits a `document.create` per
  migrated decision). Rejected during implementation: a derived-state
  system already replays `decision.*` through the changed projection, so
  the only gap is the non-replayed `state_json` snapshot, which an
  in-place fold handles without adding events to a signed log.
- **Keep a `decision.*` shim transition alive only to migrate.**
  Rejected: worse than the in-place fold.

## Compatibility and rollout

Breaking: `agent-comms decision *` removed; `--kind DECISION` messages
unaffected. Schema bumps. Existing decisions become tagged documents
through `agent-comms project upgrade` (and the authority migration step
for team mode). `CHANGELOG.md` gets a **Breaking** entry with the
`decision create X` → `document create X --decision` mapping.

## Security and privacy implications

Neutral. Same signing, same supersession semantics, same authorization
(document mutations already revalidate authorization in the authoritative
transaction). No events are added to any signed log by the migration --
the immutable `decision.*` history stays exactly as it was and still
verifies; only the derived snapshot is rewritten.

## Test and rollout plan

- Migration test: a project with N decisions (including a supersede
  chain) upgrades to N tagged documents with the chain preserved; the
  event log still verifies.
- `document create --decision --notify` posts the DECISION message and
  creates the tagged document atomically.
- Delete `decision`-specific CLI/protocol/projection tests; add document
  coverage for the new flags.
- TUI test: the Documents view filters to `decision`-tagged entries.
- Full `go test ./...`, docs-site `check`, generated reference `--check`.
- One squash-merged PR from `review/feature-validity` against `dev`
  (may be the same PR as RFC 0028 — both are "feature-validity" removals
  with schema bumps and share the migration surface).

## Resolved questions

1. One combined schema bump for RFC 0028 + 0029, landing in one PR with
   one `project upgrade` for the user.
2. `--decision` is sugar for `--tag decision`; `decision` is a reserved
   tag. No `Kind` field is added to `Document`.
