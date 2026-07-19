# Release verification

Official release assets include SHA-256 checksums, SBOMs, GitHub provenance, and Cosign bundles. Installers verify the checksum and then run `cosign verify-blob` against the expected GitHub Actions workflow identity and issuer before replacing a binary. Verification failures stop installation.

Windows Authenticode and Apple notarization are deferred for the v0.1 preview. Windows SmartScreen and macOS Gatekeeper may therefore show an operating-system warning even when checksum and Sigstore verifications succeed.
