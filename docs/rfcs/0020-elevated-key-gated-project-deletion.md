# RFC 0020: Elevated-key-gated project deletion

## Status

**Accepted 2026-08-14**, implementation follows in the same session. Per `docs/rfcs/README.md`
and `docs/development-workflow.md`'s design-proposal rule, reviewed and accepted before
implementation began. Scope confirmed directly with the project owner beforehand: deletion
covers local *and* remote state unconditionally in service mode, no automatic backup is taken,
and the command is reachable from both the CLI and the TUI.

## Context

There is currently no way to fully retire an Agent Comms project. `agent-comms project upgrade`
inspects, backs up, and reconciles a project going forward; nothing tears one down. A user who
wants a project gone today has to know, and get right, several independent pieces of state:

- The local runtime directory (`.agent-comms/`, `store.Runtime`) and the `.agents` bootstrap
  descriptor at the project root.
- Any daemon currently running against it (`config.DaemonEndpoint`).
- This project's entries in the *global*, cross-project identity profile store
  (`identity.UserConfig.Profiles`, and `ActiveProfile`/`ActiveProfileBySession` if either points
  at one of them) -- shared by every Agent Comms project on the machine, not scoped to any one
  project directory.
- Every locally-cached credential and elevated key for this project in the shared OS keyring
  (`identity.KeyringStore`, keyed by `(project_id, actor)` and `(project_id,
  identity.ElevatedActor(actor))`).
- In **service** mode, this project's entire row set in the shared Postgres authority --
  `agents`, `tasks`, `messages`, `invocations`, `approvals`, `documents`, `artifacts`,
  `environment_entries`, `sessions`, `events`, and every other `project_id`-scoped table
  (`internal/authority/schema.sql`) -- which a project's own credentials have no direct access
  to delete; only the authority server can.

Manually chasing all of this is error-prone and, for the Postgres case, not even possible from
the client side at all today -- there is no delete endpoint. This RFC adds one governed command,
`agent-comms project delete`, that does the whole thing atomically and correctly, gated the same
way this project already gates its other most sensitive actions: an elevated-key passphrase,
proving a human is physically present and intends this.

## Proposed design

### Authorization

Deletion is **OWNER-only**, checked from live state, not just "an elevated key happens to be
registered" -- mirrors the existing absolute protection `agent suspend`/`agent revoke`/every role
transition already give the owner (RFC 0018, RFC 0019's Alternatives). An elevated key must
already be registered (`agent elevate-key`); if none is, the command refuses outright with a
pointer to register one first, the same gap `doctor`'s `NO_ELEVATED_KEY` finding already
surfaces.

`internal/service/service.go`'s existing elevated-key decrypt path (used today by
`ExecuteWithPassphrase` for `protocol.RequiresElevatedKey` transitions) is factored into a small
reusable helper, `decryptElevatedKey(actor, passphrase string) (identity.Credential, error)`,
used by both the existing callers and the new one -- no parallel crypto implementation.

Once decrypted, the elevated key signs the authorization -- reusing `controlplane.Command` itself
as the envelope (`Type: "project.delete"`, `ProjectID`, `Actor`, `EntityID: ProjectID`,
`IdempotencyKey`, `IssuedAt`), rather than a bespoke signed type: `Command.Sign`/`Command.Verify`
already implement exactly the canonical-hash-then-Ed25519-sign/verify this needs, and every other
signed write in the system already produces and checks this exact shape, so there is no parallel
crypto envelope to keep in sync with the real one. This signature is the proof of human intent
for the rest of the operation -- checked locally in personal mode, and sent to the authority
server for verification in service mode. It is never itself written to the durable event log: in
personal mode there is nothing left to hold it in by the time deletion finishes (see
Alternatives); in service mode, the tombstone row described below is where the equivalent record
lives. It is deliberately *not* routed through the normal `Mutate`/command-endpoint pipeline --
see Alternatives for why.

### New method: `Service.DeleteProject`

```go
func (s *Service) DeleteProject(actor, passphrase, confirmProjectID string) (DeleteProjectResult, error)
```

`confirmProjectID` must equal the project's actual `ProjectID` (from `Store.Config()`) or the
call refuses before touching anything -- the typed-confirmation step described under CLI/TUI
below, enforced once, centrally, rather than trusting every call site to have prompted for it.

Steps, in order, each one gating the next:

1. Load config, resolve `actor`'s current role from state; refuse unless it is exactly `OWNER`.
2. Refuse unless `confirmProjectID == config.ProjectID`.
3. `decryptElevatedKey(actor, passphrase)`; refuse if none is registered or the passphrase is
   wrong (identical error behavior to every existing elevated-key consumer).
4. Sign `ProjectDeletionAuthorization{ProjectID, Actor: actor, IssuedAt: s.Now()}`.
5. **If `config.RuntimeMode == "service"`:** POST the signed authorization to the new authority
   endpoint (below). Abort here, with local state left completely untouched, on any failure --
   network error, signature rejection, project already gone, or an authority server too old to
   support this (detected up front via `GET /v1/capabilities`'s existing `schema_version` field,
   before attempting the call at all, for a clear "upgrade the authority server" error instead of
   a raw 404). This ordering is deliberate: local state is the only record that a retry is even
   needed, so it must never be discarded before remote deletion is confirmed.
6. Stop the local daemon if one is running, reusing `projectlifecycle`'s existing
   `stopDaemon`-style health-check-then-`Shutdown` sequence.
7. Remove every locally-cached credential and elevated key for this project from the shared
   keyring: for each actor in the local state's `Agents` map, unioned with every
   `identity.UserConfig.Profiles` entry whose `ProjectID` matches (covers an actor known only via
   a stale profile, not current state), `KeyringStore.Delete(projectID, actor)` and
   `KeyringStore.Delete(projectID, identity.ElevatedActor(actor))`.
8. Remove those same profile entries from the global `UserConfig.Profiles`, clearing
   `ActiveProfile`/any `ActiveProfileBySession` entry that pointed at one of them, and persist
   the updated user config.
9. `os.RemoveAll(filepath.Join(root, store.Runtime))` then `os.Remove(filepath.Join(root,
   ".agents"))`.
10. Return a `DeleteProjectResult` reporting what happened at each of steps 6-9 individually
    (mirrors `projectlifecycle.Result`'s own per-action reporting) -- if step 6 or 7 partially
    fails after step 5 already succeeded, remote state is already, correctly, gone; this is
    reported as a partial result needing manual local cleanup, not retried or hidden as a full
    success.

Personal mode skips step 5 entirely; nothing else changes. There is no tombstone in personal
mode -- the runtime holding any record of the deletion is itself what step 9 removes, which is
exactly what "no automatic backup, delete is truly final" means for a self-hosted project.

### New authority server endpoint

`DELETE /v1/projects/{project}`, body `ProjectDeletionAuthorization` plus its signature and the
actor's key fingerprint (same envelope shape `controlplane.Command` already uses for signed
writes). Server-side, in one transaction:

1. Load the project's current state; refuse (`403`) unless `actor`'s role is exactly `OWNER`,
   re-checked from authoritative state rather than trusting the client's own check.
2. Verify the signature against that actor's *registered elevated* public key for this exact
   `project_id` (not just any key of theirs) -- scopes a compromised elevated key to the one
   project it was issued for, and stops a captured signature for one project being replayed
   against a different, later project that happens to reuse the same ID.
3. Insert one row into a new `deleted_projects` table -- `project_id`, `owner_id`, `deleted_by`,
   `actor_key_fingerprint`, `deleted_at`, all immutable, no project *data* -- deliberately **not**
   a foreign key of `projects(project_id)`, so it survives the cascade in the next step instead
   of being deleted along with everything else. This is not the automatic backup that was
   explicitly declined (it holds no restorable project data, nothing an operator could use to
   reconstruct the deleted project) -- it exists purely so a shared, multi-tenant authority
   instance retains non-repudiable proof of who authorized an irreversible admin action against
   shared infrastructure, which is a materially different thing.
4. `DELETE FROM projects WHERE project_id = $1` -- every other project-scoped table already
   declares `REFERENCES projects(project_id) ON DELETE CASCADE` (`internal/authority/schema.sql`;
   confirmed for all twenty, including the sixteen `events` partitions), so this one statement is
   the entire remaining teardown.
5. `DELETE`ing an already-deleted or never-existing `project_id` returns `404` rather than
   erroring ambiguously -- deletion is idempotent, so a client retry after a dropped response is
   always safe.

Ships as `schemaMigration{Version: 4, Automatic: true, ...}` in
`internal/authority/schema.go`'s existing migration list -- purely additive (one new table, one
new index), so it applies at normal server startup exactly like the existing automatic
migrations, no `--allow-disruptive` step needed.

### CLI: `agent-comms project delete`

New subcommand under the existing `project` command group
(`internal/app/app.go`'s `projectCmd()`, alongside `upgrade`). **Refuses outright in
`--non-interactive` mode** -- deliberately, matching `agent elevate-key`'s own reasoning
(`internal/app/cmd_agent.go`'s comment on why elevate-key is CLI-only): a passphrase prompt is
meaningless to a script, and this is the single most destructive command in the system. No
`--yes`, no piped-passphrase flag; there is no scripted path for this one.

Interactive flow:

1. Prints the project ID, owner, runtime mode, and (service mode only) the authority URL, with an
   explicit warning that this is irreversible and, in service mode, deletes this project's data
   from the shared authority too.
2. Prompts `Type the project ID (<id>) to confirm permanent deletion: `, matched exactly against
   `config.ProjectID`.
3. Prompts for the elevated-key passphrase (masked, reusing `promptPassphrase`).
4. Calls `Service.DeleteProject`; emits `project.delete` with the full `DeleteProjectResult`.

### TUI: Project settings hub

A new "Danger zone" entry, distinct from every other row action by styling alone (matches the
audit's fix for a real focus indicator earlier this cycle -- destructive actions should look
different, not just behave differently). Opens a form built the same way `agents.go`'s
ORCHESTRATOR-grant and switch-role forms already are:

```go
Fields: []FormField{
	{Label: "Type the project ID to confirm"},
	{Label: "Elevated-key passphrase", Mask: true},
},
```

`Dispatch` calls `Service.DeleteProject` directly (not through `ExecuteWithPassphrase`, since
this isn't a state-machine transition). On success, the project this `Model` was constructed
against no longer exists -- there is no view left to return to. The program prints a final
confirmation and exits, the same terminal action `q` takes today, rather than attempting to
re-render any view against a store that is now gone.

## Alternatives considered

- **Model `project.delete` as an ordinary signed event through the existing
  `Execute`/`ExecuteWithPassphrase` pipeline**, extending `protocol.RequiresElevatedKey` with a
  `case "project.delete": return true`. Rejected: that pipeline's entire contract is appending to
  and projecting a *surviving* event log. A deletion event would need to be inserted and then
  immediately cascade-deleted by the very row it authorized, along with the table it lives in --
  either it never durably records who authorized the deletion (defeats the audit tombstone), or
  the delete has to special-case itself out of its own cascade, which is more convoluted than
  treating it as the dedicated, one-purpose method this RFC proposes.
- **Automatic backup before deleting**, matching `project upgrade`'s own pattern. Explicitly
  declined by the project owner: the point of this command, as asked for, is that it is truly
  final -- a lingering automatic copy of deliberately-destroyed data would itself be a second
  thing to secure or forget to clean up.
- **Local-only deletion, refuse in service mode.** Considered as the safer default and presented
  as an option; rejected by the project owner in favor of always deleting remote state too, so
  "delete" means the same thing regardless of runtime mode.
- **A soft-delete / grace-period undo window** before the Postgres cascade actually runs.
  Rejected as scope creep against an explicit "no backup, final" decision -- a grace period is a
  backup with extra steps.

## Compatibility and rollout

Purely additive: a new CLI subcommand, a new optional TUI form, a new authority HTTP endpoint,
and one additive (`IF NOT EXISTS`) schema migration. Nothing existing changes behavior. A CLI
built against this RFC talking to an authority server that hasn't been upgraded yet detects that
up front via `GET /v1/capabilities`'s `schema_version` and refuses locally with a clear
"authority server needs upgrading" error, rather than issuing the `DELETE` and getting a
confusing `404`.

## Security implications

- OWNER-only, enforced both client-side (before prompting for anything) and server-side (from
  authoritative state, not trusting the client) -- an actor merely holding *a* registered
  elevated key (any principal can register one) is not sufficient on its own; role is checked
  independently.
- Requires an elevated key to already be registered; there is no path to delete a project with
  only ordinary credential possession, matching every other elevated-key-gated action.
- The signature binds the authorization to one exact `project_id`, scoping a compromised elevated
  key's blast radius here to the single project it was issued against.
- Server-side deletion is one transaction: a dropped connection mid-request never leaves a
  partially-deleted project, and a retry against an already-deleted `project_id` is a clean `404`
  rather than a second, ambiguous mutation.
- The `deleted_projects` tombstone is metadata only (who, when, which project) -- it is not a
  backup and restores nothing; explicitly not what the project owner declined.
- No scripted or non-interactive path exists at all -- closes the obvious failure mode of this
  landing in a CI script or a shell alias by accident.
- This is, by explicit design and explicit user decision, unrecoverable. There is no code path in
  this RFC that mitigates a wrong confirmation followed by a correct passphrase; the typed
  project-ID step exists solely to catch that class of mistake before the passphrase prompt is
  ever reached.

## Test and rollout plan

- `internal/service`: `DeleteProject` unit coverage -- refuses for a non-OWNER actor, refuses on
  project-ID mismatch, refuses with no elevated key registered or a wrong passphrase, personal
  mode never calls the authority client, service mode aborts with local state fully intact on a
  simulated remote failure, full success path removes runtime dir/`.agents`/keyring
  entries/profile entries and reports each individually.
- `internal/authority`: endpoint unit + integration coverage against a real Postgres instance
  (matching this project's existing `postgres-integration` CI job) -- non-OWNER actor rejected,
  wrong-project signature rejected, cascade actually empties every project-scoped table, the
  tombstone row survives the cascade, a second `DELETE` on the same `project_id` returns a clean
  `404`.
- `internal/app`: end-to-end coverage for the CLI command's confirmation flow, and its outright
  refusal under `--non-interactive`.
- `internal/tui`: coverage for the new Danger Zone form and its exit-on-success behavior.
- `go test -race`, `staticcheck`, `gofmt`, `scripts/coverage-floor.sh` all clean before shipping,
  matching this session's established verification bar.

## Unresolved questions

- Whether a separate, opt-in `agent-comms project export` (a manual, on-demand snapshot,
  distinct from the automatic backup already declined here) is worth adding later for users who
  want to snapshot before deleting of their own accord. Left for a future RFC if wanted.
- Whether the `deleted_projects` tombstone should be surfaced through any CLI/doctor command, or
  remain a raw operator-only database artifact for now. Proposed default is the latter (nothing
  new to expose) unless review wants it queryable.
