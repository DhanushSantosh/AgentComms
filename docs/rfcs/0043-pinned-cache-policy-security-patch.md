# RFC 0043: Pinned cache-policy security patch

## Status and scope

Accepted, 2026-10-04. Owner: the project maintainer. Proposed by codex-main
during the final release audit. The owner selected "Accept RFC 0043 and
implement the patch" in this project chat. This is not an audit waiver.

## Evidence and problem

The docs' Astro dependency installs `http-cache-semantics`. Version 4.3.0 is
compatible with Astro's declared range, and the current npm advisory database
reports zero vulnerabilities after a targeted lockfile update. Behavioral
verification contradicts treating that result as complete remediation:

`node --test sites/docs/cache-policy.test.mjs` still rejects the installed
4.3.0 for shared session-cookie, `proxy-revalidate` and `no-cache` cases.
Its `satisfiesWithoutRevalidation` accepts a synthetic `max-stale=86400`
request despite the response being security-restricted. Ordinary public
responses and explicitly public cookies remain reusable in the same fixture.

The installed `index.js` matches upstream commit
`b1d4bd682fbab0252985de45219f4e7497c0067c` byte for byte:
SHA-256 `ede1cc404a492fa348eb9d97a3007a0d72aa717bd22cd86a56bd0824c19729ca`.
The private-cookie policy is shared, storable and has zero maximum age;
the request is nevertheless accepted. This excludes a stale installation or
an accidentally private-cache fixture as the explanation.

References:

- [GHSA-ch52-4w7c-c8xp](https://github.com/advisories/GHSA-ch52-4w7c-c8xp)
- [Upstream issue 56](https://github.com/kornelski/http-cache-semantics/issues/56)
- [Published source](https://github.com/kornelski/http-cache-semantics/blob/b1d4bd682fbab0252985de45219f4e7497c0067c/index.js)

Astro currently uses the dependency for build-time image TTL calculation,
not a deployed shared authenticated response cache. No exploit of an Agent
Comms site is claimed. The repair closes the installed dependency's known
reuse behavior rather than relying on that deployment distinction forever.

## Proposed design

1. Keep the real dependency identity and the registry integrity-pinned 4.3.0
   lockfile. Add a small documented patch under `third_party`, preserving
   upstream license, commit, original digest and bounded diff.
2. Apply it with a repository-local Node script from the root npm
   `postinstall`. Verify the exact upstream version and original source
   digest before changing the dependency. Accept the exact patched digest
   as an idempotent no-op; fail clearly for every unknown input. Confine
   writes to this workspace's resolved dependency directory.
3. Gate cached reuse before freshness/staleness decisions when the response
   is non-storable, requires `no-cache` validation, or is a shared response
   with `proxy-revalidate` or a cookie lacking the existing public/immutable
   opt-in. Preserve ordinary expiry/max-stale behavior and documented public
   cookie opt-ins. Evaluate the fix against the public dependency API, not
   only a source-text pattern.
4. Run the regression through `docs:check`, alongside unchanged npm audit.
   Verify a clean normal `npm ci` applies the patch. Installs with lifecycle
   scripts disabled must run the explicit patch command before checks/builds;
   they receive no claim of a patched install merely from lockfile presence.
5. Remove the local patch once a published upstream version passes the same
   regression. An unrecognized upstream version fails installation rather
   than being modified by a fuzzy text replacement.

No provider configuration, public CLI, signed event, database schema,
production authority timeout, deployment mode or release promotion changes.
No advisory suppression, dependency rename or invented upstream version.

## Verification and completion criteria

- Original cookie bypass reproduces on unpatched 4.2.0 and 4.3.0.
- Patched public API rejects cookie/proxy/no-cache/non-storable reuse, with
  ordinary public expiry and public/immutable cookie opt-ins still working.
- Patch installation tests cover pristine input, already-patched input,
  unknown version/hash, missing dependency and unsafe target resolution.
- Clean `npm ci`, unchanged `npm audit --audit-level=high`, docs generation,
  both site checks/builds, browser suites and candidate CI pass.
- Retain provenance and regressions when retiring the patch; record the
  verified upstream replacement and deletion in the audit ledger.

## Alternatives

Waiting for a behaviorally verified upstream release avoids maintenance but
leaves this release gate unresolved. Relying only on npm's new advisory range
does not explain the reproduced bypass. An Astro downgrade, package rename,
audit suppression or changing the test to expect insecure reuse is not a fix.

## Risks and ownership

The project temporarily maintains a transitive JavaScript security patch.
Lifecycle-disabled installations and unexpected upstream changes need explicit
diagnostics. The maintainer owns upstream monitoring and retirement. Acceptance
authorizes this bounded maintenance cost; it does not approve v1 publication
or unrelated dependency changes.
