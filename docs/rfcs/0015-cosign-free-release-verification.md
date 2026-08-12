# RFC 0015: Cosign-free release verification

## Status

**Implemented on `dev`, 2026-08-12.** Per `docs/rfcs/README.md` and
`docs/development-workflow.md`'s design-proposal rule, this RFC was
reviewed and accepted before implementation began; the core verification
approach was validated with a standalone prototype against a real, live
release asset before writing this document (see "Test and rollout plan").
The design below was built as proposed, with two small real corrections
found during implementation, recorded here rather than silently edited
into the original proposal:

1. **`verify.NewSignedEntityVerifier` (used in the original prototype) is
   deprecated in `sigstore-go`** in favor of `verify.NewVerifier`, an
   identical signature — caught by `staticcheck`, not by any functional
   difference; swapped with no other change.
2. **`VerifyBlob` checks the bundle and artifact files locally before
   fetching the Sigstore trust root**, not after as the initial prototype
   did — a missing/malformed bundle or an invalid identity pattern now
   fails immediately without an unnecessary network round-trip. This also
   made `TestVerifyBlobFailsClosedOnMissingFiles` genuinely network-free,
   which the original ordering would not have allowed.

`nightly.yml`'s build loop also gained `agent-comms-verify` alongside
`release.yml`'s and `ci.yml`'s, beyond what this RFC's Compatibility
section named explicitly — a natural extension of the same design intent
(the nightly channel's own documented manual-verification instructions
now use it too), not a scope change.

**One claim in "Test and rollout plan" below could not be fulfilled as
written and is corrected here rather than quietly**: `agent-comms-verify`
does not exist in any release published before this RFC, so a genuine
live smoke-test install against a *real published release* was not
possible yet. What was actually done instead: a full dry run of
`install.sh`'s real mechanics (checksum comparison, both the pass and the
deliberate-failure case, and the actual verifier invocation) against a
real `v0.3.0` binary/bundle standing in for what the next real release
will serve, plus the freshly built `agent-comms-verify` itself — every
step genuinely executed, nothing mocked, just not through a live
`curl | sh` against a tagged release that doesn't exist yet.
`install.ps1` could not be executed at all in this environment (no
`pwsh`) and is verified by code review mirroring `install.sh`'s
proven-correct logic plus `ci.yml`'s existing PowerShell parser check;
it still needs a real Windows run before the next release ships.

## Context

`install.sh`, `install.ps1`, and `agent-comms update`'s self-update path
all verify a downloaded release binary against its Cosign bundle by
shelling out to a separately installed `cosign` CLI
(`docs/site/security/releases.md` documents the resulting manual
verification command). Today, all three hard-fail with an instruction to
go install `cosign` first if it isn't already on `PATH`
(`install.sh:8`, `install.ps1:28-32`, `cmd_update.go:43-44`).

This is real, user-facing friction that contradicts the rest of this
project's installation story: a "curl a script and get a working binary"
one-liner that actually requires a separate ~40-80MB Sigstore CLI to be
manually installed first, via its own separate instructions, on every new
machine, is not what "one command install" means to a user encountering
it for the first time. It is also an ongoing, real maintenance cost:
issue #16 (`install.ps1`'s cosign prerequisite check couldn't be satisfied
by any documented Windows cosign install method) was a direct symptom of
depending on an external tool's own install conventions rather than
controlling the whole path ourselves.

**Desired outcome:** nobody installing or self-updating Agent Comms ever
needs to separately install anything to get real, full Sigstore
verification — while keeping the actual cryptographic guarantee exactly
as strong as it is today. Dropping verification entirely (falling back to
the SHA-256 checksum alone) is explicitly out of scope: a checksum proves
the download wasn't corrupted in transit, not that it came from the real
`release.yml` GitHub Actions workflow via the real GitHub OIDC identity --
that second guarantee is the actual point, and CONTRIBUTING.md forbids
weakening release verification.

## Proposed design

**`github.com/sigstore/sigstore-go`** is the official pure-Go Sigstore
SDK -- no external process, no separately installed tool. It exposes
everything `cosign verify-blob --bundle` does: `root.FetchTrustedRoot()`
fetches Sigstore's own public TUF trust root (the same independent trust
anchor real `cosign` uses -- this project's own release infrastructure is
never the root of trust for verification, only for *distributing* the
verifier), `bundle.LoadJSONFromPath` loads a Cosign bundle file directly,
and `verify.NewShortCertificateIdentity(issuer, "", "", identityRegexp)`
+ `verify.NewSignedEntityVerifier(...).Verify(bundle, policy)` reproduce
`--certificate-oidc-issuer`/`--certificate-identity-regexp` exactly.

Two distinct problems, two distinct fixes:

1. **`agent-comms update` (self-update)**: the `agent-comms` binary
   already exists and can link a Go library directly. `internal/
   releaseverify` (new package) wraps the calls above; `cmd_update.go`'s
   `exec.LookPath("cosign")`/`exec.CommandContext(ctx, "cosign", ...)`
   pair is replaced with one direct function call. No external process,
   no `PATH` lookup, ever again, for this path.

2. **`install.sh`/`install.ps1` (first install)**: there is no
   `agent-comms` binary yet to link anything into -- this is the genuine
   bootstrap problem. The fix is a small, dedicated companion binary,
   **`cmd/agent-comms-verify`**, built from the exact same `internal/
   releaseverify` package, published as its own per-platform release
   asset (`agent-comms-verify-<os>-<arch>`) alongside `agent-comms`
   itself, checksummed in the same `checksums.txt`. The installer scripts
   download it automatically -- the same way they already download the
   main binary -- and invoke it in place of an external `cosign`. Its own
   integrity for *this one bootstrap step* rests on HTTPS-to-github.com
   plus the SHA-256 checksum, exactly the same trust bootstrap every
   `curl | sh` installer (including today's, and including
   `sigstore/cosign-installer`'s own GitHub Action) already relies on to
   fetch anything at all. This is not a weaker guarantee than today's
   "install real cosign yourself" -- a user's first-ever `cosign` install
   rests on that identical HTTPS-download trust bootstrap; nothing about
   Sigstore's model second-guesses that first step, it only strengthens
   every step after it.

`agent-comms-verify`'s CLI surface deliberately mirrors `cosign
verify-blob --bundle`'s flag names (`--bundle`, `--certificate-identity-
regexp`, `--certificate-oidc-issuer`, positional artifact path) so it is
a drop-in replacement in both installer scripts and in the documented
manual-verification command -- someone who already trusts real `cosign`
can still use it interchangeably, this isn't a proprietary format change.

## Alternatives considered

- **Auto-download the official `cosign` binary if missing.** Simpler
  (no new Go dependency or binary of our own to maintain), but makes the
  installer depend on sigstore's own release-asset naming/infrastructure
  staying stable indefinitely, and doesn't reduce total download size
  (`cosign` itself is ~40-80MB vs. `agent-comms-verify`'s measured
  ~18MB stripped). Rejected: trades a one-time build cost on our side for
  a permanent, un-auditable-by-us dependency on someone else's release
  process, for every future install.
- **Drop verification, keep the SHA-256 checksum only.** Rejected
  outright -- discussed under Context; contradicts CONTRIBUTING.md and
  weakens the documented threat model for no real benefit once the actual
  friction (a manual prerequisite) has a real fix available.
- **Vendor cosign's own CLI as a library dependency instead of
  sigstore-go directly.** `cosign` itself is increasingly built on
  `sigstore-go` internally; importing the full `cosign` module would pull
  in its signing-side dependencies (KMS providers, etc.) that a
  verify-only tool never needs. `sigstore-go` alone is the right-sized
  dependency for exactly the capability needed.

## Compatibility and rollout

- Real `cosign` remains a fully supported, documented alternative for
  manual verification (`docs/site/security/releases.md`) -- nothing about
  this RFC removes the ability to verify a release with the official tool
  independently; it only removes the *requirement* to have it for the
  installer/self-update paths to work at all.
- `release.yml`'s per-platform build loop gains one more binary
  (`agent-comms-verify`) alongside the three it already builds
  (`agent-comms`, `agent-comms-daemon`, `agent-comms-server`) --
  Cosign-signed, checksummed, and provenance-attested identically to the
  others. `ci.yml`'s `cross-build` job gains the same target so a broken
  cross-compile is caught on every PR, not just at release time.
- `go.mod` gains one new direct dependency, `github.com/sigstore/
  sigstore-go`. This is the one deliberate exception to keeping the
  dependency surface minimal (a real, tracked hardness signal for this
  project): the capability it buys -- removing a hard external
  prerequisite from every install path -- is worth the one addition, and
  no other dependency in this project does anything comparable.
- No change to the actual cryptographic guarantee: same Sigstore trust
  roots, same `certificate-identity-regexp`/`certificate-oidc-issuer`
  policy, same bundle format. This is a delivery-mechanism change, not a
  security-model change.

## Security and privacy implications

- `agent-comms-verify` performs a live network fetch of Sigstore's public
  TUF trust root on first use (`root.FetchTrustedRoot()`), the same
  network dependency real `cosign` already has on its own first run (it
  fetches and locally caches from the same public TUF mirror). This is
  not a new network dependency class introduced by this change --
  today's "install `cosign` first" path already requires exactly this,
  just via a separately installed tool instead of an automatically
  fetched one.
- `agent-comms-verify`'s own integrity for the installer bootstrap case
  is checksum-only, not Sigstore-verified -- an inherent, unavoidable
  property of any first-trust-bootstrap step (see Proposed design). It is
  not weaker than what a user's own first `cosign` install already relies
  on.
- No secrets, credentials, or private data flow through
  `agent-comms-verify` at any point -- it reads two local files (artifact,
  bundle) and makes read-only public HTTPS requests (GitHub Releases API,
  Sigstore's public TUF mirror), identical in kind to what `install.sh`/
  `install.ps1` already do today.

## Test and rollout plan

- **Prototype validated before writing this RFC**, against the real,
  live `v0.3.0` release assets (not a synthetic bundle): confirmed
  `sigstore-go` verification succeeds against the genuine
  `agent-comms-linux-amd64` binary and its published `.bundle`; confirmed
  it correctly *rejects* both a byte-flipped tampered copy of the same
  binary (real cryptographic signature-mismatch failure, not a
  checksum-based check) and a deliberately wrong certificate-identity
  regexp. A stripped, `-trimpath`-built release binary measured ~18.4MB
  and verified in ~1.2s including the TUF trust-root fetch; cross-compiled
  cleanly for `windows/amd64` with no additional build tags needed.
- Once implemented: `go test ./internal/releaseverify/...` covering the
  same three cases (valid bundle, tampered artifact, wrong identity) as
  committed regression tests, not just the throwaway prototype run above.
- Both installer scripts get a live smoke-test install against a real
  published release (not just a syntax check) before this is considered
  done.
- `agent-comms update`'s existing self-update tests continue to pass
  with the new verification path substituted in.

## Unresolved questions

- Whether `agent-comms-verify` should also gain its own thin Cosign
  self-signature in `release.yml` for defense-in-depth (so a
  security-conscious user who already has real `cosign` installed could
  independently verify the verifier itself) -- not required for this
  RFC's actual goal (removing the installer's hard prerequisite), and
  would reintroduce the exact bootstrap circularity this design
  deliberately avoids for the one first-install case that needs it.
  Left as a possible, non-blocking future hardening, not decided here.
