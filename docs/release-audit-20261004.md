# Major-release audit — in progress

This record tracks the owner's final whole-project audit. It is not a release
approval or proof that every workflow works. Baseline: `abfb99c` / v0.8.2;
release-history candidate: `2521418`. Audit fixes must be checked on their own
resulting revision. Native Windows/macOS execution is provided by candidate
CI, not inferred from Linux cross-compilation.

## Coverage and evidence ledger

| Surface | Required evidence | Current state |
| --- | --- | --- |
| Whole Go tree | Full suite, vet, race, staticcheck, coverage floors | Default full suite, vet and pinned-toolchain staticcheck passed; isolated app/doctor coverage passed; full uncached race and remaining coverage floors pending |
| Identities, governance, protocol | Role/credential isolation, approval expiry/binding/consumption, replay and rejection cases | Protocol, projection, projectlifecycle, identity and personal authority packages passed three uncached repetitions; deeper source review pending |
| Storage and upgrades | Signed-history replay, tamper rejection, SQLite/cache and Postgres timestamp migration | Tests identified; real Postgres suite running |
| Shared authority and recovery | Authentication, writes, idempotency, cache lag, retry, deletion, stream admission | Uncached Postgres authority/daemon suites passed; coverage 53.5% / 44.5%; race and further stress pending |
| CLI/MCP | Actual adapter writes, result/order/history parity, generated reference consistency, failure semantics | Existing parity test inspected; broader runtime checks pending |
| TUI | Navigation/actions, constrained panes, resize, ordering, approvals, runtime-independent messaging | Default suite running; interactive PTY and source review pending |
| Runtime/providers | Local-process lifecycle, durable delivery and real installed provider smoke | Claude and explicit-model Codex live two-turn smokes passed; OpenCode stopped on denied tool requests; extra-root fix verified; ignore-user-config remains unresolved |
| Installation and release trust | Authentic installer bootstrap, genuine signature acceptance, tamper/identity/issuer rejection | Live releaseverify suite passed uncached, including all four genuine-release subtests |
| Platforms | Native Linux/Windows/macOS tests, Windows pipe-close regression, six-target/four-binary builds | All three platform jobs and cross-build passed on ed4d57d in CI 37147761316 |
| Go dependencies | govulncheck and direct/transitive exposure review | govulncheck passed: zero called vulnerabilities; unreachable dependency advisories need review |
| Site dependencies | npm audit, impact assessment and any fixes | High-severity unpatched advisory blocks current npm gate; fast-uri lock upgraded to patched 3.1.8 and moderate advisory cleared |
| Docs/landing | Build, content generation, keyboard/mobile/desktop/browser/visual and Lighthouse checks | Pre-push browser suites passed (docs 45, landing 71); candidate CI site jobs failed npm audit |
| Production deployment | Exact candidate deploy and release/version correctness | Token regression tests and production build pass; both site deployments passed on e573c90 in workflow 37146103139 |
| Repository coordination | Inbox obligations, integrity, doctor, task state and exact Git/CI refs | Claude informed before audit and updated with blockers; integrity verified, doctor clear; 2521418 and e573c90 pushed to dev |

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

### Additional runtime/stress evidence

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
- Claude's reported delivery-failure flake passed 20 race-enabled repetitions
  (`TestInvocationDeliveryFailureDoesNotTerminateObligation`). No timing
  relaxation was applied; success repetitions do not establish flake absence.
- Postgres race stress failed concurrent-load checks with statement timeouts
  while race/coverage suites saturated this four-core host. An isolated repeat
  is required to distinguish load-sensitive failure from a product regression.
  The bounded-parallelism repeat also failed the 100-concurrent-invocation
  authority case with SQLSTATE 57014 (statement timeout); no timeout was raised
  and this failure remains open despite native CI's passing non-race run.
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

Complete the pending ledger with exact commands, revisions and outcomes;
resolve validated release blockers without weakening integrity or security
gates; repeat affected checks after fixes; verify candidate platform CI and
site deployment; report any explicit supported-scope limits. Do not call the
audit complete while a required surface is untested or a gate remains red.
