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

Nothing to install first. Select an exact release from the [release page](https://github.com/DhanushSantosh/AgentComms/releases), then use the installer stored in that protected release tag. The installer verifies the tag-pinned digest of `agent-comms-verify` before using it to check the release signature; no separately installed Cosign is required (see [Verify a release](/security/releases)). Linux and macOS also require `curl` and Python 3.

## Linux and macOS

```sh
VERSION=vX.Y.Z # replace with the release you selected
curl -fsSL "https://raw.githubusercontent.com/DhanushSantosh/AgentComms/$VERSION/install.sh" | AGENT_COMMS_VERSION="$VERSION" sh
```

The default destination is `~/.local/bin/agent-comms`. Ensure `~/.local/bin` is on `PATH`.

To install a preview, choose its exact tag in the same way. Standalone
installers deliberately do not resolve mutable `stable` or `preview` channels.
After the first verified install, the built-in updater provides the convenient
latest-release flow:

```sh
agent-comms update check
agent-comms update apply
```

## Windows PowerShell

Download `install.ps1` from the exact release tag, then pass that same tag:

```powershell
$Version = 'vX.Y.Z' # replace with the release you selected
Invoke-WebRequest "https://raw.githubusercontent.com/DhanushSantosh/AgentComms/$Version/install.ps1" -OutFile install.ps1
.\install.ps1 -Version $Version
```

The default destination is `%LOCALAPPDATA%\Programs\AgentComms`. The installer adds that directory to the user `PATH` when needed.

## Confirm the active build

Open a new terminal and run:

```sh
agent-comms version
agent-comms update check
```

Installation preserves the previous binary as `agent-comms.previous` or `agent-comms.exe.previous`. Release assets are checked against SHA-256 and a Sigstore bundle before replacement.

## Build from source

Source builds are for development and for trying `dev` before a release, not an unsigned substitute for release verification. See [Build from source](https://github.com/DhanushSantosh/AgentComms/blob/main/CONTRIBUTING.md#build-from-source) in the contributor guide.
