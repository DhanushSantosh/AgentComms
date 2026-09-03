---
title: Changelog
description: What changed in each tagged release, why it matters, and where to find the full technical detail.
section: Releases
order: 1
audience: Everyone
lastVerified: 2026-09-03
related: [guide/maintenance, security/releases]
---

Every tagged release is signed and dated. This page summarizes what changed and why; the repository's [CHANGELOG.md](https://github.com/DhanushSantosh/AgentComms/blob/main/CHANGELOG.md) carries the exhaustive per-change detail this page intentionally leaves out.

Every release below is **Beta** — before v1.0.0, SemVer's own 0.x.y convention means anything may still change without notice. There is no Stable channel yet; that label only becomes accurate once a 1.x release ships.

## v0.6.0 — "Chain of Trust" — Beta — 2026-09-03

A governance and transport-security pass: approvals now bind to the exact operation and expiry a reviewer saw, standalone installers verify against a digest pinned in the release tag instead of mutable release assets, and the shared authority service gets an application-level access token — plus two approval-reuse gaps closed in orchestrator grants and task takeovers.

**Security**

- **Breaking:** approvals for contract publication and approval-gated invocations now carry a SHA-256 subject digest and an expiry; existing action-only approvals no longer authorize these operations and must be renewed. See RFC 0025.
- **Breaking:** production `agent-comms-server` startup now requires `AGENT_COMMS_AUTHORITY_TOKEN`, alongside existing TLS and signing-key requirements. See RFC 0026.
- **Breaking:** standalone installers now require an exact release version, authenticate the downloaded verifier against six platform digests committed in that protected tag, and bind Sigstore verification to the exact requested tag. A separately installed Cosign binary is still not required. See RFC 0025.
- Orchestrator-grant and task-takeover approvals are now ID-scoped and single-use: a matching approval is consumed once used and can no longer be replayed to re-authorize the same grant or takeover indefinitely. See RFC 0023 and RFC 0024.
- Authority SSE streams now use a dedicated bounded connection pool so long-lived stream holders can no longer exhaust health-check or mutation capacity. See RFC 0025.
- Fixes two high-severity CVEs: `google.golang.org/grpc` (HTTP/2 DATA-frame-fragmentation heap exhaustion) and `fast-uri` (SSRF/host-confusion via percent-decoding and IDN canonicalization).

## v0.5.0 — "Plain Speech" — Beta — 2026-08-26

Every command's default human output changes from a raw JSON dump to a readable summary — semantic status, tables, and next-action hints, while `--json` stays byte-compatible — plus permanent, elevated-key-gated project deletion.

**Added**

- **Breaking:** a new `--output human|plain|json|jsonl` contract replaces printing raw backend JSON to a human terminal by default. `human` (the interactive default) renders concise summaries, status, and responsive tables; `plain` is the stable uncolored fallback for redirected/non-interactive output; `json` keeps the existing versioned `Envelope` unchanged (`--json` remains a supported alias); `jsonl` is a new, versioned one-record-per-line contract for naturally streaming commands (`watch`, `invocation listen`), rejected for bounded commands. Human output is intentionally not byte-compatible across versions; scripts that parse text should use `plain` or `--json`. See RFC 0022.
- **Breaking:** `agent-comms project delete` / the TUI's Danger Zone form permanently deletes a project — its local runtime always, and its entire remote row set on the shared authority in service mode too. OWNER-only, requires a registered elevated key with no fallback, and is verified independently both locally and server-side. No `--yes`, no piped-passphrase flag, and refuses outright under `--non-interactive` or from MCP. No automatic backup; this is unrecoverable by design. See RFC 0020.

## v0.4.0 — "Proof of Presence" — Beta — 2026-08-14

Identity resolution can no longer silently misattribute a signed action to the wrong actor — closing a real incident end to end — plus self-service role switching with freeform custom labels, ConPTY-backed interactive-serve on Windows, cosign-free release verification, and a deep TUI interaction audit.

**Security**

- **Breaking:** a governed write resolved through the legacy, machine-wide default-actor fallback is now refused outright whenever a project has two or more locally-registered identities to choose between, instead of silently signing under whichever one happens to be active — the exact mechanism behind a real, confirmed incident where one agent's action was signed under a different agent's identity. See RFC 0017.
- A provider session (Claude Code, Codex) now resolves its own isolated default actor, instead of inheriting the one shared, machine-wide default every concurrent session and script on the account used to read and write. See RFC 0016.
- Self-registering from a session-less caller no longer claims the shared legacy default-actor slot for every other session-less process on the account.
- The project owner's role can no longer be changed through any path at all, self-service or administrative — mirroring the existing, absolute protection `agent suspend`/`agent revoke` already give the owner.

**Added**

- **Breaking:** self-service role switching (`agent switch-role` / `agent_switch_role`) — any active principal can relabel its own role at any time, to `ORCHESTRATOR` or any freeform custom label, with no owner/orchestrator elevation required. Switching to `ORCHESTRATOR` keeps the full existing human-approval-plus-elevated-key gate. The `AGENT`/`OBSERVER` roles are removed. See RFC 0018.
- `interactive-serve` and `--takeover-pid` now work on Windows, built on ConPTY in place of `creack/pty`, a named pipe in place of a unix domain socket, and `TerminateProcess` in place of POSIX signals. See RFC 0014.
- `install.sh`/`install.ps1` and `agent-comms update` no longer require a separately installed `cosign` CLI to verify a release — a new companion binary, `agent-comms-verify`, performs the identical check with no external process. See RFC 0015.
- The TUI's command palette now has real mouse support, matching every other surface in the app.
- A new "change role" action in the TUI lets an owner/orchestrator change an already-active agent's role.

**Fixed**

- The TUI now resolves ambiguous legacy actors to the project owner instead of refusing outright — the human operating the TUI is never blocked by RFC 0017's own refusal.
- A deep TUI interaction audit found and fixed seven distinct defects in the command palette, keybindings, and focus indication: the palette could leak keystrokes into whatever row list it was opened over, the spacebar could never be typed into it, its displayed "top match" and what Enter actually ran could silently disagree, it had no mouse support at all, clicking away from an open palette navigated underneath it without closing it, the "change role" action collided with the global refresh keybinding, and there was no visual indicator that a hub tab was actively focused versus merely selected.
- `runtime verify-adapter` now checks a provider's subcommand `--help` output too, not just its top-level one.
- Unknown-flag errors now produce output on every platform instead of failing silently.
- `install.ps1`'s cosign verification path is fixed.

## v0.3.0 — "Point and Click" — Beta — 2026-08-08

A TUI you can drive with a mouse from a real-sized terminal, session-pinned interactive delivery that survives a restart, a declarative path for adding new CLI providers without touching Go, and a public marketing/docs site.

**Added**

- Full native mouse support across the TUI — click, scroll, sidebar and hub-tab navigation, double-click-to-act, and Project settings (the one view that was missing it) — plus dynamic responsive layout, so the TUI no longer requires a desktop-sized terminal.
- `--takeover-pid` safely migrates a live interactive session, and every migrated/resumed session now pins its exact provider session ID (auto-discovered for claude and opencode) instead of racing each provider CLI's own "most recent session" guess.
- A declarative JSON adapter specification system: add a new CLI provider by dropping a spec file under `.agent-comms/adapters/`, no Go changes required.
- `runtime.delete`, `task lock`, `runtime verify-adapter`, and human-readable table output by default for agent/runtime/invocation list commands.
- A public marketing site and docs site, and a nightly beta build channel.

**Fixed**

- A long tail of TUI correctness fixes found by making the interface work at real terminal sizes, including two real ANSI/text-wrapping corruption bugs (a truncated escape sequence leaking onto screen; a bordered box that could render wider than the terminal and split its own border mid-line).
- `interactive-serve --takeover-pid` now refuses outright if the calling process is itself a descendant of the target PID, instead of silently killing its own controlling terminal.
- Stale interactive-serve sockets are cleaned up automatically on startup.

**Security**

- Removed the `agy` (Google Antigravity) worker adapter and every agy-specific integration point, over a genuinely unresolved third-party Terms of Service compliance question. This never reached a tagged release, so there is nothing for an existing install to migrate away from. Full research record in the repository's `docs/backlog.md`.

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

## Nightly builds

Separate from every release above: an unstable snapshot builds from `dev`'s latest commit daily, for developers sanity-checking current work -- not a numbered release, not installed by `install.sh`/`install.ps1`, and not **Beta** either. It's published as a public OCI artifact rather than a GitHub Release, so it never appears alongside real tagged versions and carries no version history of its own -- the `:latest` tag is simply overwritten every run.

```sh
oras pull ghcr.io/dhanushsantosh/agentcomms-nightly:latest
```

No login required. The binaries are still Cosign-signed and attested exactly like a real release, just under a different workflow identity. `oras pull` fetches the whole bundle, including `agent-comms-verify` (see [Verify a release](/security/releases/)) -- no separately installed `cosign` needed:

```sh
./agent-comms-verify \
  --bundle agent-comms-linux-amd64.bundle \
  --certificate-identity-regexp '^https://github.com/DhanushSantosh/AgentComms/.github/workflows/nightly.yml@refs/heads/dev' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  agent-comms-linux-amd64
```

## Verifying a release

Every published binary is built from a tagged commit and its checksums are published alongside it. See [Verify a release](/security/releases/) for the exact steps to confirm a download matches what was actually tagged before you run it.
