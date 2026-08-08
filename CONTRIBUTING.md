# Contributing

Thank you for improving Agent Comms. All development, including urgent fixes, targets the `dev` branch; `main` only ever receives release promotions from `dev` -- never a branch cut directly from `main`. See [release process](docs/releasing.md#urgent-fixes).

1. Open an issue for material behavior, feature, schema, governance, or public-contract changes.
2. Start a short-lived branch from current `dev` and open a pull request against `dev`.
3. Keep each pull request coherent. Large feature pull requests are welcome when splitting would create incomplete or misleading states; include a review map.
4. Add focused tests and update documentation for changed behavior.
5. Keep fixtures synthetic and project-agnostic. Never commit real runtime history, credentials, leases, or private communication.
6. Sign every commit under the [Developer Certificate of Origin](https://developercertificate.org/) using `git commit -s`.
7. Every commit must also carry a real cryptographic signature (SSH or GPG) -- distinct from the DCO sign-off above, which is a text trailer, not a verifiable signature. Both `dev` and `main` require this (`required_signatures`). Set up SSH signing once per machine/agent identity:
   ```sh
   git config --global gpg.format ssh
   git config --global user.signingkey ~/.ssh/id_ed25519.pub
   git config --global commit.gpgsign true
   git config --global tag.gpgsign true
   ```
   Then add that same public key to your GitHub account as a **signing key** (not just an authentication key): `gh auth refresh -h github.com -s admin:ssh_signing_key && gh ssh-key add ~/.ssh/id_ed25519.pub --type signing`. Verify locally with `git log --show-signature`.
8. Run `go test ./...` and `go vet ./...`. CI performs the authoritative platform, race, static, vulnerability, contamination, and build checks.

Do not weaken authorization, integrity verification, authority transactions, release verification, or the contamination guard.

Public contracts, schemas, governance, signing, storage transactions, installation security, supported platforms, and major TUI navigation require an accepted RFC before implementation. See [development workflow](docs/development-workflow.md), [RFC guidance](docs/rfcs/README.md), and [maintainer guidance](docs/maintainers.md).

Contributions are licensed under Apache-2.0. Contributors retain copyright and certify their right to contribute through DCO sign-off; no separate CLA is required.
