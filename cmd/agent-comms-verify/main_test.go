package main

import "testing"

func TestRunRequiresAllThreeFlags(t *testing.T) {
	cases := [][]string{
		{"artifact"},
		{"--bundle", "b", "artifact"},
		{"--bundle", "b", "--certificate-identity-regexp", "r", "artifact"},
		{"--certificate-identity-regexp", "r", "--certificate-oidc-issuer", "i", "artifact"},
	}
	for _, args := range cases {
		if err := run(args); err == nil {
			t.Fatalf("expected missing-flag args %v to fail", args)
		}
	}
}

func TestRunRequiresExactlyOneArtifactPath(t *testing.T) {
	base := []string{"--bundle", "b", "--certificate-identity-regexp", "r", "--certificate-oidc-issuer", "i"}
	if err := run(base); err == nil {
		t.Fatal("expected a missing artifact path to fail")
	}
	if err := run(append(base, "artifact1", "artifact2")); err == nil {
		t.Fatal("expected more than one artifact path to fail")
	}
}

func TestRunRejectsUnknownFlags(t *testing.T) {
	if err := run([]string{"--not-a-real-flag", "value"}); err == nil {
		t.Fatal("expected an unknown flag to fail parsing")
	}
}

func TestRunFailsOnMissingBundleFile(t *testing.T) {
	// Exercises the real path through to releaseverify.VerifyBlob without
	// any network dependency -- a nonexistent bundle file fails before any
	// Sigstore trust-root fetch is attempted (see releaseverify.VerifyBlob's
	// own ordering).
	err := run([]string{
		"--bundle", "/nonexistent/path.bundle",
		"--certificate-identity-regexp", "^https://example.com/",
		"--certificate-oidc-issuer", "https://example.com",
		"/nonexistent/artifact",
	})
	if err == nil {
		t.Fatal("expected a missing bundle file to produce an error")
	}
}
