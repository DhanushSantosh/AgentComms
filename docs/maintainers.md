# Maintainer guide

Agent Comms uses graduated trust:

- Contributors submit issues and pull requests.
- Triage maintainers manage issue quality, labels, and project status.
- Core maintainers review and merge routine changes.
- Release/security maintainers approve releases and handle private vulnerability reports.

New maintainers receive only the permissions required for their role. Replace individual CODEOWNERS entries with GitHub teams as the group grows.

## Public planning

Use one public GitHub Project with `Triage`, `Planned`, `Ready`, `In progress`, `In review`, `Blocked`, and `Done` states. Milestones define release scope; the project board reflects execution status. Agent Comms coordinates active local work and does not replace the public backlog.

Accepted issues are not closed merely because they are old. Incomplete support questions may receive a reminder after 30 days and close 14 days later. Draft pull requests receive a check-in after 45 inactive days. Confirmed bugs, security work, roadmap items, and accessibility issues are exempt from automated closure.

## Security response

Acknowledge credible private reports within three business days. Validate and develop fixes privately, coordinate disclosure with the reporter, release before publishing technical detail, and assign a CVE when impact warrants it. Never request credentials or exploit details in public issues.

## Repository administration

- `dev` is the default branch and accepts reviewed feature work.
- `main` accepts release promotions and documented hotfixes.
- Required checks, CODEOWNERS review, conversation resolution, stale-review dismissal, and force-push protection remain enabled.
- Fork pull requests receive no secrets and must not execute untrusted code under `pull_request_target`.
- Release workflows use least privilege, OIDC, protected environments, and immutable action references.
