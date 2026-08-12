// Command agent-comms-verify is a small, standalone companion to
// agent-comms: it verifies a release artifact against its Cosign bundle
// using github.com/sigstore/sigstore-go (pure Go, no external cosign
// process), and nothing else. It exists to remove the hard prerequisite
// install.sh/install.ps1 previously had on a separately installed cosign
// CLI -- see docs/rfcs/0015-cosign-free-release-verification.md for the
// full design.
//
// Its flags deliberately mirror `cosign verify-blob --bundle`'s so it is a
// drop-in replacement in both installer scripts and in the documented
// manual-verification command:
//
//	agent-comms-verify --bundle <artifact>.bundle \
//	  --certificate-identity-regexp <regexp> \
//	  --certificate-oidc-issuer <issuer> <artifact>
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/DhanushSantosh/AgentComms/internal/releaseverify"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent-comms-verify:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("agent-comms-verify", flag.ContinueOnError)
	bundlePath := fs.String("bundle", "", "path to the Cosign bundle file (required)")
	identityRegexp := fs.String("certificate-identity-regexp", "", "regexp the signing certificate's subject alternative name must match (required)")
	oidcIssuer := fs.String("certificate-oidc-issuer", "", "the exact OIDC issuer the signing certificate must have (required)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: agent-comms-verify --bundle <artifact>.bundle --certificate-identity-regexp <regexp> --certificate-oidc-issuer <issuer> <artifact>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bundlePath == "" || *identityRegexp == "" || *oidcIssuer == "" {
		fs.Usage()
		return errors.New("--bundle, --certificate-identity-regexp, and --certificate-oidc-issuer are all required")
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("exactly one artifact path is required")
	}
	artifactPath := fs.Arg(0)

	if err := releaseverify.VerifyBlob(artifactPath, *bundlePath, *oidcIssuer, *identityRegexp); err != nil {
		return err
	}
	fmt.Printf("Verified OK: %s\n", artifactPath)
	return nil
}
