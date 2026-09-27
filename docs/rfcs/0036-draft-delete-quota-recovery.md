# RFC 0036: `agent-comms draft delete` and local draft quota recovery

## Status

**Accepted, 2026-09-24.** Implemented by claude-main under task
`release-draft-delete-20260924`. The project owner accepted the single-draft
CLI design: an unknown ID errors, and bulk deletion and expiry are out of
scope. Integration review and verification remain required before merge.

## Problem and desired outcome

Local drafts are bounded three ways in `internal/controlplane/contracts.go`:

- `MaxDraftBytes` (5 MiB) — the largest single draft.
- `MaxDraftsPerProject` (1,000) — the per-project count cap.
- `MaxDraftStorageBytes` (50 MiB) — the per-project byte cap.

Both `internal/draftstore.Store.SaveDraft` and
`internal/localcache.Cache.SaveDraft` enforce the count and byte caps by
recomputing `COUNT(*)` and `SUM(LENGTH(body))` over the project's rows on
every save, refusing the write with `CodeRateLimited` when either cap would
be exceeded.

**Drafts never expire, and until now there was no delete path.** The
stores exposed `SaveDraft`, `ImportDraft`, and `Drafts` and nothing else,
and the CLI exposed `draft save`, `draft list`, and `draft show`. A project
that reached either cap was permanently stuck: every subsequent
`draft save` failed with "local draft count limit reached" and the only
recovery was deleting `drafts.db` by hand, which discards every draft in
every project sharing that store.

Desired outcome: a user who hits a draft quota can release it by deleting
the drafts they no longer want, without leaving the CLI and without
destroying unrelated drafts.

## Design

One new verb, threaded through the existing draft chain unchanged:

```
agent-comms draft delete --id <draft-id>
```

1. **`internal/draftstore.Store.DeleteDraft(ctx, projectID, draftID)`** —
   `DELETE FROM drafts WHERE project_id=? AND draft_id=?`. Keyed on the
   pair, matching the table's `PRIMARY KEY (project_id, draft_id)`.
2. **`internal/localcache.Cache.DeleteDraft`** — an exact mirror. The two
   stores back the same CLI surface in service and personal mode
   respectively, so any behavioural difference would show up as the same
   command behaving differently depending on whether a daemon is running.
3. **`internal/wasmdemo.MemoryCache.DeleteDraft`** — the same contract over
   the in-memory slice. Required, not optional: the WASM demo passes
   `*MemoryCache` as `daemon.draftStore` and `daemon.cacheStore`
   (`cmd/agent-comms-tui-wasm/bootstrap.go`), so both interfaces gaining a
   method obliges it.
4. **Daemon** — `DELETE /v1/projects/{project}/drafts/{draft}`, returning
   `{"deleted": true, "authoritative": false}`, with `DeleteDraft` added to
   the `draftStore` and `cacheStore` interfaces.
5. **`internal/daemonclient.Client.DeleteDraft`** — `url.PathEscape` on both
   path segments, matching the existing draft methods.
6. **`internal/service.Service.DeleteDraft(id)`** — resolves the project ID
   from config and delegates, with the same `remoteErr` / "drafts require
   authoritative service mode" guards `SaveDraft` and `Drafts` use.
7. **CLI** — `draft delete --id`, emitting a `draft.delete` document whose
   hint states that deleting frees quota.

### Deleting an absent draft is an error

`DeleteDraft` returns `controlplane.Error{Code: CodeValidation}` with
`draft %q not found` when no row matched, rather than succeeding silently.

Two reasons. First, it matches `draft show`, which already errors on an
unknown ID. Second, a silent success hides a mistyped ID behind an exit
code of 0, which for a recovery command is the worst outcome: the user
believes they freed quota and has not.

`CodeValidation` rather than a new `CodeNotFound`: `internal/controlplane`
defines no not-found code today, and adding one to the shared error
vocabulary is a wider change than this task warrants. If a future RFC
introduces `CodeNotFound`, this call site should move to it.

### Scope: single-ID only

No `--all`, no glob, no prune-by-age. Quota recovery does not need bulk
deletion, because both stores recompute usage per call — deleting one
draft immediately frees exactly its count and bytes. A bulk verb is a
larger decision (confirmation semantics, dry-run, cross-project reach) and
is deliberately left out.

## Alternatives considered

**Automatic expiry.** A TTL on drafts would bound growth without user
action, but it deletes user work on a timer. Drafts are explicitly
non-authoritative scratch space the user chose to keep; silently
discarding them is worse than an error telling them they are at the cap.
Expiry remains available as a later, additive option.

**Evicting the oldest draft on save.** Makes `draft save` destructive and
surprising: a save that succeeds by quietly deleting something else is a
data-loss bug wearing a convenience feature's clothes.

**Idempotent delete (absent ID succeeds).** Conventional for HTTP DELETE,
and rejected here for the exit-code reason above. The daemon route still
reports the store's error rather than a blanket 200.

## Compatibility and rollout

Purely additive. No schema change — no migration, and `SchemaVersion`
stays 1. Existing commands, routes, and stored drafts are untouched. An
older client against a newer daemon is unaffected; a newer client against
an older daemon receives 404 from the unknown route, which surfaces as a
command error rather than silent success.

## Security and privacy implications

Deletion is scoped to `(project_id, draft_id)`, so it cannot reach drafts
belonging to another project that reuses the same draft ID — covered by a
regression test in every store. Drafts are local and non-authoritative:
deleting one writes no event to the signed chain and cannot alter project
authority. No new filesystem paths, permissions, or network surface.

## Test and rollout plan

- `internal/draftstore`: delete removes only the named draft; project
  isolation; absent ID returns `CodeValidation`; **quota recovery** —
  fill `MaxDraftsPerProject` exactly, assert the next save is refused,
  delete one, assert the same save now succeeds.
- `internal/localcache`: mirror, including project isolation and the
  absent-ID error.
- `internal/wasmdemo`: delete removes it; deleting twice errors.
- `internal/daemon`: the DELETE route end to end through the real mux,
  using a draft ID containing `/` so path escaping is actually exercised;
  second delete must not return 200.
- `internal/app`: the full CLI chain — save two, delete one, confirm the
  other survives in `draft list`, confirm a mistyped ID exits non-zero,
  and confirm `delete` without `--id` is refused rather than becoming a
  bulk operation.

## Decisions on follow-ups

1. Keep `CodeValidation` for an absent draft in this release. It matches
   `draft show` and avoids introducing a new shared error code for one
   command. A future, coherent not-found error taxonomy may change both
   commands and the daemon's HTTP mapping together; that is not a prerequisite
   for quota recovery.
2. Keep deletion single-ID only. `draft list` plus per-ID deletion is enough
   to recover quota without a destructive bulk action. Bulk or age-based
   pruning would need its own explicit design for confirmation, dry-run, and
   project scope; it is not part of this release.
