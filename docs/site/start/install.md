---
title: Install Agent Comms
description: Install a signed release on Linux, macOS, or Windows and verify that the user-level binary is active.
section: Start here
order: 2
audience: Human operators
lastVerified: 2026-09-02
related: [security/releases, guide/maintenance]
---

The installer places the CLI at user level. Existing managed projects reconcile themselves when the new binary first opens them; you do not copy a binary into every repository.

## Before installation

Nothing to install first. The commands below already point at the current release ({{LATEST_TAG}}) — copy, paste, and run. The installer verifies the tag-pinned digest of `agent-comms-verify` before using it to check the release signature; no separately installed Cosign is required (see [Verify a release](/security/releases)). Linux and macOS also require `curl` and Python 3.

## Linux and macOS

```sh
curl -fsSL "https://raw.githubusercontent.com/DhanushSantosh/AgentComms/{{LATEST_TAG}}/install.sh" | AGENT_COMMS_VERSION={{LATEST_TAG}} sh
```

The default destination is `~/.local/bin/agent-comms`. Ensure `~/.local/bin` is on `PATH`.

## Windows PowerShell

```powershell
Invoke-WebRequest "https://raw.githubusercontent.com/DhanushSantosh/AgentComms/{{LATEST_TAG}}/install.ps1" -OutFile install.ps1
.\install.ps1 -Version {{LATEST_TAG}}
```

The default destination is `%LOCALAPPDATA%\Programs\AgentComms`. The installer adds that directory to the user `PATH` when needed.

## Confirm the active build

Open a new terminal and run:

```sh
agent-comms version
agent-comms update check
```

The installer also places `agc`, a shorter synonym for `agent-comms` — `agc version`, `agc tui`, and so on all work identically. Every example in these docs uses the full name.

Installation preserves the previous binary as `agent-comms.previous` or `agent-comms.exe.previous`. Release assets are checked against SHA-256 and a Sigstore bundle before replacement. Running the installer again upgrades in place; once installed, `agent-comms update apply` is the faster way to pick up a new release than re-running the installer.

## Installing a different release

Standalone installers deliberately do not resolve mutable `stable` or `preview` channels — the commands above always target one exact, protected tag. To install an older release or a preview instead of the current one, pick its tag from the [release page](https://github.com/DhanushSantosh/AgentComms/releases) and substitute it for `{{LATEST_TAG}}` in either command above.

## Build from source

Want `dev`'s current tip rather than a numbered release — to try something not out yet, or to run a binary you built yourself instead of trusting a download? No installer, no signature to verify, just Go:

```sh
git clone https://github.com/DhanushSantosh/AgentComms.git
cd AgentComms
go build -o ./bin/agent-comms ./cmd/agent-comms
./bin/agent-comms version
```

`./bin/agent-comms` is a real, working binary built from whatever commit you cloned — `dev`'s current tip by default. It won't have `agent-comms update` or a verifiable release signature; install a signed release above for that. Building the other shipped binaries, contributing changes back, and the project's development rules are in [CONTRIBUTING.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CONTRIBUTING.md#build-from-source).
