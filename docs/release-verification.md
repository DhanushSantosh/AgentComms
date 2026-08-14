# Release verification

> The structured product guide is [Verify a release](site/security/releases.md). This file remains a compatibility target for existing links.

Official release assets include SHA-256 checksums, SBOMs, GitHub provenance, and Cosign bundles. Installers verify the checksum and then verify the bundle against the expected GitHub Actions workflow identity and issuer before replacing a binary -- using `agent-comms-verify`, a small companion binary built from `internal/releaseverify` (`github.com/sigstore/sigstore-go`, the official pure-Go Sigstore SDK) rather than requiring a separately installed `cosign` CLI. See [docs/rfcs/0015-cosign-free-release-verification.md](rfcs/0015-cosign-free-release-verification.md) for why. Verification failures stop installation.

Windows Authenticode and Apple notarization are deferred for the v0.1 preview. Windows SmartScreen and macOS Gatekeeper may therefore show an operating-system warning even when checksum and Sigstore verifications succeed.
