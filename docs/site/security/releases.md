---
title: Verify a release
description: Check release checksums, Sigstore identity, provenance, and the current operating-system signing limits.
section: Security and trust
order: 4
audience: Security reviewers
lastVerified: 2026-08-12
related: [start/install, security/integrity]
---

Official releases contain platform binaries, SHA-256 checksums, Cosign bundles, an SPDX SBOM, GitHub build provenance, and a small companion verifier binary (`agent-comms-verify`).

## What the installer checks

Before replacing the user-level binary, `install.sh` and `install.ps1`:

1. select the requested stable, preview, or exact release;
2. download the binary, checksum file, matching Cosign bundle, and the `agent-comms-verify` companion binary for your platform;
3. compare every downloaded binary's SHA-256 digest against `checksums.txt`;
4. verify the bundle against the expected GitHub Actions workflow identity and OIDC issuer, using `agent-comms-verify` -- no separately installed `cosign` CLI required;
5. preserve the prior binary and install the verified replacement.

Any missing asset or verification failure stops installation. `agent-comms update` (self-updating an already-installed copy) verifies the same way, built directly into the binary -- see [docs/rfcs/0015-cosign-free-release-verification.md](https://github.com/DhanushSantosh/AgentComms/blob/main/docs/rfcs/0015-cosign-free-release-verification.md) for why this no longer needs `cosign` on `PATH` at all.

## Manual verification

Use the command printed by the installer, or run the equivalent check with the downloaded `agent-comms-verify` binary:

```sh
./agent-comms-verify \
  --bundle agent-comms-linux-amd64.bundle \
  --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  agent-comms-linux-amd64
```

A real, separately installed [`cosign`](https://docs.sigstore.dev/cosign/system_config/installation/) remains a fully supported, independent way to run the identical check -- `agent-comms-verify`'s flags deliberately mirror `cosign verify-blob --bundle`'s exactly, so the command above works unchanged with either tool:

```sh
cosign verify-blob \
  --bundle agent-comms-linux-amd64.bundle \
  --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  agent-comms-linux-amd64
```

Also compare the asset digest with `checksums.txt` and inspect the GitHub provenance/SBOM associated with the release.

## Platform warnings

Sigstore verification is independent of Windows Authenticode and Apple notarization. Until native platform signing is published for a release, SmartScreen or Gatekeeper may still display an operating-system warning even when checksum and Sigstore verification succeed. For the developer-only nightly channel and how to verify it, see the [changelog](/releases/changelog/#nightly-builds).
