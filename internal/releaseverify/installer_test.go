package releaseverify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallerAuthenticatesVerifierBeforeExecution(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("fixture models the linux/amd64 installer path")
	}

	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	installer := filepath.Join(repoRoot, "install.sh")
	version := "v9.9.9"
	cliName := "agent-comms-linux-amd64"
	verifierName := "agent-comms-verify-linux-amd64"

	for _, tc := range []struct {
		name      string
		validPin  bool
		wantError bool
	}{
		{name: "substituted verifier is never executed", wantError: true},
		{name: "tag-pinned verifier reaches signature verification", validPin: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			fixtures := filepath.Join(root, "fixtures")
			binDir := filepath.Join(root, "bin")
			installDir := filepath.Join(root, "install")
			marker := filepath.Join(root, "verifier-executed")
			for _, dir := range []string{fixtures, binDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			cli := []byte("fixture CLI")
			verifier := []byte("#!/bin/sh\n: > \"$MARKER\"\nexit 0\n")
			writeFixture(t, fixtures, cliName, cli, 0o644)
			writeFixture(t, fixtures, verifierName, verifier, 0o644)
			writeFixture(t, fixtures, cliName+".bundle", []byte("fixture bundle"), 0o644)
			cliDigest := sha256.Sum256(cli)
			writeFixture(t, fixtures, "checksums.txt", []byte(hex.EncodeToString(cliDigest[:])+"  "+cliName+"\n"), 0o644)

			verifierDigest := sha256.Sum256(verifier)
			pin := hex.EncodeToString(verifierDigest[:])
			if !tc.validPin {
				pin = "0000000000000000000000000000000000000000000000000000000000000000"
			}
			writeFixture(t, fixtures, "release-verifier-checksums.txt", []byte(version+" "+verifierName+" "+pin+"\n"), 0o644)

			assets := []map[string]string{}
			for _, name := range []string{cliName, verifierName, "checksums.txt", cliName + ".bundle"} {
				assets = append(assets, map[string]string{"name": name, "browser_download_url": "https://fixtures/" + name})
			}
			release, err := json.Marshal(map[string]any{"tag_name": version, "draft": false, "assets": assets})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, fixtures, "release.json", release, 0o644)

			fakeCurl := `#!/bin/sh
set -eu
url=
out=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2;;
    -*) shift;;
    *) url=$1; shift;;
  esac
done
case "$url" in
  */releases/tags/*) src=release.json;;
  */release-verifier-checksums.txt) src=release-verifier-checksums.txt;;
  *) src=${url##*/};;
esac
cp "$FIXTURES/$src" "$out"
`
			writeFixture(t, binDir, "curl", []byte(fakeCurl), 0o755)

			cmd := exec.Command("sh", installer)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FIXTURES="+fixtures,
				"MARKER="+marker,
				"AGENT_COMMS_VERSION="+version,
				"AGENT_COMMS_INSTALL_DIR="+installDir,
			)
			err = cmd.Run()
			if tc.wantError && err == nil {
				t.Fatal("installer succeeded with a verifier that did not match the trusted pin")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("installer rejected the valid pinned verifier: %v", err)
			}
			_, markerErr := os.Stat(marker)
			if tc.wantError && !os.IsNotExist(markerErr) {
				t.Fatal("untrusted verifier was executed")
			}
			if !tc.wantError && markerErr != nil {
				t.Fatalf("trusted verifier was not executed: %v", markerErr)
			}
		})
	}
}

func writeFixture(t *testing.T, dir, name string, body []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), body, mode); err != nil {
		t.Fatal(err)
	}
}
