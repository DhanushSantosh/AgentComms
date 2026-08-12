// Package releaseverify verifies a downloaded release artifact against its
// Cosign bundle in pure Go, with no external process and no separately
// installed tool. It is a drop-in equivalent of:
//
//	cosign verify-blob --bundle <bundle> \
//	  --certificate-oidc-issuer <oidcIssuer> \
//	  --certificate-identity-regexp <identityRegexp> <artifact>
//
// using github.com/sigstore/sigstore-go, the official pure-Go Sigstore SDK.
// See docs/rfcs/0015-cosign-free-release-verification.md for the design
// this closes: install.sh/install.ps1 and agent-comms update previously
// hard-required a separately installed cosign CLI on PATH, or refused to
// run at all.
package releaseverify

import (
	"fmt"
	"os"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// VerifyBlob verifies that artifactPath is the exact, unmodified content a
// Sigstore-signed workflow attested at bundlePath, signed by a certificate
// whose OIDC issuer exactly matches oidcIssuer and whose subject alternative
// name (the workflow identity) matches identityRegexp.
//
// The trust anchor is Sigstore's own public TUF trust root -- fetched live
// over the network the same way a separately installed real `cosign` binary
// already does on its own first run -- never this project's own release
// infrastructure. That fetch is this function's one network dependency;
// bundle verification itself is fully offline once the trust root and
// bundle are in hand, since a `cosign verify-blob --bundle` bundle already
// contains its own transparency-log inclusion proof.
func VerifyBlob(artifactPath, bundlePath, oidcIssuer, identityRegexp string) error {
	// Check everything purely local first -- a missing/malformed bundle or
	// artifact, or an invalid identity pattern, should fail immediately
	// without an unnecessary network round-trip to fetch the Sigstore trust
	// root.
	signedEntity, err := bundle.LoadJSONFromPath(bundlePath)
	if err != nil {
		return fmt.Errorf("load bundle %s: %w", bundlePath, err)
	}

	artifact, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", artifactPath, err)
	}
	defer artifact.Close()

	certIdentity, err := verify.NewShortCertificateIdentity(oidcIssuer, "", "", identityRegexp)
	if err != nil {
		return fmt.Errorf("build certificate identity policy: %w", err)
	}

	trustedRoot, err := root.FetchTrustedRoot()
	if err != nil {
		return fmt.Errorf("fetch Sigstore trust root: %w", err)
	}

	verifier, err := verify.NewVerifier(trustedRoot,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("build Sigstore verifier: %w", err)
	}

	policy := verify.NewPolicy(verify.WithArtifact(artifact), verify.WithCertificateIdentity(certIdentity))
	if _, err := verifier.Verify(signedEntity, policy); err != nil {
		return fmt.Errorf("verify %s against %s: %w", artifactPath, bundlePath, err)
	}
	return nil
}
