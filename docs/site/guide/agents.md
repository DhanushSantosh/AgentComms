---
title: Agents and access
description: Create identities, assign roles and scopes, rotate keys, and safely revoke or delete agents.
section: User guide
order: 2
audience: Human operators
lastVerified: 2026-08-01
related: [security/identity, agents/integrations]
---

An agent identity and an agent process are different things. Registration creates the principal and signing key. Activation grants authority. A runtime connects a process to that principal.

## Register and activate

```sh
agent-comms agent register \
  --id DAMON \
  --display-name "DAMON" \
  --principal-type AGENT

agent-comms agent activate \
  --id DAMON \
  --role AGENT \
  --scope src \
  --scope tests
```

An identity may self-register. Registering a different ID requires an active human or orchestrator sponsor. Valid roles are `OWNER`, `ORCHESTRATOR`, `AGENT`, and `OBSERVER`; the owner role is established during project initialization.

## Manage the lifecycle

```sh
agent-comms agent list
agent-comms agent rename --id DAMON --display-name "Damon / API"
agent-comms agent suspend --id DAMON
agent-comms agent activate --id DAMON --role AGENT --scope src
agent-comms agent rotate-key --actor DAMON
```

Suspension stops new authority without erasing history. Key rotation records a new key boundary while preserving the old public-key history required to verify earlier events.

## Elevated human authority

A human principal can register a separate passphrase-protected signing key:

```sh
agent-comms --actor owner agent elevate-key
```

The elevated key is required for sensitive identity operations and human-tier approvals when configured. It is CLI-only so an unattended MCP client cannot answer the passphrase prompt.

## Revoke and delete

Revocation is terminal for the current principal:

```sh
agent-comms agent revoke --id DAMON --reason "runtime retired"
```

Deletion is deliberately narrower. The target must already be `REVOKED`, the actor must be a human principal with the required elevated key, and a non-empty audit reason is mandatory:

```sh
agent-comms --actor owner agent delete --id DAMON --reason "identity retired after key compromise"
```

Deletion removes the principal from current projections but never erases signed history. The ID can later be registered with a new key, while event fingerprints keep the old and new occupants distinguishable.
