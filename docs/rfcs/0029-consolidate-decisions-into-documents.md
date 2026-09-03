# RFC 0029: Consolidate decision records into documents

## Status

**Proposed, 2026-09-02.** Owner: Dhanush Santosh. Implementation branch:
`review/feature-validity`. Awaiting owner acceptance before implementation,
per `docs/rfcs/README.md`.

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
6. **Migration:** `projectlifecycle` converts each existing
   `State.Decisions[id]` into `State.Documents[id]` with
   `Body = Statement`, `Tags = ["decision"]`, `Author` = the original
   event actor, preserving `Supersedes` and `Status`. Historical
   `decision.*` events stay in the immutable log and project into the
   migrated documents via the upgrade, not via replay of the old
   transition (which is gone) — the upgrade writes a single
   `document.create` catch-up event per migrated decision, signed by the
   project owner, referencing the original in its payload.
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
- **Leave the migration to replay** (re-run a shim transition). Rejected:
  keeping a dead transition alive only to migrate is worse than a
  one-time catch-up event the upgrade emits.

## Compatibility and rollout

Breaking: `agent-comms decision *` removed; `--kind DECISION` messages
unaffected. Schema bumps. Existing decisions become tagged documents
through `agent-comms project upgrade` (and the authority migration step
for team mode). `CHANGELOG.md` gets a **Breaking** entry with the
`decision create X` → `document create X --decision` mapping.

## Security and privacy implications

Neutral. Same signing, same supersession semantics, same authorization
(document mutations already revalidate authorization in the authoritative
transaction). The migration's catch-up events are owner-signed and
verifiable like any other event.

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

## Unresolved questions

1. One combined schema bump for RFC 0028 + 0029, or two? Leaning one —
   both land in the same PR, one `project upgrade` for the user.
2. Does `--decision` warrant being more than sugar for `--tag decision`
   (e.g. a first-class `Kind` field on `Document`)? Leaning no; a
   reserved tag is enough and keeps the model flat.
