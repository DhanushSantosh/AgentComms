# Contributing

Thank you for improving Agent Comms. Ordinary development targets the `dev` branch; `main` is reserved for release promotion and hotfixes.

1. Open an issue for material behavior, feature, schema, governance, or public-contract changes.
2. Start a short-lived branch from current `dev` and open a pull request against `dev`.
3. Keep each pull request coherent. Large feature pull requests are welcome when splitting would create incomplete or misleading states; include a review map.
4. Add focused tests and update documentation for changed behavior.
5. Keep fixtures synthetic and project-agnostic. Never commit real runtime history, credentials, leases, or private communication.
6. Sign every commit under the [Developer Certificate of Origin](https://developercertificate.org/) using `git commit -s`.
7. Run `go test ./...` and `go vet ./...`. CI performs the authoritative platform, race, static, vulnerability, contamination, and build checks.

Do not weaken authorization, integrity verification, authority transactions, release verification, or the contamination guard.

Public contracts, schemas, governance, signing, storage transactions, installation security, supported platforms, and major TUI navigation require an accepted RFC before implementation. See [development workflow](docs/development-workflow.md), [RFC guidance](docs/rfcs/README.md), and [maintainer guidance](docs/maintainers.md).

Contributions are licensed under Apache-2.0. Contributors retain copyright and certify their right to contribute through DCO sign-off; no separate CLA is required.
