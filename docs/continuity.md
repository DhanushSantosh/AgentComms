# Maintainer continuity

A short, factual inventory of what a second maintainer (or the owner, after
time away) needs to keep this project running -- written because a codebase
hardness audit found this project's single largest risk is not in the code:
**295 of 295 non-bot commits in this repository's history are authored under
one human identity.** Every other number in that audit (test coverage,
static analysis, supply-chain signing, branch protection) improves with
ordinary engineering work. This one doesn't, and it's the one that actually
compounds if left alone. This document is the cheap mitigation available
today: make the "if the owner is unavailable" path a known procedure instead
of something reconstructed under pressure.

## What actually needs a person, and who that is today

As of 2026-08, the repository has exactly one collaborator
(`DhanushSantosh`, admin) and exactly one required reviewer on the protected
`release` GitHub environment (the same person). That is the entire
bus-factor surface described below -- nothing here is hypothetical or
padded.

## Secrets and keys inventory

| What | Where it lives | Who can use/rotate it |
|---|---|---|
| Release binary signing | **Keyless** (Sigstore/Cosign OIDC via `sigstore/cosign-installer` in `release.yml`) -- there is no long-lived private key to lose, back up, or hand off. Any GitHub Actions run authorized to the `release` environment can sign, because the identity being attested *is* that workflow run, not a stored secret. | Nobody holds a key; access is controlled entirely by who can approve the `release` environment (below). |
| `VERCEL_TOKEN` | GitHub Actions repository secret, used only by `deploy-sites.yml` to publish `sites/docs`/`sites/landing`. The only non-keyless secret this repository holds. | Rotate from the Vercel account's token settings, then `gh secret set VERCEL_TOKEN`. Losing it stops doc/landing site deploys, nothing else -- it has no access to signing, releases, or the authority/event log. |
| Commit signing (SSH) | Per-machine/per-identity `~/.ssh/id_ed25519` (see CONTRIBUTING.md item 7), registered on GitHub as a *signing* key, not stored in this repository at all. | Each committer (human or agent identity) generates and registers their own; there is nothing central to hand off here by design. |
| Project actor keys / elevated key | Platform credential store (OS keyring) per `docs/architecture.md`'s Integrity section, never in git history. Project-specific, not repository-specific. | Out of scope for repository continuity -- these belong to whoever runs a given Agent Comms *project*, not to this source repository. |

## The `release` environment approval gate

`release.yml` builds on every `v*` tag push but will not publish until a
human approves the protected `release` GitHub environment
(Settings → Environments → `release`). Today that's one required reviewer:
the repository owner. If a release is ever waiting on approval and the
owner is unavailable:

1. A second person needs **write access to the environment's reviewer
   list**, which requires repository admin access -- there is currently no
   one who has this but the owner. This is the actual single point of
   failure in the release pipeline, not the signing step (which is
   keyless and workflow-scoped, not person-scoped).
2. Until a second admin exists, the honest mitigation is smaller: know that
   this gate exists, know it's `Settings → Environments → release →
   Required reviewers`, and don't let "who else can click approve" stay an
   unasked question until a release is actually blocked on it.

## Admin bypass on branch protection

Both `dev` and `main` require passing status checks, one approving review
(no self-approval), and signed commits. `enforce_admins` is deliberately
`false` on both -- the owner can merge past the review-count gate
specifically, via `gh pr merge --admin`, which this project uses routinely
today since there is no second reviewer to provide a real approval. This is
not a loophole being quietly relied on; it's the documented, load-bearing
reason `enforce_admins` is off at all. **What `--admin` must never bypass:**
a real failing status check (tests, security, signoff, release-gate,
cross-build) -- those failing means something is actually broken, and admin
bypass exists for the review-count gate having no one to satisfy it, not for
ignoring a red build.

## When to revisit the DCO owner-exemption

`.github/workflows/dco.yml` exempts commits authored under
`TRUSTED_OWNER_EMAIL` from the per-commit `Signed-off-by` trailer (see
CONTRIBUTING.md item 6). This does **not** need a code change the moment an
external contributor shows up -- the exemption already only matches one
specific author email; anyone else's commit already requires its own real
sign-off today, unconditionally. There is exactly one situation that
*would* need revisiting it: if a second person is ever given commit rights
using a shared identity or the owner's own email/keys rather than their
own distinct one. That should not happen -- give a new maintainer their own
git identity and, separately, decide then whether they also warrant their
own narrow exemption (they would not, by the same reasoning the current one
rests on: the exemption exists because the author field alone already
identifies the sole rights-holder, which stops being true the moment a
second real contributor exists).

The gate that *will* start mattering on its own, with no document to update,
is the ordinary required-review count: the day a second trusted reviewer
exists, PRs should start getting real reviews instead of routine admin
bypass, and that day should be treated as the actual end of this project's
single-maintainer phase -- not a formal decision, just a change in habit
worth noticing when it arrives.
