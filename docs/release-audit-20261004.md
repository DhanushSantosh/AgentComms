# Major-release audit — in progress

This record tracks the owner's final whole-project audit. It is not a release
approval or proof that every workflow works. Baseline: `abfb99c` / v0.8.2;
release-history candidate: `2521418`. Audit fixes must be checked on their own
resulting revision. Native Windows/macOS execution is provided by candidate
CI, not inferred from Linux cross-compilation.

## Coverage and evidence ledger

| Surface | Required evidence | Current state |
| --- | --- | --- |
| Whole Go tree | Full suite, vet, race, staticcheck, coverage floors | Immutable ff682f8 full uncached race passed; 498e070 native matrix (including coverage gate), security and cross-build passed; audit source/behavior review continues |
| Identities, governance, protocol | Role/credential isolation, approval expiry/binding/consumption, replay and rejection cases | Protocol, projection, projectlifecycle, identity and personal authority packages passed three uncached repetitions; deeper source review pending |
| Storage and upgrades | Signed-history replay, tamper rejection, SQLite/cache and Postgres timestamp migration | Fresh SQLite/cache lifecycle race suite passed; actual Postgres migration/confirmation/future-schema checks and new corrupt-history/projection rejection tests passed |
| Shared authority and recovery | Authentication, writes, idempotency, cache lag, retry, deletion, stream admission | Fresh parallel uncached Postgres authority/daemon race passed after test-fixture isolation (28.406 / 89.451 seconds); exact CI coverage command passed at 54.4% / 44.5%; broader source review continues |
| CLI/MCP | Actual adapter writes, result/order/history parity, generated reference consistency, failure semantics | Isolated authoritative adapter parity passed uncached (12.743 seconds); broader runtime checks pending |
| TUI | Navigation/actions, constrained panes, resize, ordering, approvals, runtime-independent messaging | Full TUI suite passed uncached (91.617 seconds); fresh-binary PTY navigation/resize/quit and runtime-independent overview passed; remaining interaction/source review pending |
| Runtime/providers | Local-process lifecycle, durable delivery and real installed provider smoke | Claude and explicit-model Codex live two-turn smokes passed; OpenCode no-tool worker smoke passed; extra-root, accepted RFC 0042 isolation rejection and acknowledged-crash fixes tested; broader adapter review pending |
| Installation and release trust | Authentic installer bootstrap, genuine signature acceptance, tamper/identity/issuer rejection | Live releaseverify suite passed uncached, including all four genuine-release subtests |
| Platforms | Native Linux/Windows/macOS tests, Windows pipe-close regression, six-target/four-binary builds | All three platform jobs and cross-build passed on 498e070 in CI 37152023503 |
| Go dependencies | govulncheck and direct/transitive exposure review | Compatible x/crypto and x/mod security updates validated; refreshed scan has no called/package findings, only upstream-test-only OpenPGP module advisory |
| Site dependencies | npm audit, impact assessment and any fixes | Accepted RFC 0043 pinned patch passes 20 behavior/maintenance checks and unchanged npm audit; c50385a docs/landing/security CI jobs passed |
| Docs/landing | Build, content generation, keyboard/mobile/desktop/browser/visual and Lighthouse checks | Post-patch checks/builds, docs 45/landing 71 browser tests and unchanged Lighthouse thresholds passed locally; both site CI jobs passed on c50385a |
| Production deployment | Exact candidate deploy and release/version correctness | Both site deployments passed on c50385a in workflow 37178144647; live landing download reports v0.8.2 and docs releases/changelog serves the beta archive |
| Repository coordination | Inbox obligations, integrity, doctor, task state and exact Git/CI refs | Claude updated through sequence 516; integrity verified through 514; c50385a confirmed on origin/dev. CI 37178144687 completed successfully across all eight jobs |

The test-fixture candidate c30cae7 subsequently passed all eight CI jobs in
workflow 37179122223, including the unchanged parallel Postgres coverage gate.
This does not certify later source changes until their own candidate CI runs.

Candidate 7315f3a (accepted RFC 0045) passed all eight CI jobs in workflow
37180918042 and both site deployments in 37180918041. A fresh full uncached
race run of that exact revision is live from an immutable Git archive at
`/tmp/agc-release-candidate-7315f3a-HstUsH`, with output in `race.log`.
Do not treat a running handle or partial package output as a completed pass.
The app package subsequently hit its default ten-minute test-process deadline
(600.890 seconds); the active test had run eight seconds at the timeout.
The dump includes runnable executable-content hashing, not evidence that this
specific test deadlocked. This full invocation is not a pass. Other packages
continue in the same live process; retain its log and terminal result before
deciding the scope of an isolated rerun. Do not weaken repository assertions
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
