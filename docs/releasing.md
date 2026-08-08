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
3. Curate `CHANGELOG.md`'s new version entry in two parts, in this order:
   - A short highlights block, directly under the version heading, before
     the first `###` category: an italicized one- or two-sentence summary,
     then the changes grouped under the same category labels used below
     (`**Added**`, `**Fixed**`, `**Security**`, ...) as terse, one-line
     bullets — prefix any breaking or upgrade-relevant item with
     `**Breaking:**` and list it first in its group. This is what
     `release.yml` extracts (everything up to the first `###`) as the
     GitHub Release title/body, so keep it genuinely short: a handful of
     bullets per group, not a restatement of the detail below.
   - The full technical detail, exactly as before, under `###` category
     headings (Added/Changed/Fixed/Security/...) per Keep a Changelog.
   A first release (no prior tag) may use a single prose paragraph instead
   of grouped bullets for the highlights block — there's nothing yet to
   contrast against. Also record release notes, compatibility statements,
   and known limitations — including any new optional runtime dependency a
   worker adapter now requires (for example, Node.js/npm for the `claude-acp`
   and `codex-acp` ACP adapters, or the `opencode` binary for `opencode-acp`).
4. Obtain core-maintainer review and merge using a merge commit.
5. A release/security maintainer chooses a unique change-reflective episode
   nickname, records it in the changelog, and creates the protected annotated
   SemVer tag on the resulting `main` commit — the tag's own annotation
   message (`v<version> — "<Episode Title>"`) becomes the GitHub Release
   title via `release.yml`, so it must exist (`git tag -a`, not a
   lightweight tag).
6. Approve the protected GitHub `release` environment.
7. Automation builds and publishes binaries, checksums, SBOMs, provenance, and keyless Cosign bundles.
8. Verify a clean install and signature from the published assets.
9. Merge `main`'s new tip back into `dev` (a fast-forward or simple merge
   commit, not a rebase). Skipping this leaves `dev` missing the commit
   GitHub created when merging the release PR, which makes the *next*
   release PR show as behind its base and can fail its own DCO signoff
   check once that unsigned merge commit falls inside its diff range.

The tag is the version source of truth. Never rebuild, replace, or silently edit assets for an existing version; publish a new version instead. Humans approve releases but do not upload local binaries.

## Urgent fixes

There is no hotfix branch cut from `main`. Every release, urgent or not, is promoted from `dev` following the same Promotion steps above -- a fix branched from `main` skips `dev`'s own CI/review history and (as observed firsthand cutting v0.2.1) can silently carry a stale `main`-era `release.yml`, missing whatever release-automation improvements have landed on `dev` since the last promotion. An urgent fix still lands on `dev` first (expedited review is fine; skipping CI or qualified approval is not), then promotes to `main` immediately rather than waiting for unrelated work to be ready.

If delaying validation would cause material harm, a release/security maintainer may use the documented emergency bypass. Record the approver, reason, skipped checks, risk, and rollback plan. Run skipped checks immediately and open a retrospective within two business days. Force pushes and asset replacement remain forbidden.

## Nightly builds

A separate, developer-only channel from everything above -- not to be confused with **Beta**, which before v1.0 is what every real tagged release (`v0.1.0`, `v0.2.0`, ...) already is, and which regular users install via `install.sh`/`install.ps1`. Nightly is for developers sanity-checking `dev`'s current state, not for people who want to use the app.

`nightly.yml` builds an unstable snapshot straight from `dev`'s latest commit once a day (or on demand via `workflow_dispatch`), gated only on its own `deep-test` job passing -- no CHANGELOG entry, no PR, no protected `release` environment approval. Published as a public OCI artifact to GitHub Container Registry rather than a GitHub Release: a Release would sit in the same list real tagged releases do, sorted by publish date, and be easy to mistake for one. Binaries are still Cosign-signed with full provenance attestation and independently verifiable the same way real releases are -- they're just not wired into `install.sh`/`install.ps1`, which always install the latest real release. Nightly builds report their version as `0.0.0-nightly` so they're never mistaken for a numbered release.

Pull the latest nightly build with [`oras`](https://oras.land) -- no login required, the package is public:

```sh
oras pull ghcr.io/dhanushsantosh/agentcomms-nightly:latest
```

The `:latest` tag is overwritten every run; this is a rolling snapshot, not a version. **One-time setup note for maintainers:** the first push creates the GHCR package, which may default to private -- verify it's set to Public under [github.com/DhanushSantosh?tab=packages](https://github.com/DhanushSantosh?tab=packages) → `agentcomms-nightly` → Package settings after the first run, or the pull command above will fail for anyone without registry access.

## Support

Before v1, the newest preview line receives best-effort support. After v1, support the current stable minor and provide critical security fixes for the previous minor for six months after replacement.
