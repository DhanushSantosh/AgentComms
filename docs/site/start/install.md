---
title: Install Agent Comms
description: Install a signed release on Linux, macOS, or Windows and verify that the user-level binary is active.
section: Start here
order: 2
audience: Human operators
lastVerified: 2026-08-01
related: [security/releases, guide/maintenance]
---

The installer places the CLI at user level. Existing managed projects reconcile themselves when the new binary first opens them; you do not copy a binary into every repository.

## Before installation

The release installers require [Cosign](https://docs.sigstore.dev/cosign/system_config/installation/) so they can verify the signed release bundle. Linux and macOS also require `curl` and Python 3.

## Linux and macOS

```sh
curl -fsSL https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.sh | sh
```

The default destination is `~/.local/bin/agent-comms`. Ensure `~/.local/bin` is on `PATH`.

To install a preview or a specific release:

```sh
AGENT_COMMS_CHANNEL=preview ./install.sh
AGENT_COMMS_VERSION=v0.2.0 ./install.sh
```

## Windows PowerShell

Download `install.ps1` from the release repository, then run:

```powershell
.\install.ps1
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

Use source builds for development, not as an unsigned substitute for release verification:

```sh
git clone https://github.com/DhanushSantosh/AgentComms.git
cd AgentComms
go build -o ./bin/agent-comms ./cmd/agent-comms
./bin/agent-comms version
```

The project currently targets the Go version declared in `go.mod`.
