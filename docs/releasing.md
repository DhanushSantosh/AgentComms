# Release process

Releases are readiness-based and follow Semantic Versioning: `v0.x.y-preview.n`, `v0.x.y-rc.n`, and `v0.x.y`. After v1, breaking public-contract changes require a major version.

## Release nicknames

Every release is presented as an episode of an American science-fiction series.
The canonical Git tag remains machine-safe SemVer, while the annotated tag
message and GitHub release title use:

`v<version> — “<Episode Title>”`

The episode title must:

- describe the defining user-facing change in that release;
- be short enough to scan in release lists;
- never repeat a previous release nickname;
- avoid names of real television series, characters, or episodes.

Prereleases follow the same convention. For example:
`v0.2.0-preview.1 — “Signal Lost”`.

The `v0.1.0` release is nicknamed **“The Control Room”** for its operator-console
TUI, agent controls, command palette, and resilient local control plane.

## Promotion

1. Open a release pull request from `dev` to `main`.
2. Confirm full platform, race, vulnerability, contamination, installer, and end-to-end checks.
3. Curate `CHANGELOG.md`, release notes, compatibility statements,
   and known limitations — including any new optional runtime dependency a
   worker adapter now requires (for example, Node.js/npm for the `claude-acp`
   and `codex-acp` ACP adapters, or the `opencode` binary for `opencode-acp`).
4. Obtain core-maintainer review and merge using a merge commit.
5. A release/security maintainer chooses a unique change-reflective episode
   nickname, records it in the changelog, and creates the protected annotated
   SemVer tag on the resulting `main` commit.
6. Approve the protected GitHub `release` environment.
7. Automation builds and publishes binaries, checksums, SBOMs, provenance, and keyless Cosign bundles.
8. Verify a clean install and signature from the published assets.

The tag is the version source of truth. Never rebuild, replace, or silently edit assets for an existing version; publish a new version instead. Humans approve releases but do not upload local binaries.

## Hotfixes

Create `hotfix/<description>` from `main`. Use expedited review without bypassing focused CI or qualified approval. Merge the fix to `main`, release it, then merge `main` back into `dev`.

If delaying validation would cause material harm, a release/security maintainer may use the documented emergency bypass. Record the approver, reason, skipped checks, risk, and rollback plan. Run skipped checks immediately and open a retrospective within two business days. Force pushes and asset replacement remain forbidden.

## Support

Before v1, the newest preview line receives best-effort support. After v1, support the current stable minor and provide critical security fixes for the previous minor for six months after replacement.
