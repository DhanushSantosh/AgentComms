# RFC 0012: Agent identity deletion and per-event key fingerprinting

- Status: Implemented
- Owners: Agent Comms maintainers

## Problem and desired outcome

`agent.revoke` is permanent by design: a revoked principal can never act
again, be reactivated, renamed, or suspended, and `agent.register` refuses
to reuse its ID forever (`internal/protocol/transitions.go`'s `"principal
already exists"` check, keyed only on ID, never on status). This is
deliberate -- see `docs/governance.md` -- specifically to stop an entity
from shedding a bad reputation by re-registering under the same, previously
flagged name. The only way to get an ID back today is a numbered suffix
(`alpha-2`).

That's real friction with no relief valve. The desired outcome is a
narrow, explicit way to permanently delete a *revoked* agent's identity so
its ID becomes available for reuse by an unrelated future principal --
without reopening the impersonation risk the current permanence exists to
prevent. Concretely: given any historical task, message, or invocation
attributed to an ID, it must remain possible to tell *exactly* which
physical key performed it, even if that ID has since been deleted and
reused by someone else entirely.

The missing piece, confirmed by reading the actual code rather than
assuming: `controlplane.Event` -- the durable, hash-chained record that is
never modified after commit -- carries no field recording which key signed
it. A `KeyFingerprint` field does exist, but only on `model.Event`, the
CLI-facing response envelope, computed client-side right after signing
(`internal/service/service.go`) and never persisted; it's visible once, in
the response to whoever just performed that action, then gone.
`Receipt.KeyFingerprint` is a different thing entirely -- the *authority's*
own signing key, proving the receipt itself is authentic, not the actor's.
So today, replaying history for an ID that changed hands would show no
durable way to distinguish "old occupant" from "new occupant" work.

Note this is not a hash-collision problem. Ed25519 public keys are
256-bit, generated via `ed25519.GenerateKey(rand.Reader)` -- two agents
ever landing on the same key is already astronomically impossible by
construction. `identity.Fingerprint()` (SHA-256 truncated to 64 bits,
`internal/identity/identity.go`) is already a strong, standard hash; it's
sufficient for display and comparison at any realistic scale. The gap is
purely that nothing durable records *which* fingerprint acted on *which*
event.

## Proposed design

Two independent, sequenced pieces: persist the acting key's fingerprint on
every future event (a prerequisite, since deletion is meaningless without
it), then add the delete transition itself.

### Part 1: Persist the actor's key fingerprint per event

Add `ActorKeyFingerprint string` to `controlplane.Event`
(`internal/controlplane/contracts.go`), populated by both authority
backends at the exact point signature verification succeeds -- not
self-reported by the caller, not derived after the fact. In
`internal/personalauthority/engine.go`'s `Mutate` and
`internal/authority/postgres.go`'s `Mutate`, right after
`commandPublicKey` resolves the key and `command.Verify(publicKey)`
succeeds:

```go
event.ActorKeyFingerprint = identity.Fingerprint(publicKey)
```

This ties the field to a cryptographically verified fact (this exact key
really did produce this exact signature), which is what makes it trustable
as the disambiguator later -- a field the *client* set on the command
would just be a claim.

**This field must become part of `HashEvent`'s canonical hash**
(`eventCanonical` in `internal/controlplane/contracts.go`), or a rogue
database admin could rewrite a historical fingerprint undetected, defeating
the entire point. This is a hash-format change and needs the exact
rigor RFC 0011's fix pass applied to the `Legacy bool` field, which sets
the precedent to follow directly:

```go
type eventCanonical struct {
    // ... existing fields ...
    ActorKeyFingerprint string `json:"actor_key_fingerprint,omitempty"`
}
```

`HashEvent` is a pure function of the `Event` struct. Every event
already committed has no fingerprint recorded (the column/field will be
empty for them). Because the JSON tag is `omitempty` and the Go zero
value for a string is `""`, marshaling `eventCanonical` for any event with
an empty `ActorKeyFingerprint` **omits the field from the JSON entirely**,
producing byte-identical canonical JSON to today -- so every event
committed before this ships keeps verifying against its existing stored
hash, unchanged. Only events committed *after* this ships, which will
always have a non-empty fingerprint (both backends resolve one before
ever committing), get the field included and therefore a materially
different hash than they would have gotten before. This is the same
trick already used for `Legacy` (`strings.HasPrefix(event.IdempotencyKey,
"legacy:")`, itself `omitempty` on a bool whose zero value is `false`) --
copy that pattern exactly, do not improvise a different one.

This is a real, if small, admission: fingerprint disambiguation only
applies going forward. It cannot be retroactively backfilled for events
already committed (there is no cryptographically trustworthy way to
determine after the fact which of several historical key-rotation windows
actually signed a specific old event without re-deriving it from
sequence-range heuristics, which is exactly the kind of unverified
inference this RFC is trying to avoid relying on). Given this project's
history is still young, treat this as acceptable, not as a gap to solve
here.

Surface the new field in read paths: `agent-comms history`,
`agent-comms search`, and `EventRecord` output over MCP should include
`actor_key_fingerprint` per event once populated. Consider a
`--key-fingerprint` filter on `history`/`search` so a human can isolate
one physical identity's actions across an ID-reuse boundary directly,
rather than eyeballing it. Note explicitly in that output/docs that
multiple distinct fingerprints under one ID is *already normal* today via
`agent.rotate-key` -- the new signal isn't "one ID, one fingerprint," it's
"every individual event has a precise, queryable, tamper-evident answer."

### Part 2: `agent.delete`

New event type `agent.delete`, payload `AgentDeleted{Reason string}`
(mirrors `RuntimeStatusChanged`'s shape, used by revoke today).

Validation (`internal/protocol/transitions.go`), a new block alongside the
existing `agent.revoke` one:

- Target must exist and have `Status == "REVOKED"`. Deleting an
  `ACTIVE` or `PENDING` principal is rejected outright -- revoke and
  delete stay two deliberate, separate steps; there is no combined
  "revoke-and-delete."
- No self-delete case: a principal can only be deleted once already
  revoked, and a revoked principal fails `active()` on every transition
  including this one, so `id == actor` is naturally unreachable here
  without an extra check.
- Require a HUMAN principal unconditionally, regardless of the target's
  prior role -- deliberately stricter than revoke, which only requires
  HUMAN for an orchestrator/human target. Deletion destroys the "this ID
  was flagged" signal permanently; even deleting a long-revoked plain
  AGENT should not be a decision an AGENT-principal orchestrator can make
  alone.
- Add `agent.delete` to `protocol.RequiresElevatedKey`'s classification
  (`internal/protocol/transitions.go`), so once the acting human has
  registered an elevated key, deletion needs that signature too, exactly
  like granting ORCHESTRATOR or revoking one. Thread this through both
  authority backends' `commandPublicKey`/`scopedElevationState`
  (`internal/personalauthority/engine.go`,
  `internal/authority/postgres.go`) and the client-side
  `Service.elevateCredentialIfNeeded`
  (`internal/service/service.go`) the same way `agent.revoke` was
  threaded through in the prior fix pass -- `scopedElevationState` will
  need a new `case "agent.delete":` fetching the target agent's row, the
  same shape as its existing `agent.revoke` case.

Reducer (`internal/projection/apply.go`): `delete(s.Agents, e.EntityID)`.
No cascade logic needed beyond this -- by the time a principal reaches
`REVOKED`, `agent.revoke`'s existing cascade has already revoked its own
runtimes (`TestAgentRevokeCascadesToOwnRuntimes`); tasks, messages, and
invocations it touched keep referencing its ID string in history, which is
correct and expected.

New CLI command `agent-comms agent delete --id <id> --reason <reason>`
(`internal/app/app.go`, mirrors `revoke`'s shape in `agentCmd()`). **No
MCP tool** -- same reasoning as `agent elevate-key`: this requires the
elevated key, an MCP connection has no interactive terminal to answer that
prompt with, and exposing it would be pointless at best.

`agent.register`'s existing check (`internal/protocol/transitions.go`,
`if _, exists := st.Agents[id]; exists { reject }`) needs no change --
once the entry is gone from state, registration under that ID succeeds
automatically.

### Registering the new event type

Follow the three-registry pattern this codebase already requires for any
new event type (`internal/model/payloads.go`'s `payloadFactories`,
`internal/projection/apply.go`'s `ApplyEvent` switch,
`internal/authority/postgres.go`'s own manual `decodePayload` switch --
`internal/personalauthority`'s equivalent is generic via reflection and
needs no case). The regression test added for exactly this failure mode,
`internal/authority/decode_payload_test.go`'s
`TestDecodePayloadCoversEveryRegisteredEventType`, will fail loudly if the
Postgres case is missed -- run it before considering this done.

## Alternatives considered

- **Keep the numbered-suffix convention as the only answer.** Rejected --
  it's the status quo this RFC exists to improve on; explicitly requested
  against.
- **Delete the underlying events.** Rejected outright, not just
  undesirable: the event log is hash-chained (`PreviousHash`) and signed:
  removing an event invalidates every subsequent event's hash. Not a
  design choice, a cryptographic impossibility without a full history
  rewrite.
- **Retroactively backfill `ActorKeyFingerprint` for existing events** by
  reconstructing key-validity windows from each actor's
  `agent.register`/`agent.rotate-key`/`agent.elevate-key` event sequence
  ranges. Rejected: this is an *inference*, not a verified fact -- unlike
  the field populated at commit time directly from a successful signature
  check, a reconstructed value could be wrong (e.g. if an ID was ever
  deleted and reused before this RFC existed, which is exactly the
  ambiguous case in question) with no way to detect the error.
- **Store the full public key instead of a truncated fingerprint.** Viable,
  zero collision risk by construction either way. Recommend the
  fingerprint for consistency with the existing `KeyFingerprint`/
  `ElevatedKeyFingerprint` display convention already used throughout
  `status`/`doctor`, at the cost of a few dozen bytes; not a strong
  preference, revisit if a concrete need for the full key surfaces.

## Compatibility and rollout

- `internal/authority` (Postgres): `events.actor_key_fingerprint TEXT`
  is a real column addition needing a new entry in
  `internal/authority/schema.go`'s `schemaMigrations` list
  (`{Version: 2, Name: "actor-key-fingerprint", Automatic: true, SQL:
  "ALTER TABLE events ADD COLUMN IF NOT EXISTS actor_key_fingerprint TEXT
  NOT NULL DEFAULT ''"}` or equivalent) -- a nullable/defaulted column add
  is non-disruptive, mark it `Automatic: true` per the classification
  scheme the RFC 0011 fix pass established.
- `internal/personalauthority` (SQLite/personal mode): events are stored
  inside a single serialized blob per project row -- the new struct field
  serializes automatically, no migration needed, matching how
  `ElevatedPublicKey` was added to `model.Agent` earlier without one.
- No change needed to `internal/localcache`'s cache-rebuild path beyond
  whatever naturally follows from the new `Event` field replaying through
  the existing `projection.ApplyEvent`/cache pipeline.
- Existing tests that construct `controlplane.Event`/`eventCanonical`
  values directly (if any) should be checked for hash-equality assumptions
  that assumed the old field set; none should break given `omitempty`, but
  verify rather than assume.

## Security and privacy

- Deletion is more consequential than revocation (it destroys the "this
  identity was flagged" signal permanently, not just future capability),
  so it is gated at least as strictly: human-only, unconditionally, plus
  the elevated key once registered -- stricter than revoke's
  orchestrator-target-only human check.
- Fingerprints are not secret; they're already shown in `status`/`doctor`
  output today. Persisting one more per event introduces no new privacy
  exposure.
- The hash-format change is the highest-risk part of this RFC by a wide
  margin -- get the `omitempty`/derivation-at-hash-time mechanics wrong
  and every pre-existing event's verification silently breaks. This is
  not hypothetical: RFC 0011's own fix pass had to correct exactly this
  mistake once already for the `Legacy` field (finding 4). Any
  implementation of this RFC must include a test that hashes a
  pre-this-RFC-shaped `Event` (constructed with the old field set only)
  and asserts the resulting hash is byte-identical to what `HashEvent`
  produces for that same event today, before touching anything else.

## Test and rollout plan

- `internal/controlplane`: `HashEvent` byte-compatibility test (old field
  shape hashes identically with and without the code change present);
  a new-event test proving a populated `ActorKeyFingerprint` changes the
  hash and that tampering with a stored fingerprint is caught as a hash
  mismatch on replay/verify.
- `internal/protocol`: `agent.delete` validation tests -- rejects
  `ACTIVE`/`PENDING` targets, rejects a non-human actor unconditionally
  (including for a plain revoked AGENT target, not just
  orchestrator/human), succeeds for a human actor against a REVOKED
  target. `RequiresElevatedKey` classification test for `agent.delete`.
- `internal/personalauthority` and `internal/authority` (Postgres,
  integration-gated): the same primary-key-rejected-once-elevated-key-
  registered regression shape already used for `agent.activate`/
  `agent.revoke`, applied to `agent.delete`; a live round-trip proving a
  deleted ID can be re-registered by an unrelated new principal, and that
  the old identity's historical events retain their original
  `ActorKeyFingerprint`, distinct from the new registrant's.
- `internal/service`, `internal/app`, `internal/mcp`: `ElevateKey`-gated
  passphrase-prompt tests for the new CLI command, mirroring
  `agent elevate-key`'s own test shape; confirm no MCP tool exists for
  `agent.delete` (mirrors the existing `approval_approve`-is-not-a-tool
  assertion pattern).
- Live verification (matching this project's standing preference for
  validating security-shaped claims against a real project, not just
  unit tests): revoke a real throwaway agent, delete it, re-register a
  different agent under the identical ID, and confirm `agent-comms
  history --actor <id>` shows two distinct `actor_key_fingerprint` values
  across the boundary with no way to conflate them.

## Unresolved questions

The first implementation deliberately has no cool-down. Revoke and delete
remain separate signed commands, while operators that need a waiting period
can enforce one procedurally without introducing hidden clock-dependent
behavior into the base protocol.

`doctor` does not flag rapid revoke-delete-register cycles in this release.
The history and search commands expose the tamper-evident fingerprint
boundary directly; a heuristic warning can be added later if operational
evidence shows it is useful.

`AgentDeleted` contains only the required audit reason. The deleted
projection is reconstructable from the preceding event history, so copying a
mutable snapshot into the deletion payload would add a second representation
without strengthening integrity.

## Implementation notes

- PostgreSQL schema version 2 adds the non-null
  `actor_key_fingerprint` column with an empty default; migration version 1
  is unchanged so existing checksum records stay valid.
- `history` supports `--actor` and `--key-fingerprint`; `search` supports
  `--key-fingerprint`. Filters apply to the bounded page selected by
  `--cursor` and `--limit`.
- `agent delete` requires an explicit non-empty `--reason`. It is CLI-only
  and uses the elevated-key passphrase path when the acting HUMAN principal
  has registered one.
- No deletion cool-down or deleted-agent payload snapshot is part of this
  release.
