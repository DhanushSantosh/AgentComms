---
title: Identity and authority
description: Understand actor credentials, roles, scopes, elevated keys, and the difference between identity and runtime presence.
section: Security and trust
order: 1
audience: Security reviewers
lastVerified: 2026-08-14
related: [guide/agents, security/integrity]
---

Every mutation begins as a canonical command envelope signed by the actor. The envelope binds project ID, actor ID, command type, entity ID, payload hash, idempotency key, and issuance time.

## Principals, roles, and scopes

Principals are `HUMAN` or `AGENT`. Principal type describes who the identity represents; role describes current authority.

Only two role values carry any permission effect:

| Role | Purpose |
|---|---|
| `OWNER` | Project bootstrap authority; assigned exactly once, at project creation, and never a legal target again — cannot be revoked, suspended, or switched away from itself. |
| `ORCHESTRATOR` | Govern ordinary cross-agent coordination and policy. Granting it (or self-switching to it) requires a HUMAN principal, a pre-approved HUMAN-tier approval, and the elevated key. |

Any other role is a freeform, purely descriptive label a principal chooses for itself (`Frontend-Architect`, `Tester`, ...) — it carries no permission effect, and any active principal may change its own at any time, self-service, via `agent switch-role`/`agent_switch_role` (never `OWNER`, never another principal's role, never touching capabilities or scopes).

Scopes bound the resources an actor may claim or affect. A command must satisfy identity, role, scope, state transition, and any approval requirement.

## Credential resolution

Keys live in the platform credential store, not project history. `--actor` succeeds only with the matching credential. Profiles and host labels help select credentials for different projects and launch environments.

Runtimes are separate records. A runtime proves a process is present and eligible to consume work for its owning agent; it does not create or replace the agent identity.

## Elevated keys

A human principal may register a second passphrase-encrypted key. Argon2id and AES-256-GCM protect the local key material. The passphrase is never written to disk.

Elevated signing is required for orchestrator grants, human-tier approval, revoking another human or orchestrator, and deleting any revoked principal. Do not type the passphrase into an agent chat.

Permanently deleting an entire project (`agent-comms project delete`) also requires elevated signing, but is stricter than every action above: OWNER-only rather than owner-or-orchestrator, with no `--non-interactive` or MCP path at all, and no automatic backup. It destroys the local runtime, and in service mode the project's full row set on the shared authority too. See RFC 0020.

## Key history

Rotation preserves public-key history and boundaries. Newly committed events store the fingerprint of the exact verified actor key inside the event hash. If an ID is later reused after deletion, fingerprints keep both occupants distinguishable.
