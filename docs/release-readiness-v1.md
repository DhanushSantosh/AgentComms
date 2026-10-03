# First stable release readiness

Reviewed 2026-10-03 against `abfb99c` (published `v0.8.2`) and the current
release-history changes. This is a release preparation record, not a claim
that `v1.0.0` has already been published.

## Current baseline

`dev`, `origin/main`, and the `v0.8.2` release share `abfb99c`. The release is
published, and CI passed on both branches. No product commits were left
unreleased at the start of this pass.

The latest beta incorporated:

- Event-derived creation/update timestamps, newest-first lists, JSON order
  arrays, and CLI/MCP/TUI agreement on ordering (RFC 0041).
- Verified history replay for timestamp migrations: personal authority
  schema 2, projection cache schema 4, and PostgreSQL migration 7. Existing
  projects require an upgrade; unverifiable history fails migration.
- Windows daemon replacement lock handling, SemVer toolkit comparison,
  isolated test credentials/config, and cross-platform TUI test corrections.
- Refreshed docs, real TUI captures, and the rebuilt lower landing sections.

The preceding release provided bounded responsive TUI panes, clear separation
of messaging from runtime presence, `doctor --fix`, consistent principal
resolution, the Windows named-pipe shutdown correction, and dependency fixes.
These are already released; the first stable release builds on that baseline.

The supported product is the CLI, TUI, and MCP interface to signed project
history, identities and roles, work leases, durable messages, approvals,
documents, and invocations. Local personal authority and an operator-managed
shared PostgreSQL authority are both supported. Messaging is independent of
runtime presence; automatic delivery requires an eligible runtime.

## Release-history changes in this pass

- The landing site's release feed contains published stable versions only.
  Until the first stable version is published it shows an explicit pending
  state and links to the docs archive.
- Docs retain every beta entry in the canonical Markdown source. The human
  page uses a collapsed beta archive and a dropdown; only the selected entry
  is visible. Existing heading links select and open the matching entry.
- Stable and beta selections are separate. Opening the beta archive hides
  the stable entry, keeping one release visible across the page.
- Raw Markdown and agent documentation retain the complete historical record.
  Existing Git tags, GitHub releases, installer pins, and assets remain intact.
- Future stable entries belong in both the docs changelog and the landing
  stable feed as part of promotion. Download/version labels continue to
  reflect the latest published tag until the new release is published.

## Current evidence and remaining gates

The local project has verified signed integrity and no doctor findings or
AGC attention items. GitHub reports no open Dependabot security alerts. The
open routine-dependency PR #72 is not a security blocker and is outside this
release-history change.

The baseline passed [dev CI](https://github.com/DhanushSantosh/AgentComms/actions/runs/37003558777)
and [main CI](https://github.com/DhanushSantosh/AgentComms/actions/runs/37001870420).
The changed site candidate must pass its own builds, content/reference checks,
desktop/mobile browser checks, and CI before promotion; baseline green does
not substitute for candidate validation.

Local validation of this patch passed:

- `npm run landing:check` and `npm run docs:check`: type checks, production
  builds, release-browser unit tests, and docs content validation.
- `npm run docs:generate:check`: generated CLI/MCP references are current.
- Landing Playwright: 71 passed, 3 expected skips; docs Playwright:
  45 passed, 1 expected skip, covering desktop and mobile.
- `git diff --check`: no whitespace errors.

The first browser pass exposed a missing mobile archive link, which was
fixed. Concurrent runs also produced a live-TUI timeout and a browser crash
before navigation; the focused TUI retry and both complete single-worker
suites subsequently passed. These checks do not replace the new commit's CI
or Windows-native validation. The release-history patch is not yet committed
or published as part of this preparation record.

No current product blocker is identified under the accepted deployment scope:
trusted self-hosted teams. Known limits remain explicit:

- Entity lists load full state, and draft listing has no continuation cursor.
  The focused recovery/parity validation does not establish production-scale
  throughput or a long-duration multi-worker soak.
- Interactive PTY delivery still relies on echo heuristics. Structured
  delivery receipts, hosted joining, custom provider identity design, and
  first-class worker supervision remain deferred.
- Native Windows Authenticode and Apple notarization are not provided;
  checksums, provenance, and Sigstore verification remain the release trust
  mechanisms. Windows lower-landing visual baselines remain a documented
  follow-up; Linux browser baselines are the current site CI gate.

## Promotion sequence

Follow [the release process](releasing.md) for `v1.0.0`: curate the stable
summary and compatibility statement, select a unique release nickname, update
README/installer pins and the verifier manifest, and add the stable site
entries. Run checks on the release candidate, promote `dev` to `main`, tag
the resulting reviewed commit, and obtain the protected release-environment
approval. Verify a clean published install, redeploy the sites after assets
exist, and merge the release tip back into `dev`.

The beta archive is historical reference after v1. Stable public-contract
changes then follow SemVer: incompatible public changes require a new major.
