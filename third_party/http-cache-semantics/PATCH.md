# Temporary cache-policy security patch (RFC 0043)

Dependency identity: `http-cache-semantics` 4.3.0, from the integrity-pinned npm
lockfile. Upstream: https://github.com/kornelski/http-cache-semantics,
commit `b1d4bd682fbab0252985de45219f4e7497c0067c`.

Original `index.js` SHA-256:
`ede1cc404a492fa348eb9d97a3007a0d72aa717bd22cd86a56bd0824c19729ca`.
Patched `index.js` SHA-256:
`9efa07719bad21230327f0c0a9de5a9cfee4fe695e68d80e749d26b15bff0c85`.

The bounded change inserts `reuse-guard.txt` (exported as `insertion` by
`scripts/patch-cache-policy.mjs`) immediately after `evaluateRequest` validates
request headers. It prevents freshness/max-stale reuse of non-storable,
no-cache and shared proxy-revalidate/private-cookie responses. Existing public
and immutable cookie opt-ins remain available. No other upstream code changes.

Normal root `npm ci` applies the hash-checked patch through `postinstall`.
When installing with `--ignore-scripts`, run `npm run deps:patch` explicitly
before builds/checks. The docs regression runs during `docs:check`; maintenance
tests run during patch installation. Unknown versions/hashes fail closed.
The repository-wide npm audit remains mandatory and unchanged.

This is a local security repair, not a claim that npm's upstream 4.3.0 artifact
is repaired. Remove the patch and lifecycle hook when a published upstream
version passes the same regression; record verified replacement evidence.
The maintainer owns monitoring and retirement. Upstream BSD license is retained
alongside this record; the upstream package retains its own license as well.
