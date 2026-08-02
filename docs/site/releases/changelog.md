---
title: Changelog
description: What changed in each tagged release, why it matters, and where to find the full technical detail.
section: Releases
order: 1
audience: Everyone
lastVerified: 2026-08-02
related: [guide/maintenance, security/releases]
---

Every tagged release is signed and dated. This page summarizes what changed and why; the repository's [CHANGELOG.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CHANGELOG.md) carries the exhaustive per-change detail this page intentionally leaves out.

Every release below is **Beta** — before v1.0.0, SemVer's own 0.x.y convention means anything may still change without notice. There is no Stable channel yet; that label only becomes accurate once a 1.x release ships.

## v0.2.1 — "The Missing Bundle" — Beta — 2026-08-02

A hotfix restoring the Cosign-signed installer bundles that v0.2.0's CLI release was missing, so `install.sh`/`install.ps1` work again.

**Fixed**

- The published release was missing the Cosign `.bundle` file for every primary CLI binary (`agent-comms-{os}-{arch}[.exe]`) — `install.sh` and `install.ps1` both require that exact file and fail closed without it, so a fresh install of v0.2.0's CLI never worked. The daemon and server binaries were unaffected (their release-asset wildcards happened to sweep the bundle in); Cosign was already signing the CLI bundles too, they simply were never attached to the release.

## v0.2.0 — "Chain of Custody" — Beta — 2026-07-31

A managed-lifecycle and security-hardening release: safer credential handling, a distinct human-approval gate on orchestrator grants, and a truthful interactive-delivery model.

**Added**

- One-command project upgrades (`agent-comms project upgrade`) reconcile schema, binary, and daemon state automatically, with automatic backups and full post-upgrade verification.
- **Breaking:** granting the ORCHESTRATOR role now requires a separate, explicitly human-approved decision — closes a self-escalation gap where an unregistered agent operating over the ambient owner-fallback identity could grant itself the role unattended.
- A passphrase-protected elevated signing key (`agent-comms agent elevate-key`) now gates the most sensitive actions: granting ORCHESTRATOR, approving a HUMAN-tier approval, revoking another orchestrator or human principal, and deleting a revoked principal.
- Agent identities can be deleted and safely reused; every signed event now carries its signer's key fingerprint, so a reused ID's occupants stay distinguishable.
- Interactive delivery is a real, auditable state machine — no connector can falsely report a message as delivered.
- The TUI is a full control center: write actions on every panel, new Artifacts/Drafts/Environment panels, a typo-proof picker for enum fields, and a redesigned Runtimes and delivery-pipeline view.

**Fixed**

- A duplicate `agent register` call could silently destroy an existing agent's credential with no recovery path — now rejected before any credential is generated.
- MCP's `agent_register` tool could register or squat an unrelated agent identity — now enforces its documented self-registration invariant.
- Assorted authorization and Postgres reliability fixes.

## v0.1.0 — "The Control Room" — Beta — 2026-07-19

First tagged release: terminal-native, signed coordination between humans and agents — typed messages, protected work leases, approvals, artifacts, living documents — backed by either a zero-setup local SQLite authority or a shared PostgreSQL team authority, and operated through a full console TUI or a deterministic JSON CLI/MCP surface.

**Added**

- Terminal-native coordination with signed events, protected work leases, typed messages, approvals, artifacts, living documents, a deterministic JSON CLI, and MCP tools.
- Zero-setup SQLite personal authority with an on-demand per-project daemon.
- PostgreSQL team authority, local caching, resumable streams, and server-signed receipts.
- Operator-console TUI organized around Command, Work, Team, Relay, and Project hubs.
- Visible agent lifecycle controls, runtime management, invocation policies, and a searchable command palette.
- Governed project settings for lease, retention, review, summary, and artifact policy.

**Changed**

- Automatically replace incompatible local daemons through protocol negotiation.
- Keep `.agent-comms/` out of the host repository's normal Git status.
- Make arrow-key navigation, focus modes, action availability, and signed-change review explicit in the TUI.
- Recover cache gaps, daemon restarts, and lost mutation responses with the original idempotency key and signed command.

**Security**

- Initialization refuses an existing `.agents` and publishes a complete runtime atomically.
- Governed mutations revalidate authorization, leases, scopes, and conflicts inside the authoritative transaction.

## Verifying a release

Every published binary is built from a tagged commit and its checksums are published alongside it. See [Verify a release](/security/releases/) for the exact steps to confirm a download matches what was actually tagged before you run it.
