---
title: Verify a release
description: Check release checksums, Sigstore identity, provenance, and the current operating-system signing limits.
section: Security and trust
order: 4
audience: Security reviewers
lastVerified: 2026-08-02
related: [start/install, security/integrity]
---

Official releases contain platform binaries, SHA-256 checksums, Cosign bundles, an SPDX SBOM, and GitHub build provenance.

## What the installer checks

Before replacing the user-level binary, `install.sh` and `install.ps1`:

1. select the requested stable, preview, or exact release;
2. download the binary, checksum file, and matching Cosign bundle;
3. compare the binary's SHA-256 digest;
4. verify the bundle against the expected GitHub Actions workflow identity and OIDC issuer;
5. preserve the prior binary and install the verified replacement.

Any missing asset or verification failure stops installation.

## Manual verification

Use the command printed by the installer or run the equivalent check:

```sh
cosign verify-blob \
  --bundle agent-comms-linux-amd64.bundle \
  --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  agent-comms-linux-amd64
```

Also compare the asset digest with `checksums.txt` and inspect the GitHub provenance/SBOM associated with the release.

## Platform warnings

Sigstore verification is independent of Windows Authenticode and Apple notarization. Until native platform signing is published for a release, SmartScreen or Gatekeeper may still display an operating-system warning even when checksum and Sigstore verification succeed.

## Nightly builds (developers, not for regular use)

Separate from the releases above: an unstable snapshot builds from `dev`'s latest commit daily, for developers sanity-checking current work -- not a numbered release, not installed by `install.sh`/`install.ps1`. It's published as a public OCI artifact rather than a GitHub Release, so it never appears alongside real tagged versions:

```sh
oras pull ghcr.io/dhanushsantosh/agentcomms-nightly:latest
```

No login required. The binaries are Cosign-signed and attested exactly like a real release, just under a different workflow identity:

```sh
cosign verify-blob \
  --bundle agent-comms-linux-amd64.bundle \
  --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/nightly.yml@refs/heads/dev' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  agent-comms-linux-amd64
```

The `:latest` tag is overwritten every run -- there's no history, and no immutability guarantee the way a real `vX.Y.Z` release has.
