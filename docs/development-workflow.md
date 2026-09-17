# Development workflow

Agent Comms uses a permanent `dev` integration branch and a release-oriented `main` branch.

## Branches

- Branch new work from `dev` using `feat/`, `fix/`, `docs/`, `test/`, `refactor/`, `build/`, `security/`, or `hotfix/` prefixes.
- Open ordinary pull requests against `dev`.
- Promote releases through a dedicated pull request from `dev` to `main`.
- Release promotion uses a merge commit so the permanent branches retain shared ancestry.
- There is no branch cut from `main`, ever, including a `hotfix/` one -- it would skip `dev`'s CI/review history and can silently carry a stale `main`-era workflow/config that later diverged on `dev`. Urgent work still uses a `hotfix/` prefix off `dev`, then promotes to `main` immediately rather than waiting for unrelated work. See [release process](releasing.md#urgent-fixes).
- Never rebase, reset, force-push, or delete `dev` or `main`.

Squash is the default for routine pull requests. A merge commit is appropriate when the commit sequence is deliberately structured and each commit is independently reviewable. Rebase merging is disabled.

## From issue to merge

Material behavior, schema, and feature work begins with an issue. Small documentation, test-only, and obvious maintenance changes may proceed directly to a pull request.

1. Confirm the issue is `Ready` and unclaimed.
2. Create a short-lived branch from current `dev`.
3. Open a draft pull request early for large or architectural work.
4. Keep one coherent concern per pull request. There is no hard line-count limit.
5. Include tests, documentation, compatibility notes, and a review map when useful.
6. Sign commits under the Developer Certificate of Origin with `git commit -s`.
7. Resolve review conversations and rerun required checks after substantive changes.

Pull request titles use Conventional Commit prefixes such as `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `build:`, `ci:`, `security:`, or `chore:`.

## Design proposals

Create a lightweight RFC before implementing changes to durable schemas, CLI
or MCP contracts, authorization, credentials, leases, signing, storage
transactions, installation security, platform support, or major TUI navigation.
RFCs describe motivation, alternatives, compatibility, rollout, security, and
verification.

Accepted decisions that should outlive a pull request are recorded as ADRs. Templates and numbering guidance live under `docs/rfcs` and `docs/adrs`.

## Verification

Git and the Go version declared by `go.mod` are the only core workstation requirements. Additional linters, Task, hooks, containers, and editors are optional; CI is authoritative.

Before requesting review, run:

```sh
go test ./...
go vet ./...
```

Behavior changes require focused tests. Bug fixes include regression coverage when practical. Governance and state-machine changes cover allowed and denied transitions. Documentation-only work does not require artificial tests.

CI uses fast pull-request checks, a complete platform matrix after integration, and full release gates. Coverage is reviewed by risk and regression rather than an arbitrary global percentage.

## Ownership and review

Contributors submit pull requests. Triage maintainers manage public work. Core maintainers review and merge routine changes. Release/security maintainers control releases, signing workflows, and private vulnerability handling.

Sensitive identity, durable-state, schema, release, installer, and public-interface paths require CODEOWNERS review. Authors cannot approve their own pull requests. Green CI supports review but does not replace it.

## AI-assisted work

AI assistance is allowed, but the human contributor remains responsible for correctness, licensing, security, privacy, and tests. Disclose material assistance for architecture, security-sensitive code, or substantial generated sections. Do not add AI watermarks or fabricated co-authors, and never send credentials, private reports, or unpublished vulnerability material to unapproved services.

## Dogfooding boundary

External contributors do not need Agent Comms. Maintainers and automated agents may use it for concurrent work in shared checkouts. Real `.agent-comms` history, credentials, leases, and communication records never enter this toolkit repository; only synthetic fixtures are allowed.
