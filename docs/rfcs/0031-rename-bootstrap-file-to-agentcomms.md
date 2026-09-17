# RFC 0031: Rename the managed bootstrap file from `.agents` to `.agentcomms`

## Status

**Accepted, 2026-09-05.** The project owner requested this directly while
reviewing [UX-01 of the release UX audit](research/2026-09-05-release-ux-audit.md)
(`docs/research/2026-09-05-release-ux-audit.md`), per `docs/rfcs/README.md`.

Changes the on-disk installation contract (a file every initialized project
creates and that other tooling can collide with), so it requires review.

## Problem and desired outcome

`init` refuses to run if anything at all already exists at `.agents` --
file, empty directory, or populated directory -- with no way to coexist
(UX-01). `.agents` is also an increasingly common convention among
unrelated agent-tooling projects for their own directory of agent
configs/personas, so the collision is not hypothetical or specific to one
other tool; it is a generic name in a space several projects now use.

The audit's own proposal for UX-01 was better preflight messaging and
resumable-state detection while keeping `.agents`. The project owner chose
a more direct fix instead: stop using a name likely to collide at all.
`.agentcomms` is unambiguous, matches the product's own name (consistent
with the `.agent-comms` runtime directory it points at), and removes the
collision risk rather than making the collision easier to recover from.

Desired outcome: new projects never touch `.agents`; existing projects
already carrying `.agents` from before this change upgrade to
`.agentcomms` automatically, with the stale file cleaned up, not just
newly ignored.

## Proposed design

### 1. The managed file itself

`store.ManagedFiles()`'s map key changes from `.agents` to `.agentcomms`.
Content and purpose are unchanged: the same tiny generated marker
(`# Agent Comms managed bootstrap\nruntime = .agent-comms\n...`) that
`ManagedBootstrapValid`/doctor use to confirm a project's bootstrap matches
its configured runtime mode, still pointing at the same `.agent-comms`
runtime directory (that directory's own name is unaffected -- only the
top-level marker file's name changes).

`runtimeinit.Initialize`'s pre-flight conflict check (`bootstrapPath`) now
checks for `.agentcomms` instead of `.agents`. The check's behavior is
otherwise unchanged: any existing entry at that exact path (file or
directory) still blocks init with the same explicit error, since a
population of that specific product-named path by anything else would be
a real, worth-surfacing conflict rather than the previous generic-name
false-positive UX-01 identified.

### 2. Existing projects: automatic migration, not a break

`store.ManagedFilesVersion` bumps from 1 to 2. `Inspect`'s existing drift
detection (`managedFilesCurrent`, keyed off `ManagedFilesVersion` and a
per-file content hash already stored in `config.ManagedFileHashes`)
already treats any `ManagedFiles()` map change as a normal, automatic
"backup drift and publish canonical files" migration step -- this is the
same mechanism every prior managed-file change has used, not a new one.

`backupProject` (which runs first, unconditionally, for any pending
migration) preserves the old `.agents` file's content under the project's
own `.agent-comms/backups/<timestamp>-<id>/` directory before anything is
changed, exactly as it does for every other reconciled file today.

`publishManagedFiles` -- the step that writes the new `ManagedFiles()` set
-- additionally removes `.agents` if present, after successfully writing
`.agentcomms`, so a reconciled project ends up with exactly one bootstrap
file on disk, not both. This is the one piece of net-new logic; everything
else is the existing migration path applied to an existing map change.

### 3. Every other reference

- `store.runtimeIgnoreRules`'s `.gitignore` entry becomes `/.agentcomms`
  (existing repos keep whatever stale `/.agents` line they already have;
  `EnsureRuntimeHidden` only adds missing rules, never removes one, so a
  harmless leftover `.gitignore` line is an acceptable trade against
  rewriting a file this project does not fully own).
- `doctor`'s `MANAGED_BOOTSTRAP_MISSING` message and `cmd_core.go`'s
  `init` confirmation prompt text both say `.agentcomms`.
- `service.go`'s project-delete cleanup removes `.agentcomms` (and still
  tolerantly no-ops on a missing `.agents`, in case delete runs against a
  project that was never reconciled past this migration -- deletion must
  not require every prior migration to have already run).

## Alternatives considered

- **Keep `.agents`, improve preflight/coexistence messaging only** (the
  audit's own UX-01 proposal). Rejected by the owner in favor of removing
  the collision surface itself rather than making it more recoverable.
- **Support both names indefinitely** (read either, write only the new
  one). Rejected: doctor/`ManagedBootstrapValid` would need to check two
  paths forever for no lasting benefit once every project has migrated
  once; the automatic migration path already makes indefinite dual-support
  unnecessary.
- **A more disambiguated name than `.agentcomms`** (e.g. keeping a
  separator, `.agent-comms-bootstrap`). Rejected as unnecessary; the
  product name alone is already distinctive enough, and shorter names are
  worth preferring when they cost nothing in clarity.

## Compatibility and rollout

Breaking in the narrow sense that the on-disk file name changes, but the
existing `ManagedFilesVersion` migration path already handles every prior
managed-file change the same way -- this is not a new upgrade mechanism.
`agent-comms project upgrade` (or any command's automatic reconcile) picks
it up like any other pending migration; a project mid-upgrade that crashes
resumes from its journal exactly as today. No schema-version bump, no
signed-event change -- this is purely the local managed-file set, which
already has its own versioning independent of `model.SchemaVersion`.

## Security and privacy implications

None beyond what the existing migration path already carries: the old file
is backed up before removal, and the new file's content and purpose are
identical to the old one's. No change to what the bootstrap file grants or
verifies.

## Test and rollout plan

- `runtimeinit` test: `init` succeeds when `.agents` (file or directory)
  already exists, since the path is no longer touched at all; `init` still
  refuses when `.agentcomms` itself already exists.
- `projectlifecycle` migration test: a project fixture with the old
  `ManagedFilesVersion`/`.agents` state reconciles to `.agentcomms` present,
  `.agents` removed, and the old content recoverable from the backup
  directory.
- Update every existing test/doc fixture referencing `.agents` as this
  project's own bootstrap file (not third-party tooling's own use of the
  name, which this RFC does not touch).
- `go build`, `go vet`, `go test ./...`; docs-site `check`.

## Unresolved questions

None.
