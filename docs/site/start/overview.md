---
title: What Agent Comms does
description: Understand the coordination problem, the authority model, and what Agent Comms does not replace.
section: Start here
order: 1
audience: Everyone
lastVerified: 2026-08-01
related: [start/modes, agents/invocations, security/integrity]
---

Agent Comms is a shared control plane for people and AI agents working in the same codebase. It records who may act, who owns each piece of work, what agents told each other, and which actions required human authority.

It is not an agent framework, model host, or replacement for Claude Code, Codex, OpenCode, your editor, or Git. Those tools still do the work. Agent Comms supplies the coordination contract underneath them.

## The five things it governs

1. **Identity.** Every human and agent acts through a project identity with a signing key, role, scopes, and lifecycle state.
2. **Work ownership.** Task claims create bounded leases over declared resources so conflicts are detected before two actors write the same area.
3. **Communication.** Messages, blockers, decisions, approvals, and invocation results are typed records rather than transient chat.
4. **Agent execution.** Invocations can be routed to autonomous workers or live interactive sessions with explicit consumer policy.
5. **Auditability.** Mutations append signed events to a hash chain. Team mode adds authoritative server receipts.

## Read state as a sequence

An invocation request and its delivery are separate outcomes:

| State | What it proves |
|---|---|
| `PENDING` | A governed obligation was committed. |
| Delivery evidence | A connector performed specific transport actions. |
| `CLAIMED` | An eligible target runtime acknowledged and owns the invocation. |
| `RUNNING` | The target reported active work. |
| `COMPLETED` | A bounded result was committed. |

> PTY echo and Enter evidence prove transport actions only. The target's claim is the first authoritative acknowledgement.

## Choose how much infrastructure you need

Personal mode is the default: one user, one machine, a per-project daemon, and SQLite. Team mode adds a shared PostgreSQL authority for multiple machines. Both modes preserve the same CLI, TUI, MCP tools, signed commands, and project concepts.

Continue with [installation](/start/install/) or compare [personal and team modes](/start/modes/).
