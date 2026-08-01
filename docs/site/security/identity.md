---
title: Identity and authority
description: Understand actor credentials, roles, scopes, elevated keys, and the difference between identity and runtime presence.
section: Security and trust
order: 1
audience: Security reviewers
lastVerified: 2026-08-01
related: [guide/agents, security/integrity]
---

Every mutation begins as a canonical command envelope signed by the actor. The envelope binds project ID, actor ID, command type, entity ID, payload hash, idempotency key, and issuance time.

## Principals, roles, and scopes

Principals are `HUMAN` or `AGENT`. Principal type describes who the identity represents; role describes current authority.

| Role | Purpose |
|---|---|
| `OWNER` | Project bootstrap authority; cannot be revoked. |
| `ORCHESTRATOR` | Govern ordinary cross-agent coordination and policy. |
| `AGENT` | Perform scoped project work. |
| `OBSERVER` | Read without ordinary mutation authority. |

Scopes bound the resources an actor may claim or affect. A command must satisfy identity, role, scope, state transition, and any approval requirement.

## Credential resolution

Keys live in the platform credential store, not project history. `--actor` succeeds only with the matching credential. Profiles and host labels help select credentials for different projects and launch environments.

Runtimes are separate records. A runtime proves a process is present and eligible to consume work for its owning agent; it does not create or replace the agent identity.

## Elevated keys

A human principal may register a second passphrase-encrypted key. Argon2id and AES-256-GCM protect the local key material. The passphrase is never written to disk.

Elevated signing is required for orchestrator grants, human-tier approval, revoking another human or orchestrator, and deleting any revoked principal. Do not type the passphrase into an agent chat.

## Key history

Rotation preserves public-key history and boundaries. Newly committed events store the fingerprint of the exact verified actor key inside the event hash. If an ID is later reused after deletion, fingerprints keep both occupants distinguishable.
