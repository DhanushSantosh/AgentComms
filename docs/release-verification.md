# Release verification

> The structured product guide is [Verify a release](site/security/releases.md). This file remains a compatibility target for existing links.

Official release assets include SHA-256 checksums, SBOMs, GitHub provenance, and Cosign bundles. A standalone installer requires an exact version and authenticates its downloaded `agent-comms-verify` against the digest committed in that protected release tag before executing it. That verifier then checks the CLI bundle against the exact tag's GitHub Actions workflow identity and issuer. No separately installed `cosign` CLI is required. Verification failures stop installation.

Windows Authenticode and Apple notarization are deferred for the v0.1 preview. Windows SmartScreen and macOS Gatekeeper may therefore show an operating-system warning even when checksum and Sigstore verifications succeed.
