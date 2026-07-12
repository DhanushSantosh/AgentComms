# Release process

Releases are readiness-based and follow Semantic Versioning: `v0.x.y-preview.n`, `v0.x.y-rc.n`, and `v0.x.y`. After v1, breaking public-contract changes require a major version.

## Promotion

1. Open a release pull request from `dev` to `main`.
2. Confirm full platform, race, vulnerability, contamination, installer, migration, and end-to-end checks.
3. Curate `CHANGELOG.md`, release notes, migrations, compatibility statements, and known limitations.
4. Obtain core-maintainer review and merge using a merge commit.
5. A release/security maintainer creates the protected SemVer tag on the resulting `main` commit.
6. Approve the protected GitHub `release` environment.
7. Automation builds and publishes binaries, checksums, SBOMs, provenance, and keyless Cosign bundles.
8. Verify a clean install and signature from the published assets.

The tag is the version source of truth. Never rebuild, replace, or silently edit assets for an existing version; publish a new version instead. Humans approve releases but do not upload local binaries.

## Hotfixes

Create `hotfix/<description>` from `main`. Use expedited review without bypassing focused CI or qualified approval. Merge the fix to `main`, release it, then merge `main` back into `dev`.

If delaying validation would cause material harm, a release/security maintainer may use the documented emergency bypass. Record the approver, reason, skipped checks, risk, and rollback plan. Run skipped checks immediately and open a retrospective within two business days. Force pushes and asset replacement remain forbidden.

## Support

Before v1, the newest preview line receives best-effort support. After v1, support the current stable minor and provide critical security fixes for the previous minor for six months after replacement.
