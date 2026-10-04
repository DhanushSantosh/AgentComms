# RFC 0040: One TUI palette and explicit doctor repair

## Status and owners

**Accepted, 2026-09-29.** Owner: the project maintainer. Drafted by codex-main.
The owner accepted this contract conditional on matching the implementation;
the CLI help, TUI palette and legacy-config behavior, doctor repair branches,
structured output, regression tests, generated reference, and repository-wide
Go tests and vet were checked against it before changing this status.

This is a retrospective contract review, not a claim that review preceded
implementation. The `config theme` removal and `doctor --fix` addition are
included in v0.8.1 after v0.8.0. RFC 0027 section
7 promised `config theme`; this RFC supersedes that section. This acceptance
ratifies the present contract for that release; it does not erase the
fact that implementation preceded the RFC review.

## Problem and desired outcome

The TUI formerly offered several palettes through a user-configured theme.
The current TUI uses one high-contrast palette and the terminal's background
throughout. A `config theme` command would now appear to work while changing
nothing, so the implementation removed it and ignores older `theme` values.
That contradicts the accepted CLI contract in RFC 0027 unless the change is
reviewed explicitly.

`doctor` historically diagnosed project health but left even mechanical
repairs to separate commands. After an update, a project may need its managed
files reconciled with the new toolkit; a stopped local daemon can also make
verification fail. `doctor --fix` was added to perform those narrow repairs,
but adding a public flag and JSON outcome fields also requires contract
review.

Desired outcome: document one truthful, bounded CLI contract for the next
release, including the break from RFC 0027 and the limits of automatic repair.

## Proposed design

### 1. Remove theme selection

- Keep one TUI palette and terminal-default backgrounds. There is no TUI
  theme toggle or `config theme` subcommand. `config` continues to inspect
  resolved configuration and manage its other settings.
- An older user configuration's `theme` key is ignored. Reading that file
  must not fail or silently select a different palette; this proposal does
  not require deleting or rewriting a user's configuration.
- Help, generated CLI reference and the next release's changelog describe
  the removed command. RFC 0027 remains the historical record of why it was
  introduced; this RFC supersedes only its theme-selection contract.

### 2. Offer bounded `doctor --fix`

`doctor` without `--fix` remains diagnostic. With `--fix`, it may:

1. Reconcile a project's lifecycle using the same managed-file and toolkit
   mechanism as `project upgrade`, when a finding is listed in
   `doctor.FixableCodes` and the plan needs no human confirmation.
2. Start the local daemon and re-verify when verification reports that the
   daemon is unavailable.

It does not approve confirmation-required migrations, reassign leases,
change identity or authority policy, or repair connector configuration by
guessing. Findings outside the fixable set remain visible with guidance.

The result reports what was repaired and any repair errors. JSON keeps the
existing `findings` and `healthy` fields and adds `fixable` (count), plus
`fixed` and optional `fix_errors` when `--fix` is requested. A lifecycle
lock conflict triggers reinspection; success is reported only if the
lifecycle finding cleared. A continuing conflict stays an actionable error.
`doctor --fix` reports its outcome rather than marking a failed repair as
completed.

Without an initialized project, `doctor` reports `NO_PROJECT_HERE` and has
no repair target. That early diagnostic response does not include the
project-specific `fixable`, `fixed`, or `fix_errors` fields.

## Alternatives considered

- **Keep `config theme` as a compatibility no-op.** Rejected: a setter that
  accepts input but has no observable effect is a misleading public API.
- **Restore multiple palettes.** Possible if user demand warrants it, but it
  would reintroduce the background and contrast combinations this TUI change
  deliberately removed. That would need a separate design and visual tests.
- **Keep doctor diagnostic-only.** Users can invoke `project upgrade` and
  restart the daemon themselves, but that leaves routine, deterministic
  repair disconnected from the finding that explains it.
- **Make `--fix` repair every finding.** Rejected: leases, credentials and
  connector choices require human or orchestrator judgment.

## Compatibility and rollout

Removing `config theme` breaks callers of that command. This is a pre-1.0
CLI change, but still needs a Breaking entry and generated-reference update
before the next release. Existing `theme` configuration is tolerated and
ignored; other profile and project configuration stays intact.

`doctor --fix` is additive, but it writes managed project files and may start
a local daemon. Users must opt in by supplying the flag. Structured-output
consumers should tolerate the additional fields. The next release notes
should state both changes together so upgrades do not imply that theme
selection still works.

## Security and privacy implications

The theme change has no authority or privacy effect. `doctor --fix` acts on
local managed project files and daemon availability under the caller's
existing filesystem permissions. It must preserve the lifecycle's backup,
lock, integrity and confirmation boundaries. It cannot convert a finding
that needs human judgment into an implicit authorization, and must not
describe an incomplete or conflicting repair as complete.

## Test and rollout plan

- Assert `config theme` and the old TUI theme toggle are absent, and a
  legacy `theme` configuration remains readable without selecting a palette.
- Exercise diagnostic-only `doctor`, successful lifecycle repair, a
  confirmation-required plan, daemon recovery, lock conflict with a cleared
  lifecycle finding, and lock conflict with an unresolved one. Verify the
  human and JSON outcomes agree.
- Run focused `internal/app`, `internal/doctor`, `internal/tui` tests, then
  `go test ./...`, `go vet ./...`, and generated CLI-reference checks.
- Review the release changelog and command help against the implemented
  behavior. Owner acceptance is recorded above; any later contract change
  needs its own review.

## Unresolved questions

None. The owner accepted the single-palette break and bounded `doctor --fix`
contract after the implementation check.
