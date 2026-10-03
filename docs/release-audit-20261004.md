# Major-release audit — in progress

This record tracks the owner's final whole-project audit. It is not a release
approval or proof that every workflow works. Baseline: `abfb99c` / v0.8.2;
release-history candidate: `2521418`. Audit fixes must be checked on their own
resulting revision. Native Windows/macOS execution is provided by candidate
CI, not inferred from Linux cross-compilation.

## Coverage and evidence ledger

| Surface | Required evidence | Current state |
| --- | --- | --- |
| Whole Go tree | Full suite, vet, race, staticcheck, coverage floors | Vet passed; default suite running; remaining gates pending |
| Identities, governance, protocol | Role/credential isolation, approval expiry/binding/consumption, replay and rejection cases | Protocol, projection, projectlifecycle, identity and personal authority packages passed three uncached repetitions; deeper source review pending |
| Storage and upgrades | Signed-history replay, tamper rejection, SQLite/cache and Postgres timestamp migration | Tests identified; real Postgres suite running |
| Shared authority and recovery | Authentication, writes, idempotency, cache lag, retry, deletion, stream admission | Uncached Postgres authority/daemon suites passed; coverage 53.5% / 44.5%; race and further stress pending |
| CLI/MCP | Actual adapter writes, result/order/history parity, generated reference consistency, failure semantics | Existing parity test inspected; broader runtime checks pending |
| TUI | Navigation/actions, constrained panes, resize, ordering, approvals, runtime-independent messaging | Default suite running; interactive PTY and source review pending |
| Runtime/providers | Local-process lifecycle, durable delivery and real installed provider smoke | Fake-provider/default tests running; opt-in real provider checks pending |
| Installation and release trust | Authentic installer bootstrap, genuine signature acceptance, tamper/identity/issuer rejection | Live releaseverify suite passed uncached, including all four genuine-release subtests |
| Platforms | Native Linux/Windows/macOS tests, Windows pipe-close regression, six-target/four-binary builds | Candidate CI running; no native Windows result asserted yet |
| Go dependencies | govulncheck and direct/transitive exposure review | govulncheck passed: zero called vulnerabilities; unreachable dependency advisories need review |
| Site dependencies | npm audit, impact assessment and any fixes | High-severity unpatched advisory blocks current npm gate; fast-uri lock upgraded to patched 3.1.8 and moderate advisory cleared |
| Docs/landing | Build, content generation, keyboard/mobile/desktop/browser/visual and Lighthouse checks | Pre-push browser suites passed (docs 45, landing 71); candidate CI site jobs failed npm audit |
| Production deployment | Exact candidate deploy and release/version correctness | Landing deploy failed unauthenticated latest-release lookup with HTTP 403; token regression tests and production build pass; workflow verification pending |
| Repository coordination | Inbox obligations, integrity, doctor, task state and exact Git/CI refs | Claude informed before audit; audit AGC task active; release-history patch pushed |

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
build passed. Workflow verification remains required.

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

- Local toolchain: Go 1.27.1; CI is pinned to Go 1.26.6. Both results must be
  distinguished rather than assuming toolchain parity.
- Staticcheck v0.7.0 cannot decode Go 1.27 export data. Its local run failed
  in standard-library imports; a retry uses CI's Go 1.26.6 toolchain rather
  than changing product code to accommodate the tooling failure.
- Docker bridge creation failed because this host cannot create veth pairs.
  A separately named disposable Postgres container using host networking
  and `listen_addresses=127.0.0.1`, port 25432, is healthy. No existing project
  database or runtime was used for this integration run.
- Real-provider smoke tests are opt-in and use subscriptions. Their existence
  or a default-suite skip is not live-provider evidence.
- The prior release-readiness record is a dated baseline. Fresh advisory and
  deployment failures supersede its earlier no-blocker assessment.

## Closure requirements

Complete the pending ledger with exact commands, revisions and outcomes;
resolve validated release blockers without weakening integrity or security
gates; repeat affected checks after fixes; verify candidate platform CI and
site deployment; report any explicit supported-scope limits. Do not call the
audit complete while a required surface is untested or a gate remains red.
