# Major-release audit — in progress

This record tracks the owner's final whole-project audit. It is not a release
approval or proof that every workflow works. Baseline: `abfb99c` / v0.8.2;
release-history candidate: `2521418`. Audit fixes must be checked on their own
resulting revision. Native Windows/macOS execution is provided by candidate
CI, not inferred from Linux cross-compilation.

## Coverage and evidence ledger

| Surface | Required evidence | Current state |
| --- | --- | --- |
| Whole Go tree | Full suite, vet, race, staticcheck, coverage floors | Immutable 6a0e7b8 full uncached race passed; c6ed603 actual coverage command measured every declared floor and passed; 150b693 exact eight-job CI passed. Later changes need their own gates; whole-project completion review remains open |
| Identities, governance, protocol | Role/credential isolation, approval expiry/binding/consumption, replay and rejection cases | Role/requester fixes and accepted RFC 0046 grant expiry have focused/replay/real-Postgres evidence and native CI; prior no-expiry behavior retained. Full audit closure remains pending |
| Storage and upgrades | Signed-history replay, tamper rejection, SQLite/cache and Postgres timestamp migration | Fresh SQLite/cache lifecycle race suite passed; actual Postgres migration/confirmation/future-schema checks and new corrupt-history/projection rejection tests passed |
| Shared authority and recovery | Authentication, writes, idempotency, cache lag, retry, deletion, stream admission | Lost-response/lease-expiry backoff regression fixed; three actual isolated Postgres race repetitions passed (authority 44.821s / daemon 247.731s), including sustained writes/cache lag. Historical coordinator-first flake cause remains unproved |
| CLI/MCP | Actual adapter writes, result/order/history parity, generated reference consistency, failure semantics | Isolated actual authoritative adapter parity passed again on ced2173 (43.663s); latest generated-reference and platform CI passed. Final requirement-by-requirement closure remains pending |
| TUI | Navigation/actions, constrained panes, resize, ordering, approvals, runtime-independent messaging | Actual isolated PTY navigation/resize at five sizes, signed actions and runtime-independent messaging passed; final frame-bounds fix ee6b7c4 passed exact CI. Full frozen 6a0e7b8 TUI race passed (409.770s); later candidate platform CI passed |
| Runtime/providers | Local-process lifecycle, durable delivery and real installed provider smoke | Actual Codex exec/live, Claude native exec/live/ACP and OpenCode exec/ACP evidence recorded; RFC 0047 owned trees/ACP reaping and RFC 0048 routing tested. RFC 0049 is wired and independently reviewed; native reset completion, Undo preservation, observed-request saved-approval isolation, final mode/governance/full-attach/continuity and same-ID/two-project controls pass. Expanded candidate full uncached race and local gates pass; exact candidate CI remains open |
| Installation and release trust | Authentic installer bootstrap, genuine signature acceptance, tamper/identity/issuer rejection | Live releaseverify suite passed uncached, including all four genuine-release subtests |
| Platforms | Native Linux/Windows/macOS tests, Windows pipe-close regression, six-target/four-binary builds | All three native platform jobs and cross-build passed on 150b693 in CI 37189925642; this does not execute Linux-only real-provider opt-ins on Windows/macOS |
| Go dependencies | govulncheck and direct/transitive exposure review | Compatible x/crypto and x/mod security updates validated; refreshed scan has no called/package findings, only upstream-test-only OpenPGP module advisory |
| Site dependencies | npm audit, impact assessment and any fixes | Accepted RFC 0043 pinned patch passes 20 behavior/maintenance checks and unchanged npm audit; c50385a docs/landing/security CI jobs passed |
| Docs/landing | Build, content generation, keyboard/mobile/desktop/browser/visual and Lighthouse checks | Post-patch checks/builds, docs 45/landing 71 browser tests and unchanged Lighthouse thresholds passed locally; both site CI jobs passed on c50385a |
| Production deployment | Exact candidate deploy and release/version correctness | Both site deployments passed on c50385a in workflow 37178144647; live landing download reports v0.8.2 and docs releases/changelog serves the beta archive |
| Repository coordination | Inbox obligations, integrity, doctor, task state and exact Git/CI refs | 150b693 is on origin/dev and exact CI 37189925642 passed all eight jobs; integrity verified through 572 and installed v0.8.2 doctor healthy. Installed runtime is not the newer source candidate. Claude updated; audit task remains IN_PROGRESS |

The test-fixture candidate c30cae7 subsequently passed all eight CI jobs in
workflow 37179122223, including the unchanged parallel Postgres coverage gate.
This does not certify later source changes until their own candidate CI runs.

Candidate 7315f3a (accepted RFC 0045) passed all eight CI jobs in workflow
37180918042 and both site deployments in 37180918041. A fresh full uncached
race run of that exact revision finished from an immutable Git archive at
`/tmp/agc-release-candidate-7315f3a-HstUsH`, with output in `race.log`.
The app package subsequently hit its default ten-minute test-process deadline
(600.890 seconds); the active test had run eight seconds at the timeout.
The dump includes runnable executable-content hashing, not evidence that this
specific test deadlocked. This full invocation is not a pass. All remaining
packages passed in that invocation; its terminal exit was 1 and the original
log is retained. Do not weaken repository assertions
or CI timeouts to suppress this outcome.

## Findings under investigation

### AUD-01: build-time GitHub lookup can exhaust anonymous quota

Landing deployment run `37145634479` failed while prerendering the download
Open Graph image: the latest-release API returned HTTP 403. The lookup did
not authenticate. A regression test first failed on the missing Authorization
header. The fix adds an optional **server-only**
`AGENT_COMMS_RELEASE_API_TOKEN`, supplied by the existing read-only GitHub
workflow token in CI/deployment. It does not substitute a stale fallback or
silence bad responses. Tests now pass for token propagation, anonymous builds,
one shared lookup, 403 rejection, malformed JSON and invalid tags. Production
build passed. Both deployments passed in workflow `37146103139` on `e573c90`.

### AUD-02: unpatched npm advisory fails the release gate

`npm audit --audit-level=high` currently fails for
[GHSA-ch52-4w7c-c8xp](https://github.com/advisories/GHSA-ch52-4w7c-c8xp),
`http-cache-semantics` 4.2.0 through Astro 7.3.2. At investigation time the
registry's latest version is 4.2.0 and the advisory lists no patched release.
Npm's suggested Astro 2 downgrade is not a suitable fix. Astro's installed
call sites use CachePolicy for build-time remote-image TTL computation; the
docs deploy as static files, not an authenticated shared HTTP cache. This
is evidence about reachability, not permission to suppress the audit gate.
The resolution, continued upstream monitoring and full dependency assessment
remain open. The separate fast-uri moderate advisory was cleared with the
compatible lockfile update from 3.1.7 to 3.1.8.

Follow-up: the registry now publishes 4.3.0. A targeted lockfile update and
clean `npm ci --ignore-scripts` report zero vulnerabilities. Nevertheless,
`node --test sites/docs/cache-policy.test.mjs` fails for shared private-cookie,
`proxy-revalidate` and `no-cache` max-stale reuse, while normal public reuse
passes. The installed file matches the upstream published-source digest.
This is behaviorally reproduced dependency risk, not a validated site exploit:
Astro's current build path uses TTL methods, not the vulnerable request-reuse
method. [RFC 0043](rfcs/0043-pinned-cache-policy-security-patch.md) proposes a
bounded, provenance/hash-checked patch accepted by the owner in this chat.
The dependency upgrade alone is not a verified security fix. The regression
was observed red before patching and now passes after the bounded guard is
applied. The repository-wide audit gate remains unchanged. Both public and
immutable cookie opt-ins, ordinary expired public max-stale reuse and private
cache proxy-revalidate behavior still pass. Non-storable responses are refused.

Root `postinstall` applies an atomic, idempotent patch only to the reviewed
version/source digest and runs maintenance tests. It verifies Astro resolves
that same patched copy and rejects unknown versions/hashes, missing files,
external path resolution and a separate consumer dependency copy. All 20
behavior/maintenance cases pass. Clean normal `npm ci` applied the initial
patch automatically; the subsequent consumer-resolution check and its fixtures
pass too. A second clean normal `npm ci` of the final hook applied the patch
and passed all nine maintenance tests, including consumer-copy confinement.
Fresh candidate CI must still verify the final lifecycle hook on Node 22.
Unchanged `npm audit --audit-level=high` reports zero vulnerabilities.
License, source commit and both digests are retained under
`third_party/http-cache-semantics/PATCH.md`. Lifecycle-disabled installations
must explicitly run `npm run deps:patch`; the lockfile alone is not remediation.

Post-patch docs generation/check/build passed, validating 30 product pages,
32 rendered pages, 123 CLI commands and 30 MCP tools. Landing release-lookup
tests, TypeScript, WASM generation and production export passed. The unchanged
browser suites passed on the resulting built assets: docs 45 / one expected
desktop/mobile-only skip (1.6 minutes), landing 71 / three device-specific
skips (4.6 minutes). The desktop live WASM TUI approval interaction passed;
mobile uses the recording instead. No baseline regeneration or tolerance
change was made. Logs: `/tmp/agc-docs-cache-patch-browser-20261004.log` and
`/tmp/agc-landing-cache-patch-browser-20261004.log`. Contamination tests passed.
Unchanged Lighthouse gates passed: docs' four three-run page medians had
performance 1.0, accessibility 1.0 / 0.96 / 0.95 / 0.96, and best-practices/SEO
1.0 throughout; landing's three-run median was performance 0.97 and all other
categories 1.0. The one docs-home performance outlier (0.62) is retained in
the evidence, not removed; the pre-existing median-of-three gate passed.
Logs: `/tmp/agc-docs-cache-patch-lighthouse-20261004.log` and
`/tmp/agc-landing-cache-patch-lighthouse-20261004.log`. New exact-candidate CI
remains separate required evidence.

## Execution notes

- Owner-requested source privacy cleanup is pushed as `ed4d57d`, following
  runtime fixes `f43134d`. Personal contact text, personal-name examples,
  local home paths and identifying captured test-fixture values were replaced.
  The actual DCO script passes using the masked historical-owner allowlist;
  future local commits use a GitHub no-reply identity. Required public
  repository/module/download/signing identifiers remain operational. Historical
  commits, signed tags and published archives were not rewritten.
- OCR scanned all 62 tracked non-icon PNG/WebP assets and found no matching
  personal-name/contact candidates. Video/animation metadata is checked
  separately; metadata scanning alone is not proof about every rendered frame.
- Full MP4/GIF frame extraction produced 876 frames, deduplicated by SHA-256
  to 751 unique frames. Tesseract scanned all 751 with zero matching candidate
  filenames. This is automated OCR evidence, not a guarantee against OCR misses
  or a history/archive privacy rewrite. Existing source media was unchanged.
- The user interruption invalidated the remaining race/Postgres process
  handles, and no corresponding processes remain. Final results were not
  recoverable, so those runs are not counted as passes. Subsequent heavy gates
  are serialized with bounded compiler parallelism to avoid repeating the
  observed contention. The isolated app/doctor uncached coverage rerun passed:
  `GOMAXPROCS=2 GOTOOLCHAIN=go1.26.6 go test -count=1 -p 1 -cover
  ./internal/app ./internal/doctor`; app 437.209 seconds / 65.4%, doctor
  1.412 seconds / 62.6%.

- Local toolchain: Go 1.27.1; CI is pinned to Go 1.26.6. Both results must be
  distinguished rather than assuming toolchain parity.
- Staticcheck v0.7.0 cannot decode Go 1.27 export data. Its local run failed
  in standard-library imports; a retry uses CI's Go 1.26.6 toolchain rather
  than changing product code to accommodate the tooling failure.
  The pinned-toolchain retry passed.
- Docker bridge creation failed because this host cannot create veth pairs.
  A separately named disposable Postgres container using host networking
  and `listen_addresses=127.0.0.1`, port 25432, is healthy. No existing project
  database or runtime was used for this integration run.
- Real-provider smoke tests are opt-in and use subscriptions. Their existence
  or a default-suite skip is not live-provider evidence.
- The prior release-readiness record is a dated baseline. Fresh advisory and
  deployment failures supersede its earlier no-blocker assessment.

### AUD-03: corrupted encrypted nonce crashes elevated-key decryption

The credential source review found no nonce-length validation before AES-GCM
Open. A regression reproduced panics for decoded lengths 0, 1, 11 and 13.
The minimal length check returns an error before Open without changing the
key format, passphrase derivation or authorization rules. Identity tests pass
three times with the race detector; vet and diff checks pass. Fix integration
and final-candidate checks remain required.

### AUD-04: Codex live broker omits requested runtime boundaries

Both fresh `thread/start` and resumed `thread/resume` omitted the validated
ProcessConfig sandbox. A fake-protocol test reproduced the missing read-only
setting. The fix explicitly passes sandbox and noninteractive approval policy
per accepted RFCs 0006/0009, plus the existing internal model setting. A
separate regression proved non-retrying provider errors were ignored until a
deadline; they now return a diagnostic immediately. Codexserve race tests
passed three times. Additional writable roots were separately reproduced as
missing on both start and resume, and direct process config accepted relative,
missing and file paths. The follow-up maps AddDirs to native thread config
`sandbox_workspace_write.writable_roots` and validates absolute existing
directories before launch. The installed app-server accepted this dotted
config key and reported exactly the additional root in its effective
workspace-write policy. The complete codexserve package passes three race
repetitions and vet after the fix. Ignore-user-config remains unresolved:
installed Codex 0.149.1 exposes the isolation flag on exec, but not app-server
or the generated thread/start and thread/resume schema. Do not silently treat
this option as implemented or change user config to work around it.
The owner accepted RFC 0042 in the project chat. Worker validation and direct
broker registration now reject the unsupported option before launch; red tests
first proved both accepted it and the broker launched a provider. The broker
regression checks a child-start marker and the registration map. Default live
validation and the exec isolation flag remain covered. Public help, provider
instructions, site worker docs and generated CLI reference are updated;
reference generation/check pass. Full codexserve and worker packages pass
three race repetitions and vet after this change. A real explicit-model Codex
two-turn smoke also passed in 17.95 seconds on the changed source.

### AUD-05: failed Codex startup leaves an unregistered subprocess running

A synthetic subprocess lifetime socket reproduced leakage after initialize
rejection, thread/start rejection and initialize deadline. Successful processes
must outlive their registration request; attaching them indiscriminately to
that request context is not the fix. Startup now closes the child on failure,
including failed restart handshakes, while preserving persistent successful
runs. The regression is portable and closes its synthetic control connection
to terminate the helper even when a test fails. All three cases pass after the
fix; the package race repetitions and real context-continuity smoke above
cover the changed lifecycle. No temporary debug tracing remains.

### AUD-06: an acknowledged Codex turn hangs after process exit

A fake provider acknowledged turn/start and then exited without an answer.
The prior Send waited until its deadline instead of entering the existing
resume/retry path; the regression reproduced `context deadline exceeded`.
The process reader now signals its own generation's exit, and the current
turn drains its buffered notifications before treating that signal as EOF.
Persistent external observers remain subscribed across restart. An older
reader cannot clear a newer process's pending calls. Ten race repetitions
passed for acknowledged-crash recovery and final-answer-before-exit handling;
the complete codexserve package also passed with race, and vet passed.
This enforces the existing crash-retry contract, not a new retry policy.

### Additional runtime/stress evidence

- Fresh binary built from `492958d` was exercised in an isolated personal
  project with separate config/credential directories. A real PTY rendered
  the overview, navigated Agents/Inbox/Invocations, resized to 80x24, 60x16,
  160x48, 40x12 and 120x32, and quit with exit 0. Resize checks establish live
  survival and repaint, not exhaustive visual bounds; unit layout tests cover
  bounds separately. Two synthetic active agents without runtimes posted,
  acknowledged and replied to messages and requested/inspected a governed
  invocation. The queued request has no delivery evidence, as expected without
  a consumer. The live overview rendered both agents READY with NO RUNTIME and
  the explanation that messaging is independent of runtime presence. Fixture
  signed-history integrity verified all 11 events. Its own daemon was stopped;
  real project history and credentials were not used for the fixture.

- The real Claude process preserved context across two turns (24 seconds).
- The first Codex live smoke hit a provider rejection of the user's configured
  default `gpt-6-sol`, then timed out. The retry uses an explicit test-only
  model setting without modifying the user's Codex configuration. With the
  boundary/error patch and `AGENTCOMMS_CODEX_SMOKE_MODEL=gpt-5.5`, both real
  turns passed in 23 seconds and preserved context.
- OpenCode's real exec smoke failed after bash permission requests were denied.
  The permission boundary stayed enforced; this run does not prove successful
  execution/result publication. Its generic implementation-review fixture
  requires closer inspection before repeating the success case.
  The replacement opt-in fixture requests a synthetic no-tool receipt rather
  than a review of nonexistent implementation files. With
  `AGENTCOMMS_OPENCODE_SMOKE=1 GOTOOLCHAIN=go1.26.6 go test -count=1 -v
  ./internal/worker -run '^TestManualSmokeOpenCodeExec$'`, the real worker
  claimed, completed and published the requested receipt in 20.95 seconds.
  Ordinary worker tests also pass uncached after the fixture change. This
  establishes no-tool provider/result plumbing, not arbitrary tool execution.
- Claude's reported delivery-failure flake passed 20 race-enabled repetitions
  (`TestInvocationDeliveryFailureDoesNotTerminateObligation`). No timing
  relaxation was applied; success repetitions do not establish flake absence.
- Postgres race stress failed concurrent-load checks with statement timeouts
  while race/coverage suites saturated this four-core host. An isolated repeat
  is required to distinguish load-sensitive failure from a product regression.
  The bounded-parallelism repeat also failed the 100-concurrent-invocation
  authority case with SQLSTATE 57014 (statement timeout); no timeout was raised
  and this failure remains open despite native CI's passing non-race run.
  The same rerun's daemon integration failed queued notification: a delivery
  attempt exists for inv-queued-05 but no notify/failure event followed it.
  Subsequent attempts exist too. This is a separate incomplete-delivery result,
  not proof of a connector denial or a harmless test timeout. Diagnosis remains
  required before assigning cause or changing retry/error behavior.
  A daemon-only race rerun passed in 88.32 seconds with temporary tracing at
  remote-command and cache-apply errors; neither boundary logged an error.
  Tracing was removed afterward. This narrows the reproducibility evidence but
  does not explain the previous incomplete delivery or close the finding.
  The same 100-writer authority test passed without race instrumentation in
  3.47 seconds, without increasing statement timeout or reducing writer count.
  Race overhead versus lock/pool waiting still needs isolation.
- The concurrent local coverage-floor run failed: the app package hit its
  ten-minute timeout and doctor was killed. The host was at load 20 with swap
  use while multiple heavy suites ran. This is not a coverage-floor pass or
  proof that the failures are harmless; repeat the exact gate without competing
  stress after the current race run finishes. Native CI passed that gate on
  `e573c90`, but the new source fixes still require their own verification.
- GitHub reports the pushed commit and v0.8.2 tag signatures as verified.
  Local SSH verification lacks an allowed-signers file; that local setup
  failure is not an unsigned-commit claim. Keep the repo's configured signing
  identity for release preparation; actual v1 tag verification is a publish gate.

## Closure requirements

### Storage migration and recovery checks

On the updated Go-dependency candidate, `go test -race -count=1 -v
./internal/projectlifecycle ./internal/localcache ./internal/store` passed
(7.688, 1.306 and 1.047 seconds). This exercises migration resume from all five
journal stages, concurrent reconciliation, partial backup recovery, disruptive
confirmation, future-version refusal, symlink refusal, signed-history replay,
legacy timestamp restoration, tampered authority refusal and disposable-cache
refetch behavior. It also covers offline cache state and draft quota recovery.
Durable log: `/tmp/agc-storage-recovery-audit-20261004.log`.

Against actual disposable Postgres 17, concurrent schema initialization,
disruptive migration confirmation, timestamp restoration from history and
newer-schema refusal passed under race instrumentation (4.032 seconds).
Log: `/tmp/agc-pg-migration-audit-20261004.log`. Added SQL-backed migration-boundary
negative backfill cases for changed event contents, a deleted event, a wrong
head and a missing stored projection; all passed (1.964 seconds). They verify
the expected unverifiable-history error, roll back the transaction, and compare
state, signed events/receipts and recorded head with the baseline. These are
direct migration-boundary tests, not a new manual CLI-upgrade smoke. The schema
application code was inspected: migration execution and its version record
share one advisory-locked transaction with rollback on error. Focused vet and
diff checks passed. No database schema or migration checksum was changed.
Three uncached race repetitions of the corruption and lock-timeout recovery
regressions passed together (5.366 seconds); the corruption tests are committed
as `4e19790`.

### Postgres timeout and contention evidence

The new `TestPostgresMutationLockTimeoutIsRecoverable` passed with race
instrumentation in 1.16 seconds against the disposable Postgres 17 service.
It holds the actual project serialization row lock, verifies a valid signed
write times out as `UNAVAILABLE` without advancing history, releases the lock,
and retries the identical idempotent command successfully with a verified
receipt. Its one-second timeout is test-only; production defaults are unchanged.

The existing 100-writer workload now also has a four-connection variant.
The paired uncached race invocation
`go test -race -count=1 -v ./internal/authority -run
'^TestPostgresTransactionalAuthority(BoundedPool)?$'` passed: the default
pool took 8.60 seconds and the bounded pool 7.42 seconds. Both retained the
same writer count and engine statement timeout. This does not establish that
pool size caused the earlier intermittent failures or certify server-default
five-second timeout behavior; stress diagnosis remains open.
After the dependency updates, the combined uncached race run passed again:
default pool 11.74 seconds, bounded pool 9.33 seconds, timeout recovery 1.13
seconds (23.263 seconds for the package).

### Go dependency exposure follow-up

The pinned Go 1.26.6 govulncheck run reported no called vulnerable symbols,
but found installed `golang.org/x/crypto` 0.55.0 and `golang.org/x/mod` 0.38.0
advisories at module/package level. Narrow updates to 0.56.0 and 0.40.0,
respectively, retain Go 1.26 compatibility. Upstream advisories are
[GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354),
[GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355),
[GO-2026-6179](https://pkg.go.dev/vuln/GO-2026-6179), and
[GO-2026-6180](https://pkg.go.dev/vuln/GO-2026-6180).
The SSH package enters through Sigstore's signature dependencies; no vulnerable
SSH connection symbols were reported reachable. The vulnerable sumdb client
and tile-verification packages are not in the application's package graph
(Sigstore uses the separate sumdb/note package). The unmaintained OpenPGP advisory
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) enters only through an
upstream dependency's test graph, not an application import. After the update,
pinned govulncheck exited successfully with only that module-level OpenPGP
record and no package/called-symbol findings. The opt-in uncached live
releaseverify suite passed in 21.287 seconds, including genuine artifact
acceptance, tamper/identity/issuer rejection and verifier substitution tests.
The identity suite passed in 6.141 seconds; focused vet and diff checks passed.
Evidence logs: `/tmp/agc-security-dependency-validation-20261004.log` and
`/tmp/agc-govuln-updated-20261004.json`. No advisory gate was suppressed.
All packages compiled with `go test -run '^$' -p 1 ./...`; this is compile-only
evidence, not another full test-suite pass. Fresh focused race tests passed
for codexserve (3.804 seconds), worker (25.893 seconds), protocol (1.075 seconds)
and projection (1.118 seconds). `go mod verify` passed. The Postgres regressions
are committed separately as `929ec24`; dependency changes do not alter public
commands, durable schemas or production pool/timeout defaults.

CI `37145634511` completed: Ubuntu, Windows, macOS, PostgreSQL, security and
cross-build passed; docs and landing failed the npm advisory gate. Fix revision
`e573c90` has CI `37146103289` completed with all native/core/security/build
jobs passing and both site jobs failing npm audit; deployment `37146103139`
passed. Privacy/runtime candidate `ed4d57d` CI `37147761316` completed with
all native/core/security/build jobs passing and both site jobs failing npm
audit. The
default local suite passed with `internal/app` taking 469 seconds; some other
packages used cached results, so this is not described as a wholly uncached
run. The next race run explicitly uses `-count=1` and CI's Go 1.26.6.
The restarted full race run initially on source candidate `1b7ee9d` used
`GOMAXPROCS=2 GOTOOLCHAIN=go1.26.6 go test -race -count=1 -p 1 ./...`.
Its durable evidence log is `/tmp/agc-final-race-8vlNe1.log`. It completed
with exit 1: worker compilation used the new isolation error reference against
the previously compiled codexserve package, which lacked that symbol. Runtime
source changed while this run was live. This mixed-build failure invalidates
the full-suite result; it is not counted as a pass. Individual package output
includes app 599.102 seconds and TUI 326.311 seconds, but is not an immutable
candidate certification. A fresh coupled worker/codexserve build and focused
tests pass. Subsequent full runs use an immutable Git archive of the committed
revision so ongoing source fixes cannot produce mixed-build evidence.

CI `37149065437` on `1b7ee9d` completed: all three native platforms, security,
Postgres and cross-build passed; both site jobs failed the known npm gate.
The accepted runtime-isolation/startup candidate is `492958d`. Deployment
workflow `37150049615` passed both sites. CI `37150049666` completed:
all three native platforms, security, Postgres and cross-build passed;
the run completed with both site jobs failing the unchanged npm gate.

The acknowledged-turn crash fix is pushed as `ff682f8`. The immutable source
export is `/tmp/agc-frozen-release-BzlcAI`; its `race.log` is the durable
evidence for the new full uncached race run. The export contains committed
source only, not real project runtime data or an additional Git worktree.
This run completed with exit zero: the full uncached race suite passed on
immutable `ff682f8`, including app 557.738 seconds and TUI 328.854 seconds.
Optional live provider/real Postgres tests require separate opt-in evidence;
this run does not silently count skipped tests as exercised.
Its CI `37150910694` completed with Linux, Windows, macOS, Postgres, security
and cross-build passing; both site jobs failed the unchanged npm gate.
The Go-dependency candidate `498e070` CI `37152023503` completed: all native
platform, Postgres, security and cross-build jobs passed, with both site jobs
failing the prior locked 4.2.0 advisory. The newly reproduced 4.3.0 behavioral
failure was discovered locally afterward and remains a distinct repair gate.

## Postgres migration fixture isolation

The authority and daemon integration packages run concurrently against one
configured test database in CI. Three authority tests changed global
`schema_migrations` entries: backfill restoration removed version 7,
future-schema refusal inserted version 900002, and disruptive-confirmation
inserted version 900001. Their project UUIDs did not isolate those global
changes from the other package's authority startup or mutation traffic.

A deterministic regression first reproduced contamination: with a synthetic
future-version fixture active, an unrelated `ApplySchema` rejected the shared
database as newer than this binary. The test-only helper now creates a fresh
database for each schema-mutating test, preserves connection settings, and
drops only its generated database after deferred engine/connection closure.
The regression retains future-schema refusal in the fixture while requiring
the shared database's migration check and a real unrelated `Open` to succeed.
The test role requires `CREATEDB`; the production server does not. Contributor
guidance requires a disposable test database and CI's existing role supplies
the privilege. Production code, timeout budgets and package parallelism are
unchanged.

The four targeted tests passed three race-enabled repetitions (20.783
seconds). After adding the real authority startup assertion, the full
parallel `go test -race -count=1 -v ./internal/authority/...
./internal/daemon/...` passed against disposable Postgres 17: authority
28.406 seconds, daemon 89.451 seconds. The actual 61-invocation lag/burst
test passed in 87.00 seconds. A direct database inventory afterward found
zero `agc_migration_*` databases. Vet and diff checks passed. Durable log:
`/tmp/agc-pg-isolated-migrations-race-20261004.log`.

The unchanged parallel CI coverage command also passed uncached: authority
14.167 seconds / 54.4%, daemon 7.841 seconds / 44.5%. Log:
`/tmp/agc-pg-isolated-migrations-coverage-20261004.log`.

This closes a reproduced fixture-isolation fault, not a proven explanation
of the earlier SQLSTATE 57014 or queued-delivery timeout. Those observations
remain recorded; one successful combined run does not establish zero flakes.

## ACP capability follow-up

Fresh `GOMAXPROCS=2 go test -race -count=1 ./internal/acpclient
./internal/worker` passed on c50385a (1.178 / 14.388 seconds). Separate source
review found that the public worker CLI forwards `CodexIgnoreUserConfig`
to codex-acp, which accepts the option without implementing or forwarding it.
A temporary direct regression expecting rejection failed with exit one:
`codexACPAdapter.Validate` returned nil for read-only sandbox plus requested
user-config isolation. The owned temporary probe was removed afterward.
This is a reproduced false capability assurance, not a demonstrated exploit.
The owner accepted RFC 0044 in this project chat. The permanent regression
first failed for default, read-only and workspace-write ACP configurations,
and `worker.New` incorrectly returned an executable worker. Validation now
rejects the requested option with the adapter name and native Codex exec
alternative before returning a worker. The real-service test proves project
state remains unchanged and ordinary ACP configurations stay accepted.

Three uncached race repetitions passed for worker (76.550 seconds) and
acpclient (2.463 seconds). A public-CLI regression initializes a disposable
project and proves `runtime worker --adapter codex-acp
--codex-ignore-user-config` returns the isolation diagnostic before execution
even for an unregistered runtime; three race repetitions passed in 12.173
seconds. Native exec argument tests retain the genuine isolation flag.
Flag help, generated CLI reference, worker docs and Unreleased notes are
updated. Vet, generation consistency and diff checks passed. No provider
config, credential, approval, event or schema changes. Exact resulting
candidate b89250e passed all eight jobs in CI 37179579454. Broader adapter
review remains separate; this fix does not imply the whole audit is complete.

## Multi-project live broker routing follow-up

A temporary direct broker HTTP test on b89250e registered one logical runtime
ID in two independent temporary working directories. The second registration
returned HTTP 409 because the host-shared Codex broker indexes processes by
bare runtime ID. The first registration was not overwritten. The owned probe
was removed after recording the result; no real provider ran. This proves a
multi-project registration collision, not unauthorized data disclosure.
The maintainer accepted RFC 0045. Both provider-specific permanent HTTP tests
independently reproduced the collision before scoped clients were used.
Project-aware transport keys are now shared by workers, clients and viewers,
preserving logical signed IDs and same-project conflict protection. Both
provider suites pass uncached race tests, including concurrent project prompts,
distinct processes and scoped SSE subscriptions. Focused worker/viewer race
tests and vet pass; broader candidate CI and final release closure remain pending.

## Orchestrator grant expiry follow-up

On 7315f3a, a disposable direct-transition regression reproduced acceptance
of an expired conventional HUMAN approval for both `agent.activate` and
`agent.switch-role`, including equality at the fixed authority time. All
four rejection assertions failed identically in three runs. Separate controls
prove future-approved acceptance and PENDING/CONSUMED/REJECTED rejection, with
the expiry field retained. The grant helper omits deadline validation.
The owned failing probe was removed after recording the evidence, so no
deliberately red test is committed. No real approval or principal was changed.
[RFC 0046](rfcs/0046-orchestrator-grant-approval-expiry.md) proposes honoring
expiry at new-command validation and bounded conventional-ID re-request
recovery. It is proposed, not implemented or accepted. This remains a release
audit finding requiring governance approval, not an unauthenticated escalation.

## Remaining delivery and real-provider verification

The exact historical delivery-failure regression passed 20 uncached race
repetitions on 7315f3a (`GOMAXPROCS=2 go test -race -count=20 -run
'^TestInvocationDeliveryFailureDoesNotTerminateObligation$' ./internal/service`),
40.127 seconds. This raises confidence but does not reproduce, diagnose or
resolve the previously reported intermittent failure. No retry or timeout
assertion was weakened.

Opt-in Codex/Claude smoke fixtures now exercise an owned ephemeral HTTP broker
and project-scoped register/prompt calls for two conversation turns, rather
than only the native Process.Send interface. They request no tools/file reads,
retain provider permissions/budgets, and close only their owned brokers.
The fresh Claude run registered successfully but its first prompt returned
HTTP 502 with the provider diagnostic “OAuth session expired and could not
be refreshed” (5.021 seconds). Current real-Claude scoped end-to-end evidence
is therefore incomplete pending local provider authentication. User credentials
and configuration were not changed. Earlier direct-process success is retained
as historical evidence, not substituted for this current failure.

The real Codex scoped HTTP smoke passed both turns and preserved the requested
conversation word (22.032 seconds), using the test-only explicit `gpt-5.5`
model without altering the user's default model. This proves real scoped
registration/prompt continuity, separately from fake-process collision/SSE
tests and separately from the full claim/publish worker pipeline.

## Candidate Windows cleanup-fixture failure

The test-only follow-up ced2173 passed seven CI jobs, but Windows failed
`TestProcessFailedStartTerminatesChild/initialize` in workflow 37181374929.
The failure was the first read of the readiness byte: TCP reset after startup
failure killed the helper. It was not the regression's live-child timeout.
The fixture now waits for parent acknowledgment of readiness before serving
the deliberately failing RPC. Ten uncached race repetitions passed (23.688
seconds). Commit c025898 contains only this test synchronization, with no
production cleanup change or relaxed assertion. Exact candidate CI 37181952570
completed successfully, including native Windows validation.

## Terminal resize-boundary follow-up

A new real-Update/WindowSize regression reproduced Overview rendering eight
rows in a 1x1 terminal. Pane-size floors and overlays were not bounded by a
final physical-frame height constraint. The local correction clips the final
frame after overlays and emits no content for zero-area terminals; the model,
navigation and ordinary pane layout remain intact. Every view plus normal,
palette and confirmation states passed three uncached race repetitions across
ten edge sizes, from 1x1 to 220x70 (222.852 seconds), including restore after
tiny/zero resize. The full affected uncached race suite passed: TUI 501.391
seconds and Codex broker 4.327 seconds. Candidate platform CI remains required
after pushing this local production correction.

A fresh authoritative CLI/MCP parity and focused navigation/viewport race run
on ced2173 passed: app 43.663 seconds, TUI 24.685 seconds. These prove actual
adapter writes, inbox order/history parity, scoped viewport/selection hit tests
and runtime-independent messaging within their fixtures, not blanket UI
usability at a physically unreadable terminal size.

The opt-in real native Codex exec worker smoke passed (18.730 seconds), using
the explicit test-only gpt-5.5 model, read-only sandbox and ignored user config.
It exercised claim, provider execution, result-message publication and final
COMPLETED invocation state with a synthetic no-tool receipt. This is separate
from the two-turn scoped HTTP broker smoke.

Complete the pending ledger with exact commands, revisions and outcomes;
resolve validated release blockers without weakening integrity or security
gates; repeat affected checks after fixes; verify candidate platform CI and
site deployment; report any explicit supported-scope limits. Do not call the
audit complete while a required surface is untested or a gate remains red.

## RFC 0046 accepted expiry enforcement

The maintainer accepted RFC 0046. Before editing, regressions reproduced past
and equal expiry acceptance on both grant routes, and rejected recovery of
expired PENDING/APPROVED records. Personal-authority regressions independently
reproduced both expired grants through signed commands.

The shared validator now checks optional expiry against authority time on
both grant routes. Only exact conventional HUMAN grant records that are
CONSUMED or expired PENDING/APPROVED may be re-requested; replacement remains
PENDING and needs fresh human approval. The TUI eligibility check also honors
expiry. No projection enforcement was added: historical expired-grant events
still replay to the same role and consumed approval.

Three uncached focused race repetitions passed: protocol 1.077s, projection
1.025s, personal authority 4.267s, real disposable Postgres authority 3.410s.
Both backend workflows proved rejected grants add no event, replacement loses
the previous approver, fresh approval permits one grant, and reuse rejects.
The full affected uncached race suite passed: protocol 1.226s, projection
1.095s, personal authority 6.076s, Postgres authority 33.397s, service 66.463s,
TUI 368.813s. Focused vet and diff checks passed.

Independent security review delegation failed on the account usage limit;
the required fallback separate local review traced both validator callers,
the exact-ID/action/tier recovery conditions, TUI eligibility and projection
replacement/consumption. No additional bypass was confirmed. This is not an
independent-review success claim. Exact pushed candidate CI is still required.

The separate immutable app observation lost its tool handle and its process
subsequently disappeared without terminal output in the preserved empty log.
That result is UNKNOWN, not a pass. A current-source uncached app race run
with the longer local observation window was started only after confirming
the previous process was absent. Original whole-suite 600s timeout evidence
remains recorded; no repository timeout or assertion was weakened.

TUI frame candidate ee6b7c4 completed exact CI 37182600329 successfully.

## Exec-provider cancellation blocker

On c7c486e the opt-in worker audit regression reproduced a 100ms deadline
returning after about two seconds on both Codex and OpenCode exec paths, in
three uncached repetitions (12.087s total). Changing only child output to
`/dev/null` made both controls return in 0.10s, while inherited-output cases
remained red at 2.02s/2.01s. Direct process cancellation does not bound Go's
output-copy wait when a descendant retains the pipe. RFC 0047 proposes owned
tree cleanup plus bounded drain; closing the pipes alone is not child cleanup.
The reproducer is explicitly opt-in while this design is proposed, not counted
as a passed default-suite check. Native Windows, ACP and live broker behavior
are separate evidence requirements. The audit remains incomplete.

## Declarative prompt identity correction

Source review found the declarative prompt using the executor actor for its
Requester field. A regression with distinct synthetic principals reproduced
the error. The field now uses invocation.RequestedBy, matching native provider
prompts while preserving the executor preamble and existing signed routing.
The full uncached worker/ACP race suites passed (18.452s/1.209s); vet passed.
The opt-in deadline audit remains intentionally red when enabled and skipped
in the default suite; those default passes do not resolve that blocker.

Repository integrity verified all 545 signed events; doctor is healthy with
no findings for the installed v0.8.2 toolkit, not the newer source candidate.

## RFC 0047 implementation validation

The maintainer accepted RFC 0047. Exec adapters now share invocation-owned
process supervision: Unix starts a new group; Windows starts suspended,
assigns an unnamed kill-on-close job, and resumes only after ownership is
established. Context cancellation kills that group/job; output drain is bounded
to one second after direct-process exit. Shared live brokers are not killed.
Windows ownership failure returns an error without running the provider.

The original inherited-output regression passed three uncached race repetitions
(2.266s), and is now always-on on POSIX. Native self-executing fixtures exercise
both exec runners with children and grandchildren retaining output, cancellation,
and a same-executable unrelated process that remains responsive. Three race
repetitions passed (5.035s). Full claim/start/cancellation regressions passed
three repetitions (3.867s), proving WAITING, bounded reason, no result message,
and descendant connection closure. The first version of that fixture incorrectly
expected a nil Run error; source confirmed WAITING intentionally returns a
failure, and the corrected assertion requires that failure rather than hiding it.

Final full worker/ACP race suites passed (23.066s/1.282s), including successful
output, nonzero exit, cancel-before-start and large output/UTF-8 diagnostic
bounds. Vet passed. The real read-only, ignore-user-config Codex exec worker
smoke passed (16.098s). Windows test-binary cross-compilation passed; native
Windows/macOS outcomes remain required on the pushed candidate. Docs generated
reference consistency and complete docs check/build/content verification passed.

This supervision is not a security sandbox: Unix children deliberately leaving
the group require host/provider containment. An abrupt Windows parent crash
between suspended creation and job assignment is not covered by ordinary
cancellation proof; host supervision remains necessary. No ACP or shared broker
crash-cleanup claim is inferred from these exec tests.

The separately observed full app race suite completed successfully (583.425s)
on the c7c486e source state. Its 20-minute local observation window changed no
assertion or CI timeout. c7c486e passed exact CI 37184821421, and ea3c99f passed
exact CI 37185155412. Later source changes still need their own candidate CI.

## ACP direct-process reaping

A real spawned synthetic ACP agent completed initialize/new-session/prompt,
then failed the Close-reaping regression in three uncached repetitions
(0.056s): ProcessState remained nil. The red fixture explicitly reaped its
own child during cleanup so investigation did not leave zombies.

Close now kills and waits for the directly owned process once, closing its
command pipes and releasing the CommandContext wait machinery. Expected
termination exit status is cleanup success; other kill/wait errors remain
errors. Pipe-wired sessions retain their non-owning no-op close behavior.
This changes no provider child-tree policy or protocol permission rules.

Focused three-repeat race verification passed (1.184s). The final fixture
checks eight concurrent closes, actual prompt output, reaped ProcessState,
connection shutdown and repeated-close behavior. Full ACP/worker race suites
passed (1.229s/21.296s); vet and Windows test-binary compilation passed.
Exact candidate native CI remains required after pushing this correction.

## Candidate 6a0e7b8 and renewed Claude evidence

Candidate `6a0e7b80386f7db62fde4ed00791ea247ef09b88` is pushed to `dev`,
GitHub reports its signature verified, and exact CI `37186153227` passed all
eight jobs, including native Windows/macOS and unchanged coverage gates.
The superseded `55fa95e` CI passed seven jobs including Windows/macOS; its
Ubuntu job was cancelled by the newer push, so that run is not a full pass.

An immutable Git archive at `/tmp/agc-final-6a0e7b8-1npcrG` is running the
complete uncached race suite with CI's Go 1.26.6, `GOMAXPROCS=2`, `-p 1`,
and a local 20-minute package observation bound. No repository assertion or
CI timeout was changed. The whole invocation terminated successfully with
exit 0: app 593.488s, TUI 409.770s, worker 19.622s, and all remaining packages
passed. Its full log is `full-race.log` in that archive. This proves that frozen
revision; subsequent fixes require their own affected-package and candidate
checks. Opt-in real-provider and Postgres checks are separate, not inferred
from ordinary-suite skips.

Claude's two inbox updates were read in sequence: desktop authentication did
not authenticate a standalone CLI, then the owner completed local CLI login.
A fresh status check confirmed `loggedIn=true`, without reading or copying
credential values. The existing real project-scoped two-turn HTTP smoke passed
with race instrumentation (12.221 seconds). It uses a temporary project,
`dontAsk`, a USD 0.50 budget, and no-tool instructions. This proves persistent
conversation context, not arbitrary tool execution or a signed worker lifecycle.

## Delivery lease recovery: stale-snapshot backoff bypass

A deterministic regression injects the uncertain-response boundary: a delivery
reservation passes real protocol validation and is projected successfully, but
submission returns `context.DeadlineExceeded`. The dispatcher does not launch
an unconfirmed reservation and avoids duplicates before its lease expires.
However, at expiry it records a failure with a future retry timestamp, then
uses its independent pre-submit snapshot to launch another attempt immediately.
The regression failed with `expiry skipped backoff: deliveries=2 launches=1`
(0.015 seconds), without time sleeps or load-sensitive assertions.

Expiry now returns the affected invocation IDs. The current dispatch defers
those IDs until a later sync reads the newly recorded failure/backoff, leaving
unrelated pending invocations eligible. No retry duration, automatic-attempt
limit, lease duration, authority schema, or public protocol was changed.
The regression checks reservation expiry equality, the exact backoff,
no early retry, and successful notification at the retry boundary. Three
uncached race repetitions of all Dispatcher tests passed (1.080 seconds).
The unrelated-invocation regression also passes: expiry defers only the affected
request while another request is reserved, launched and notified. Final full
daemon/remote packages passed three uncached race repetitions (5.099s/1.042s);
vet and diff checks passed. New exact-candidate native CI remains required.

This establishes a recovery-policy defect independently of the historical
Postgres stress result. It is not evidence that the earlier missing notify
event was caused by an HTTP timeout, and does not close that investigation.

## Real Claude native worker lifecycle

The separate opt-in `TestManualSmokeClaudeExec` uses the actual authenticated
Claude CLI through the native adapter and the complete Worker pipeline in an
isolated personal project. It passed uncached with the race detector in 11.382
seconds. The signed invocation is COMPLETED and links a result message authored
by the executing principal containing the requested synthetic receipt. The
fixture uses `dontAsk`, a USD 0.50 budget, a 150-second execution deadline,
no session persistence, and instructions forbidding tools/files/commands or
delegation. It is skipped unless `AGENTCOMMS_CLAUDE_EXEC_SMOKE=1`, so CI skips
must not be counted as actual provider executions. This is real
claim/execute/publish/complete evidence, not arbitrary tool-workflow coverage.
The full ordinary worker package passed uncached with race instrumentation
(25.557 seconds); vet and diff checks passed.

## ACP handoff fixture and single-value action decoding

The real Claude ACP handoff initially failed (46.196s): the parent entered
WAITING with an invalid-target error and no child invocation. The fixture
requested the nickname `DAMON` while registering `claude-damon`; protocol
requires an exact active agent ID. The smoke instruction now names the exact
registered principal and forbids tools/files/commands. It also requires Run
success, a COMPLETED parent and a worker-authored nonempty result, rather than
merely logging execution errors and inspecting the child. The corrected real
Claude ACP smoke passed with race instrumentation (15.701s). Authorization,
identity resolution and provider permissions were not relaxed.

Follow-up source review found a separate strict-decoding gap: the decoder
accepted the first action JSON value without checking end-of-input. Direct
tests rejected neither a second JSON object, `null`, nor trailing garbage;
the full Worker fixture also completed/published instead of rejecting a
two-value action (red 0.218s). The decoder now requires EOF after exactly one
JSON value, allowing trailing whitespace. The Worker regression requires
WAITING with a reason, no result message and no additional invocation on
malformed action. Valid structured routing remains covered. Three uncached
race repetitions of these cases passed (5.794s). Full affected-package and
native candidate checks remain required.

Final changed-package race verification passed: worker 21.396s, ACP 1.156s,
daemon 2.355s, remote 1.027s; vet and diff checks passed. The real Claude ACP
handoff was repeated on the EOF-check source and passed (17.329s), with the
strong completion/result/routing assertions enabled. New exact-candidate CI
remains required after pushing this correction.

After the frozen full suite finished, three isolated actual-Postgres race
repetitions started for the default/bounded-pool 100-writer tests, mutation
lock-timeout recovery and queued connector delivery. Limits and assertions are
unchanged. The connector fixture now logs command/cache boundary errors and
matching projected delivery evidence if a queued status assertion fails;
diagnostics contain only synthetic fixture data. The invocation completed with
exit 0: authority 44.821s and daemon 247.731s, all three repetitions each.
This is fresh isolated success evidence, not a claimed cause of the earlier
stress failure or proof that a timing-sensitive flake can never recur.

Candidate `ae6a741a5227d468c56f50e69f094876c0907916` is signed/verified,
pushed, and passed all eight CI jobs in `37187258162`, including the delivery
lease-backoff fix. The newer strict-decoding change still requires its own CI.

## Candidate 171fdec and remaining provider paths

Candidate `171fdec48b16bdfe9025d26b668cdeb3891b9232` is pushed with a verified
signature and passed all eight native/site/security/Postgres/build jobs in
CI `37188004826`. This verifies the strict single-value action decoder and
stronger ACP handoff fixture on the actual candidate, not just its predecessor.

The real OpenCode ACP handoff and execute-denied tests both passed under race
instrumentation on this source (32.983 seconds). They use disposable projects,
90-second invocation budgets and the existing governance-deny policy. Handoff
requires a completed parent, worker-authored result and a correctly routed
follow-up; the denied shell invocation must enter WAITING with a denial reason,
not silently report completion. No provider credentials/config were changed.
These are separate from the earlier plain OpenCode exec receipt test.

## Coverage-gate evidence completeness

The gate audit reproduced a false-positive path in `coverage-floor.sh`:
it checks only parsed `ok` measurements and did not require every declared
floor to appear. The real script returned exit 0 for a missing required package,
empty successful output and a zero-coverage no-test line it did not parse.
Valid full measurements, a below-floor result and upstream exit 3 controls
behaved correctly. The synthetic real-script regression was red (3.161s).

The script now tracks measured required packages and fails if any declared
floor has no measurement. Thresholds, exclusions, test commands and failure
propagation are unchanged. The regression uses a disposable fake `go` executable
only in its child environment; it cannot invoke the developer's actual tests
recursively. It checks all positive/negative controls and is Linux-only because
the Bash coverage gate is Linux CI's contract, not a new Windows/macOS shell
dependency. Three race repetitions passed (9.656 seconds); syntax, focused vet
and diff checks passed. A fresh actual `GOMAXPROCS=2 GOTOOLCHAIN=go1.26.6
./scripts/coverage-floor.sh` completed with exit 0 and unchanged default package
deadlines. All declared floors had measurements and passed; app 342.879s/66.2%,
TUI 101.712s/80.5%, worker 6.078s/67.3%. Some unchanged packages reused Go's
test cache, so this is the actual coverage command's result, not a new fully
uncached invocation. Authority's unit-only coverage remains intentionally
excluded; its real Postgres gate and isolated race evidence are separate.
The final Linux-scoped regression passed again with race (5.232s).
Commit `c6ed603` candidate CI `37188895812` completed successfully across all
eight jobs, including native Windows/macOS and the actual Linux coverage gate.

## OpenCode live permission-routing finding

On `c6ed603`, three deterministic repetitions reproduced missing SSE directory
routing (subscription HTTP 400) and cross-session authorization at the actual
live-adapter call site. A synthetic shared stream emitted a foreign-session
edit before an owned-session read control; the runtime answered the foreign
edit with `once` using its own acceptEdits mode (worker 0.029s, client 0.011s).
No real provider, permission, credential or account configuration was changed.
The probes were removed after recording their result so no deliberately red
test is committed. This describes the pre-implementation finding;
[RFC 0048](rfcs/0048-opencode-live-permission-routing.md) was subsequently
accepted by the maintainer on 2026-10-04.
The release audit remains open; this finding is not cleared by prior single-
session smokes or green CI. Real provider default permission behavior is also
a separate required check, not proven by session filtering alone.

### RFC 0048 implementation and additional native-provider blocker

SSE now uses the same directory-header helper as REST. Each watcher has an
immutable exact session binding, supplied by the live adapter before its
subscription/prompt. Foreign, absent, null, non-string and malformed session
identities never reach classification, edit gating, governance, reply or denial
tracking; an empty watcher binding answers nothing. Owned category decisions
are unchanged. New regressions cover conflicting edit gates on a broadcast,
malformed identities, encoded/empty directory headers, created/resumed adapter
sessions, an owned read/result control and stream cleanup. Three focused race
repetitions passed (client 1.235s, worker 1.140s).

The first restored worker fixture incorrectly treated `/global/health` as the
health endpoint. Actual `Health` uses unscoped GET `/session`; rejecting that
request caused fallback to the existing local server on port 4096 and synthetic
prompt attempts there. These were test setup failures, not routing evidence.
No shared server was stopped or its sessions subsequently altered. The fixture
now explicitly serves and prechecks its correct health endpoint before Execute;
the passing repetitions use only its owned httptest endpoint. This mistake is
recorded rather than calling the initial failures product regressions.

An opt-in real-provider regression starts its own ephemeral-port OpenCode
1.18.33 server in a disposable directory, verifies distinct directories/session
IDs and resumed identity, subscribes with the scoped client and installs a
deny-edit watcher. It inherits the same configuration as the production live
server; it does not force permission requests in the test environment. The
actual race run failed in 23.206s: the requested synthetic file existed despite
the deny-edit watcher. The owned server was cancelled/reaped and its temporary
directory removed. No existing provider server or account config was changed
by this real-provider probe. The earlier fixture fallback is disclosed above.

This is a separate **open release blocker**: native server permission defaults
can bypass the watcher policy rather than emitting requests the watcher can
decide. RFC 0048 fixes request routing, not request emission. Do not claim full
OpenCode live policy coverage, clear the release audit or relax permissions to
turn this red opt-in green. A separate accepted policy/lifecycle design and
native allow/deny/reuse controls are required before closure.

The fresh independent read-only candidate review found no actionable routing
bypass or introduced regression. It correctly distinguishes direct adapter
resume coverage from the public worker configuration path: existing worker
validation requires UUID session IDs, whereas native OpenCode IDs use `ses_*`.
That pre-existing explicit-resume compatibility limitation was not changed or
claimed covered by this patch. Ignored lookup response ID/directory is another
pre-existing seam requiring actual-provider evidence before any exploit claim.
The full affected package race run passed (client 1.188s, worker 22.082s);
affected vet and diff checks passed. Native candidate CI remains required.

### RFC 0049 accepted preparation amendment (2026-10-04)

The maintainer accepted runtime-owned servers and restrictive per-turn policy,
then explicitly accepted the bounded no-reply/tool-disabled synthetic preparation
message. Native PATCH appends rather than replaces rules. Preparation supplies a
fresh wildcard-deny baseline and the actual selected native agent identity;
restrictive rules must be appended and read back exactly before deleting only
that exact preparation message. Existing conversation history is not deleted.

The earlier disposable native deny-preservation prototype passed in 29.733s.
This is mechanism evidence, not production closure. Exact agent lookup no longer
reads resolved configuration or guesses defaults. The updated focused client
race tests passed in 1.117s. Three owned-server lifecycle race repetitions passed
in 3.192s; affected vet passed. The production adapter/Worker lifecycle, durable
original/managed-rule bookkeeping, exact marker response-loss recovery, actual
native attach and full candidate gates remain pending. No shared provider server
was disposed, no Claude worktree changed, and no RFC 0049 candidate was pushed.

The preparation client now accepts a freshly minted exact message ID, checks the
returned message/session/agent/user-role identity and empty content, and requires
an affirmative exact-message deletion response. Its error paths do not expose
provider response bodies. The caller must persist that ID with ownership before
the request; production durable recovery remains unwired. Synthetic regressions
cover failed requests, foreign/missing identities, unexpected content and failed
or unconfirmed cleanup. An initial fixture incorrectly expected an unescaped
directory header; it was corrected to decode the existing production routing
contract, not by changing that contract.

The actual native control now invokes these client methods. It passed in
31.741s, then passed with a seeded existing conversation in 39.008s: existing
message IDs/content were unchanged after preparation, only the exact minted
marker disappeared, and native edit-deny still prevented a file even with
acceptEdits. This remains client/mechanism evidence, not production worker or
actual attach closure.

### RFC 0049 production integration and native verification

The earlier unwired statements above describe historical checkpoints, not the
current candidate. The adapter now owns its server, persists exact original and
managed rules plus preparation identity outside the repository, holds runtime
and native-session locks, resets only its owned instance before each turn, pins
the actual selected agent, and fails closed on unknown rules or recovery state.
Worker shutdown closes and reaps the server and releases ownership locks.

Focused full affected-package race tests passed (client 1.287s, worker 25.552s).
Three module ownership/recovery/cancellation race repetitions passed in 8.148s;
affected vet and diff checks passed. Windows compile-only checks passed; this
is not native Windows execution and does not replace candidate platform CI.

Actual production `TestManualSmokeOpenCodeLiveWorkerPolicy`, with no `--pure`
server override, passed all five modes in 158.242s: plan, manual, auto, dontAsk
and acceptEdits. Required native edit requests were observed, restrictive modes
prevented edits, acceptEdits created the exact file, and durable outcomes and
one-shot resource cleanup were checked. A refusal with no provider text retains
the established WAITING outcome rather than synthesizing completion.
Actual `TestManualSmokeOpenCodeLiveWorkerReadAndGovernance` passed in 80.127s:
legitimate project read completed; shell and external-directory permission
requests were observed and denied, with no shell file or external-content leak.

Actual normal full native terminal attach passed in 50.039s, retained its owned
endpoint and rendered both turns across a reset. The owner accepted supporting
that full TUI and documenting OpenCode 1.18.33's optional `--mini` limitation.
Mini stops watching after instance disposal and remains a known-red diagnostic,
not a passing gate. An earlier mini diagnostic also exposed a test cleanup
deadlock: its output drain awaited an open PTY before stopping the child.
Cleanup now stops/reaps the child before closing PTY and uses a bounded drain.
The verified orphan server from the timed-out disposable test was killed by its
exact owned process group; unrelated provider processes were preserved.

New exact-marker inspection tests reproduced an oversized-response parsing
gap: a bounded reader truncated trailing JSON and reported a valid marker.
Inspection now reads at most limit-plus-one bytes and rejects oversize before
decoding, preserving exact identity/content and single-object checks. The
preparation race tests passed in 1.098s, including absent, foreign, malformed,
nonempty, failed and oversized response cases; provider error bodies stay out
of diagnostics. Repeated-turn/native-deny controls are running. Independent
candidate review, full-tree gates and exact candidate native CI remain pending;
RFC 0049 is not yet committed or pushed.

### RFC 0049 independent review and additional controls

The actual prior-turn and foreign-runtime `always` approval controls passed in
156.57s; real native mode-change and explicit-deny controls then passed in
63.75s (combined invocation 221.390s). The saved-approval test was subsequently
strengthened to require an observed new native edit request on the restrictive
turn; that exact version is running and is not yet claimed green.

The single fresh read-only candidate review found a concrete native-history
regression. Pinned OpenCode invokes revert cleanup before honoring noReply;
preparation could therefore delete an existing undone message tail even when
later agent lookup or policy verification failed. Independently checked pinned
provider source, then added a regression which failed because unsafe preparation
reached the prompt. The candidate now decodes native revert state and refuses
non-null state before synthetic preparation, with an actionable restore/resolve
message. Focused policy/preparation race tests passed (worker 1.742s, client
1.140s). Actual native Undo preservation still requires execution.

The reviewer also identified an unresolved reset-completion boundary: native
HTTP acknowledgment precedes teardown, whose failure may only be logged. Pinned
instance-store emits its disposal event after running disposers, whereas status
alone does not establish that point. Confirming completion and testing delayed
or failed reset is pending; do not commit this as a closed security fix yet.
Whole-tree vet and staticcheck passed before the latest Undo guard; they must be
rerun on the final candidate along with the full suite and platform CI.

### RFC 0049 reset-completion and native Undo preservation

Reset tests reproduced acceptance of acknowledgment-only, false/empty and
foreign-directory responses. Native HTTP returns before disposal; the global
project disposal event is emitted after disposers finish. The client now awaits
that exact envelope/property directory pair, with bounded cancellation, and
rejects acknowledgment without completion. Module regressions additionally
require no preparation/prompt and owned-server closure on an unconfirmed reset.

Initial actual-native reset controls exposed both legal SSE data-prefix forms
and a stream-registration race. Headers/initial server.connected do not prove
the listener is acquired. Parsing now accepts data with or without the optional
space, and reset waits for the merged heartbeat before sending the request.
This preserves fail-closed semantics but currently adds about ten seconds per
turn, explicitly documented and surfaced in preparation status.

Three actual native reset completions passed with readiness gating in 37.57s.
Actual native pending-Undo preservation passed in 48.05s: the managed turn was
refused, its process closed, and a new owned observer server confirmed exact
existing history and unchanged revert state. Combined command passed in
86.677s. Full affected-package race passed (client 1.777s, worker 25.358s).

The stronger saved-approval version previously failed the prior-turn proof
because no new edit request was observed; foreign-runtime passed. No file was
created, but refusal alone was insufficient evidence. Its prompt now explicitly
requires a fresh native tool attempt and its observed-request assertion is
unchanged. That exact strengthened run is in progress, not yet declared green.

That stronger run subsequently passed in 161.889s (prior-turn 67.76s,
foreign-runtime 93.07s). Both established a real native always grant, required a
new observed managed edit request, denied it in plan mode, and prevented the
file. The earlier failed proof remains above; no assertion was weakened.
Three reset/client and unconfirmed-reset module race repetitions passed (client
3.011s, worker 1.535s), including readiness, both SSE data-prefix forms and
acknowledgment-only failure. Whole-tree vet/staticcheck passed on the candidate.
Final actual native modes/governance/full-attach/continuity rerun is running;
whole-tree suite, native candidate platform CI and completion reconciliation
remain required before committing this as a closed fix.

Windows amd64 and Darwin arm64 affected-package compile-only checks passed on
this candidate; native platform execution still requires candidate CI. Generated
reference consistency and `npm run docs:check` passed, including 13 dependency
and release-browser tests, static build, search indexing, and verification of
30 product pages, 32 rendered pages, 123 commands and 30 MCP tools. Existing
Astro plugin/clipboard deprecation hints remain non-gating; no checks were
disabled. Project integrity verified through sequence 588 and installed v0.8.2
doctor is healthy; this installed binary is not the uncommitted source candidate.

### Final RFC 0049 native gate and whole-tree throughput control

The final actual-native, uncached race gate passed in 511.984s: full TUI attach
across reset (70.71s), conversation continuity and native deny preservation
(123.15s), read/bash/external-directory governance (125.37s), and all five
permission modes (191.63s). These exercise the production worker/adapter, not
an ask-only client substitute. The separate same-logical-runtime-ID control
using two actual projects and a shared user configuration passed in 86.604s;
owned endpoints, sessions and records stayed distinct and the first project
resumed its conversation. Its module race control passed three repetitions.

The subsequent whole-tree uncached race invocation failed at the app package's
default ten-minute deadline (601.319s); other packages passed, with no observed
race or assertion failure. This failure is retained, not treated as green.
The isolated active actor-selection test passed but took 16.38s. An A/B control
using the existing explicit build identity reduced that unchanged test to
1.23s, identifying repeated fallback executable hashing as a major test cost.
CPU profiling attempts were killed and yielded no usable profile; they do not
establish an OOM or kernel cause.

A test-only TestMain change resolves and retains the genuine test executable's
build identity once. A new regression first failed before this change and then
passed, computing the actual executable SHA-256 to prove the retained fallback
identity is not an invented constant. Production identity/daemon compatibility,
test assertions and timeouts are unchanged. Full app uncached race then passed
in 54.685s. The new whole-tree uncached race subsequently passed with exit zero,
including app 84.959s, TUI 420.326s and worker 31.327s. Exact candidate CI remains
required.

The unchanged coverage-floor script subsequently passed on the expanded
candidate (app 66.2%, TUI 80.5%, worker 73.6%, opencodeclient 74.7%; every
declared floor checked). Whole-tree vet, staticcheck and diff whitespace checks
passed. Coverage used reduced package concurrency, not reduced thresholds.
Native Windows/macOS execution still awaits the new committed candidate CI.

The complete Go tree also builds successfully. Local verification did not
change thresholds, skip the timed-out test, extend its deadline, or replace
real native policy controls with mocks. This closes the local gate, not the
whole-project release audit; new exact-SHA native platform/site/security/
Postgres CI and final requirement reconciliation remain open.

### Candidate 236175c macOS routing-fixture failure

The two signed/DCO commits f59f438 and 236175c were pushed to dev. Both SSH
signatures verified locally against the configured public signing key. GitHub
reported administrative bypass of the PR/check requirements; this is not CI or
release approval. Exact CI 37212879862 passed security and Postgres, but the
macOS portable worker test failed: its fixture expected `/var/...` while the
worker correctly resolved the project to `/private/var/...`.

An explicit Linux directory-symlink case reproduced that same header mismatch
for both created and resumed sessions (red in 0.050s). The fixture now resolves
its expected native project identity but still supplies the original alias to
the worker. It retains exact header, session, disposal-event, foreign-request
and stream-closure assertions. Both direct and aliased paths passed twenty
uncached race repetitions in 3.486s. Production routing and permissions did not
change. A fresh exact-candidate macOS run is required before closure; the
original failed job is not silently rerun or counted as green.

Full affected-package uncached race passed after the fixture correction (worker
24.140s, client 2.048s); affected vet, staticcheck and whitespace checks passed.
