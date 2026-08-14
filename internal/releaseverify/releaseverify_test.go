package releaseverify_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/DhanushSantosh/AgentComms/internal/releaseverify"
)

// These tests exercise VerifyBlob against a real, live, published release
// (pinned to v0.3.0 rather than "latest" so this test's expectations never
// drift out from under an unrelated future release) instead of a committed
// fixture -- every real release asset here is a 10-19MB compiled binary,
// too large to reasonably commit to the repository (CONTRIBUTING.md's
// "keep fixtures synthetic" guidance), and no smaller genuinely
// Sigstore-signed artifact exists to substitute. Matching this project's
// own established pattern for tests with a real external dependency
// (internal/authority's postgres_integration_test.go), they are gated
// behind an explicit opt-in environment variable and skip cleanly without
// it, so the default `go test ./...` stays fast and network-free; ci.yml's
// security job (which already assumes network access for govulncheck) sets
// it to actually exercise this live, for real, on every CI run.
const liveTestEnvVar = "AGENT_COMMS_TEST_LIVE_RELEASE_VERIFY"

const (
	pinnedReleaseTag  = "v0.3.0"
	pinnedAssetName   = "agent-comms-server-linux-arm64" // the smallest real signed asset across all published platforms
	releaseAssetURL   = "https://github.com/DhanushSantosh/AgentComms/releases/download/" + pinnedReleaseTag + "/" + pinnedAssetName
	releaseBundleURL  = releaseAssetURL + ".bundle"
	realOIDCIssuer    = "https://token.actions.githubusercontent.com"
	realIdentityRegex = `^https://github.com/DhanushSantosh/AgentComms/.github/workflows/release.yml@refs/tags/`
)

func downloadTo(t *testing.T, url, path string) {
	t.Helper()
	resp, err := http.Get(url) //nolint:gosec // fixed, hardcoded release URL, not user input
	if err != nil {
		t.Skipf("live release fixture unreachable (%v); set %s only when network access is available", err, liveTestEnvVar)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("live release fixture returned HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBlobAgainstARealPublishedRelease(t *testing.T) {
	if os.Getenv(liveTestEnvVar) == "" {
		t.Skipf("skipping: set %s=1 to run this test against a real, live release download", liveTestEnvVar)
	}

	dir := t.TempDir()
	artifactPath := filepath.Join(dir, pinnedAssetName)
	bundlePath := artifactPath + ".bundle"
	downloadTo(t, releaseAssetURL, artifactPath)
	downloadTo(t, releaseBundleURL, bundlePath)

	t.Run("accepts the genuine artifact and bundle", func(t *testing.T) {
		if err := releaseverify.VerifyBlob(artifactPath, bundlePath, realOIDCIssuer, realIdentityRegex); err != nil {
			t.Fatalf("expected the real, unmodified release asset to verify: %v", err)
		}
	})

	t.Run("rejects a tampered artifact", func(t *testing.T) {
		original, err := os.ReadFile(artifactPath)
		if err != nil {
			t.Fatal(err)
		}
		tampered := append([]byte(nil), original...)
		// Flip a byte partway through the file -- anywhere is fine, this
		// only needs to change the artifact's digest, not target a
		// specific structure.
		flipAt := len(tampered) / 2
		tampered[flipAt] ^= 0xFF
		tamperedPath := filepath.Join(dir, pinnedAssetName+".tampered")
		if err := os.WriteFile(tamperedPath, tampered, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := releaseverify.VerifyBlob(tamperedPath, bundlePath, realOIDCIssuer, realIdentityRegex); err == nil {
			t.Fatal("expected a tampered artifact to fail verification, not silently pass")
		}
	})

	t.Run("rejects a mismatched certificate identity", func(t *testing.T) {
		wrongIdentity := `^https://github.com/SomeoneElse/OtherRepo/.github/workflows/release.yml@refs/tags/`
		if err := releaseverify.VerifyBlob(artifactPath, bundlePath, realOIDCIssuer, wrongIdentity); err == nil {
			t.Fatal("expected a mismatched certificate identity regexp to fail verification, not silently pass")
		}
	})

	t.Run("rejects a mismatched OIDC issuer", func(t *testing.T) {
		wrongIssuer := "https://accounts.example.com"
		if err := releaseverify.VerifyBlob(artifactPath, bundlePath, wrongIssuer, realIdentityRegex); err == nil {
			t.Fatal("expected a mismatched OIDC issuer to fail verification, not silently pass")
		}
	})
}

func TestVerifyBlobFailsClosedOnMissingFiles(t *testing.T) {
	// No network or opt-in env var needed -- this only exercises the
	// local-file error paths, before any Sigstore trust-root fetch would
	// even be attempted for the missing-artifact case.
	dir := t.TempDir()
	if err := releaseverify.VerifyBlob(
		filepath.Join(dir, "does-not-exist"),
		filepath.Join(dir, "does-not-exist.bundle"),
		realOIDCIssuer, realIdentityRegex,
	); err == nil {
		t.Fatal("expected a missing bundle file to fail, not silently pass")
	}
}
