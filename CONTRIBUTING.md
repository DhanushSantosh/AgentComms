# Contributing

Thank you for improving Agent Comms. All development, including urgent fixes, targets the `dev` branch; `main` only ever receives release promotions from `dev` -- never a branch cut directly from `main`. See [release process](docs/releasing.md#urgent-fixes).

Just want to run what's on `dev` right now, not submit a change? Skip ahead to [Build from source](#build-from-source) -- everything above it is about opening a pull request.

## Opening a pull request

1. Open an issue for material behavior, feature, schema, governance, or public-contract changes.
2. Start a short-lived branch from current `dev` and open a pull request against `dev`.
3. Keep each pull request coherent. Large feature pull requests are welcome when splitting would create incomplete or misleading states; include a review map.
4. Add focused tests and update documentation for changed behavior.
5. Keep fixtures synthetic and project-agnostic. Never commit real runtime history, credentials, leases, or private communication.
6. Sign every commit under the [Developer Certificate of Origin](https://developercertificate.org/) using `git commit -s`. Historical repository-owner identities are privately allowlisted in the `DCO_OWNER_EMAILS` repository secret; no personal contact address belongs in source files. Other contributors require a sign-off. Prefer GitHub's verified no-reply email for commit authorship. See [maintainer continuity](docs/continuity.md#when-to-revisit-the-dco-owner-exemption) for the owner-exemption policy.
7. Every commit must also carry a real cryptographic signature (SSH or GPG) -- distinct from the DCO sign-off above, which is a text trailer, not a verifiable signature. Both `dev` and `main` require this (`required_signatures`). Set up SSH signing once per machine/agent identity:
   ```sh
   git config --global gpg.format ssh
   git config --global user.signingkey ~/.ssh/id_ed25519.pub
   git config --global commit.gpgsign true
   git config --global tag.gpgsign true
   ```
   Then add that same public key to your GitHub account as a **signing key** (not just an authentication key): `gh auth refresh -h github.com -s admin:ssh_signing_key && gh ssh-key add ~/.ssh/id_ed25519.pub --type signing`. Verify locally with `git log --show-signature`.
8. Run `go test ./...` and `go vet ./...`. CI performs the authoritative platform, race, static, vulnerability, contamination, and build checks.

## Build from source

Source builds are for trying what's on `dev` before it's released, or for running a binary you built yourself instead of a download. They're unsigned and aren't a substitute for [release verification](docs/site/security/releases.md) -- install a [signed release](https://agentcomms-docs.vercel.app/start/install/) for regular use.

Build the current development branch:

```sh
git clone https://github.com/DhanushSantosh/AgentComms.git
cd AgentComms
git switch dev
version="$(git describe --tags --abbrev=0 | sed 's/^v//')-dev"
go build -ldflags "-X github.com/DhanushSantosh/AgentComms/internal/app.Version=$version -X github.com/DhanushSantosh/AgentComms/internal/buildinfo.Version=$version" -o ./bin/agent-comms ./cmd/agent-comms
./bin/agent-comms version
```

That's a working `agent-comms` built from `dev`'s current tip. It has no verifiable release signature. `agent-comms update` is available and can replace it with a verified release. Check the Build field in `agent-comms version` to identify the source revision. Builds target the Go version declared in `go.mod`.

On Windows PowerShell, use these commands after `git switch dev` to give the source build a version compatible with projects created by the latest release:

```powershell
$devVersion = (git describe --tags --abbrev=0).TrimStart('v') + '-dev'
go build -ldflags "-X github.com/DhanushSantosh/AgentComms/internal/app.Version=$devVersion -X github.com/DhanushSantosh/AgentComms/internal/buildinfo.Version=$devVersion" -o .\bin\agent-comms.exe .\cmd\agent-comms
.\bin\agent-comms.exe version
```

A source build does not install the `agc` shim automatically.

The other three shipped binaries build the same way, one `go build` each:

```sh
for cmd in agent-comms-daemon agent-comms-server agent-comms-verify; do
  go build -o "./bin/$cmd" "./cmd/$cmd"
done
```

To put your build on `PATH` instead of running it from `./bin`, either copy it into a directory already on `PATH` (e.g. `~/.local/bin` on Linux/macOS), or run `go install ./cmd/agent-comms`, which places it in `$(go env GOBIN)` or `$(go env GOPATH)/bin`. The signed-release installers also put a short `agc` alias beside `agent-comms`. For a source build, add your own: `ln -s agent-comms <dir>/agc` on Linux/macOS, or place an `agc.cmd` file next to `agent-comms.exe` on Windows containing `@"%~dp0agent-comms.exe" %*`. If you replace a dev binary manually, keep that shim in the same directory; `agent-comms update` does not create it.

## Rules for any change

Do not weaken authorization, integrity verification, authority transactions, release verification, or the contamination guard.

Public contracts, schemas, governance, signing, storage transactions, installation security, supported platforms, and major TUI navigation require an accepted RFC before implementation. See [development workflow](docs/development-workflow.md), [RFC guidance](docs/rfcs/README.md), and [maintainer guidance](docs/maintainers.md).

Contributions are licensed under Apache-2.0. Contributors retain copyright and certify their right to contribute through DCO sign-off; no separate CLA is required.
