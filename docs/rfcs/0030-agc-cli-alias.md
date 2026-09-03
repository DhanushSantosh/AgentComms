# RFC 0030: `agc` as a short alias for the `agent-comms` CLI

## Status

**Proposed, 2026-09-02.** Owner: Dhanush Santosh. Implementation branch:
`feature/agc-alias`. Awaiting owner acceptance before implementation, per
`docs/rfcs/README.md`.

Adds a second name under which the CLI is invoked and a file the
installers place on `PATH`, so it touches the installation contract and
requires review.

## Problem and desired outcome

`agent-comms` is 11 characters and gets typed dozens of times per
session — `agent-comms invocation listen --runtime …`,
`agent-comms task claim …`, `agent-comms tui`. Every comparable tool
ships a short form (`kubectl`/`k`, `git`); users already alias it by
hand.

Desired outcome: `agc` invokes exactly the same CLI as `agent-comms`,
installed automatically, with `agent-comms` remaining the one canonical
name in every doc, help string, generated reference, and error message.
No rename, no deprecation of `agent-comms`.

## Proposed design

### 1. `agent-comms` stays canonical everywhere

The binary is still `cmd/agent-comms`. The cobra root command's `Use`
stays `"agent-comms"`. This matters beyond cosmetics:
`PersistentPreRunE` and `classifyProjectScope` compare
`cmd.CommandPath()` against literal strings
(`"agent-comms live serve"`, `"agent-comms update check"`, …), and
`cmd.CommandPath()` is built from the root's `Use`, **not** from
`os.Args[0]`. Keeping `Use` fixed means every one of those comparisons
keeps working unchanged when the binary is invoked as `agc`.

Consequence: `agc --help` prints `agent-comms` in its usage lines. That
is acceptable — `agent-comms` is the name we document, and making help
adapt to `os.Args[0]` would require rewriting those ~8 path comparisons
to be root-name-agnostic for a purely cosmetic gain. Out of scope.

No Go change to the command tree, help, or `docgen`.

### 2. The alias is an install-time artifact

**`install.sh` (Linux/macOS):** after installing `agent-comms`, create a
relative, by-name symlink in the same directory:

```sh
ln -sf agent-comms "$INSTALL_DIR/agc"
```

Relative and by-name so it keeps resolving after `agent-comms` is
replaced in place (see §3). `agent-comms.previous` is unaffected.

**`install.ps1` (Windows):** Windows symlink creation needs Developer
Mode or elevation, so instead write a static shim next to the binary:

```
InstallDir\agc.cmd   ->   @"%~dp0agent-comms.exe" %*
```

`%~dp0` is the shim's own directory, so it finds `agent-comms.exe`
beside it regardless of `PATH` order. The shim never needs updating.

### 3. `agent-comms update apply` keeps the alias valid

`installRelease` (`internal/app/cmd_update.go`) renames the running
executable in place. It currently operates on `os.Executable()`
directly. Add one line:

```go
if resolved, err := filepath.EvalSymlinks(exe); err == nil {
    exe = resolved
}
```

so `agc update apply` (which may start the process via the `agc`
symlink) always replaces the real `agent-comms` file, not the symlink.
This is correct behavior independent of this RFC — an updater invoked
through a symlink should update the target. On Linux `os.Executable()`
already resolves via `/proc/self/exe`; this makes macOS and any future
platform behave the same.

After that, the by-name `agc` symlink automatically points at the new
binary, and the Windows `.cmd` shim is unaffected. No alias-refresh
code, no new state.

### 4. Source builds

`CONTRIBUTING.md`'s "Build from source" section gains one line:

```sh
ln -s agent-comms ./bin/agc   # optional: same CLI, shorter to type
```

`Taskfile.yml`'s `install` task (`-o ~/.local/bin/agent-comms`) also
drops the symlink, matching what `install.sh` does.

### 5. Docs

One sentence added where install is first described
(`docs/site/start/install.md`, `README.md`): "The installer also places
`agc`, a synonym for `agent-comms`." Nothing else changes — every
command example stays `agent-comms …`.

### 6. Shell completion

`agent-comms completion <shell>` is unchanged. Completing `agc` is a
documented one-liner appended to the completion docs, e.g. for bash:
`complete -F __start_agent-comms agc`. Generating a second completion
script for `agc` is out of scope.

## Alternatives considered

- **Rename to `agc`, keep `agent-comms` as the alias.** Rejected by the
  owner: `agent-comms` is the established name across three releases,
  the sites, and every doc; the long name is the identity, the short
  one is the convenience.
- **Ship a second binary `cmd/agc`** (`func main() { app.Run(…) }`).
  Rejected: doubles the release assets (6 platforms × 2), doubles the
  self-updater's work and the checksum/signature manifest, for no
  behavior a symlink doesn't give.
- **Dynamic root `Use` from `filepath.Base(os.Args[0])`.** Rejected:
  breaks the `cmd.CommandPath()` string comparisons in
  `classifyProjectScope` / the `--output jsonl` gate; the fix (make
  them root-name-agnostic) is disproportionate to a help-text nicety.
- **`alias agc=agent-comms` in docs only, install nothing.** Rejected:
  the ask is that it works out of the box, and a shell alias does not
  survive `sudo`, scripts, cron, or non-interactive shells.

## Compatibility and rollout

Additive. No existing invocation changes. `agent-comms` keeps working
identically. An existing install gains `agc` the next time `install.sh`
/ `install.ps1` runs (including via `agent-comms update` if the user
re-runs the installer); `update apply` alone does not retroactively
create it, which is fine — it is a convenience, not a dependency.

`CHANGELOG.md` gets an **Added** entry.

## Security and privacy implications

Minimal. `install.sh` creates one extra symlink in a directory it
already writes to; `install.ps1` writes one extra 1-line `.cmd` file
there. No new download, no new network path, no change to what is
verified before install (the single signed `agent-comms` binary). The
`EvalSymlinks` call in the updater operates on a path already derived
from `os.Executable()`; it cannot redirect the update anywhere the
process was not already running from.

## Test and rollout plan

- `install.sh` / `install.ps1` smoke tests (if present in CI) assert
  `agc version` works and reports the same version as `agent-comms
  version` after a fresh install.
- Go test for §3: an `agent-comms` binary reached through a symlink,
  `installRelease` replaces the real file and the symlink still
  resolves to the new binary.
- `go test ./...`, docs-site `check`, `staticcheck ./...`.
- One squash-merged PR from `feature/agc-alias` against `dev`.

## Unresolved questions

1. Should `reconcileUserInstallation` (which already runs on most
   commands) self-heal a missing `agc` for installs that predate this
   RFC, or is "it appears on the next installer run" enough? Leaning
   enough — self-healing adds a filesystem write to the hot path of
   every command for a one-time convenience.
2. Windows: `.cmd` shim vs a real symlink (Developer Mode / elevation
   only) vs a copy of the binary (~16 MB, always works, but
   `update apply` would leave it stale). Leaning `.cmd` shim.
