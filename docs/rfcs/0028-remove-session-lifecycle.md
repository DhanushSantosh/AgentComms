# RFC 0028: Remove the unconsumed session-lifecycle feature

## Status

**Proposed, 2026-09-02.** Owner: Dhanush Santosh. Implementation branch:
`review/feature-validity`. Awaiting owner acceptance before implementation,
per `docs/rfcs/README.md`.

This removes a public command group and a durable state collection, so it
requires review.

## Problem and desired outcome

`agent-comms session start --id <id>` and `session end --id <id>` emit
signed `session.start` / `session.end` events. `projection` applies them
into `State.Sessions map[string]SessionPayload`, and all three authority
backends persist that collection:

- `internal/personalauthority/engine.go`
- `internal/authority/postgres.go` (a dedicated `persistSessions`,
  `session_payloads` rows, replay decode path)
- `internal/localcache/cache.go`

**Nothing reads `State.Sessions`.** A repository-wide search for a read
of `state.Sessions[...]` (as opposed to a write, delete, or map
initialization) returns nothing in `service`, `controlplane`, `doctor`,
`tui`, `worker`, `mcp`, or `protocol`. There is no `session` TUI surface.
No transition, projection rule, or delivery decision consults it. It is
write-only signed state.

RFC 0007 ("agent-runtime session binding") is a different, live mechanism
(`internal/sessionbind`, a local file that pins an invocation to a
provider conversation) and is unaffected.

RFC 0027 already removed the `session heartbeat` sub-command as a no-op.
This RFC finishes the removal: `start` and `end` are events nobody acts
on either.

Desired outcome: the session feature is gone, and the persisted
`sessions` collection is dropped from the state model on a schema bump.

## Proposed design

1. **CLI:** delete `agent-comms session` and its `start` / `end`
   subcommands (`sessionCmd` in `internal/app/cmd_invocation.go`), and its
   registration in `app.go`.
2. **Model:** remove `State.Sessions`, `model.SessionPayload`, and the
   `session.start` / `session.end` entries from the payload registry.
3. **Protocol / projection:** remove the `session.*` transitions and the
   `*model.SessionPayload` projection case.
4. **Authority backends:** remove `persistSessions` and the
   `session_payloads` handling from `postgres.go`; drop the `Sessions`
   map init from `personalauthority` and `localcache`.
5. **Schema:** bump `model.SchemaVersion`. `projectlifecycle`'s existing
   migration path handles the format change; historical `session.*`
   events in an existing signed log stay in the log (immutable) but
   project to nothing, exactly as an unknown event type already does.
6. **Postgres:** a migration drops the `session_payloads` table. Existing
   rows are audit-only and carry no information any code consumed.
7. **Docs:** remove `session` from `docs/site/guide/*`, the onboarding
   decision tree, and the generated reference.

## Alternatives considered

- **Keep it as an audit-only trail.** Rejected: `runtime register` /
  `heartbeat` / `drain` / `revoke` already record runtime presence as
  signed events, and the invocation lifecycle records work. A
  second, parallel "session" audit trail that nothing surfaces adds
  schema and three persistence paths for no readable benefit.
- **Wire it into something.** No feature has asked for it in the
  RFC history (0004–0026). Building a consumer for a speculative
  collection is the wrong direction.
- **CLI-only removal, keep the schema.** Rejected: the persistence code
  in three backends is the bulk of the cost; leaving it keeps the
  maintenance tax.

## Compatibility and rollout

Breaking: `agent-comms session *` is removed. Schema version bumps.
`CHANGELOG.md` gets a **Breaking** entry. An initialized project upgrades
through the normal `agent-comms project upgrade` path; the Postgres
migration runs on the authority service's normal migration step.

No change to authorization, approvals, task/invocation/message flows, or
the `--json` envelope for any surviving command.

## Security and privacy implications

Neutral-to-positive. Removes a signed-event type and a persisted
collection, shrinking the state model and the authority schema. No
authorization path changes.

## Test and rollout plan

- Delete `session`-specific tests; add a projection test asserting a
  historical `session.start` event in a replayed log is ignored without
  error (unknown-event tolerance).
- `projectlifecycle` upgrade test covering the schema bump.
- Postgres migration test: `session_payloads` dropped, replay of a log
  containing `session.*` events still verifies.
- Full `go test ./...` and the docs-site `check`.
- One squash-merged PR from `review/feature-validity` against `dev`.

## Unresolved questions

None.
