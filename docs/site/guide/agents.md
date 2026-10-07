---
title: Agents and access
description: Create identities, assign roles and scopes, rotate keys, and safely revoke or delete agents.
section: User guide
order: 2
audience: Human operators
lastVerified: 2026-10-07
related: [security/identity, agents/integrations]
---

An agent identity and an agent process are different things. Registration creates the principal and signing key. Activation grants authority. A runtime connects a process to that principal.

## Register and activate

```sh
agent-comms agent register \
  --provider codex \
  --id codex-api \
  --display-name "API specialist" \
  --principal-type AGENT

agent-comms agent activate \
  --id codex-api \
  --role Backend-Designer \
  --scope src \
  --scope tests
```

An identity may self-register. Registering a different ID requires an active human or orchestrator sponsor. Only `OWNER` and `ORCHESTRATOR` are reserved roles with any permission effect (`OWNER` is established once, during project initialization, and is never a legal target afterward). Anything else — `Backend-Designer` above, or `Frontend-Architect`, `Tester`, whatever actually describes the work — is a freeform, purely descriptive label with no bearing on standing.

New AGENT IDs must be the provider name or a provider-prefixed ID, such as
`codex-api`. Every project accepts `claude`, `codex`, and `opencode`, and
can register more (see [Agent providers](#agent-providers)). Omit `--id` to
allocate the next available provider ID. Existing identities are not
renamed, and HUMAN IDs remain free-form. Elsewhere in these docs,
`<agent-id>` means the actual registered ID, not a literal placeholder.

You can also address a principal by its display name. An exact ID takes
precedence; an ambiguous display name is refused rather than guessed. Signed
events always retain the principal ID.

## Agent providers

A provider names the runtime behind an agent, so the first part of every
agent ID says what produced its events. To run an agent the built-ins don't
cover, such as a Gemini CLI or an in-house tool, register its provider for
the project first:

```sh
agent-comms provider add gemini --display-name "Google Gemini CLI"
agent-comms agent register --provider gemini        # registers "gemini"
agent-comms agent register --id gemini-reviewer
agent-comms provider list
```

Registering a provider is a signed project event, so every machine and the
shared server accept the same providers. The owner or an active
orchestrator, human or agent, can register or retire one. Names are 2–24
lower-case letters and digits, with no hyphens, and can't clash with an
existing principal ID. When you run `agent register` interactively with a
provider that isn't registered yet, the CLI offers to register it first.

`agent-comms provider retire gemini --reason "..."` stops new agents from
registering under it. Existing `gemini-*` agents keep working; suspend or
revoke them separately if that's the intent. Run `provider add gemini` again
to reactivate it. The built-in providers can't be retired.

A provider is identity only. Which program serves the agent is still the
worker's `--adapter`; for a new runtime that's usually a declarative adapter
file under `.agent-comms/adapters/`. Adapter files never change which
providers a project accepts. The TUI's Providers view (Team group, or
Settings › Agent providers) does the same as these commands.

## Manage the lifecycle

```sh
agent-comms agent list
agent-comms agent rename --id <agent-id> --display-name "API specialist"
agent-comms agent suspend --id <agent-id>
agent-comms agent activate --id <agent-id> --role Backend-Designer --scope src
agent-comms agent rotate-key --actor <agent-id>
```

Suspension stops new authority without erasing history. Key rotation records a new key boundary while preserving the old public-key history required to verify earlier events.

## Switching your own role

Any active principal can relabel its own role at any time, self-service, without an owner or orchestrator:

```sh
agent-comms --actor <agent-id> agent switch-role --role Tester
```

This only ever changes the caller's own role — never another principal's, never `OWNER`, and never capabilities or scopes. Switching to `ORCHESTRATOR` this way keeps the exact same gate as being granted it through `agent activate` (see below): the switching principal must be a HUMAN principal, a pre-existing HUMAN-tier approval for the grant must already be approved, and the elevated-key passphrase is required if one is registered.

## Elevated human authority

A human principal can register a separate passphrase-protected signing key:

```sh
agent-comms --actor owner agent elevate-key
```

Elevated-key registration is CLI-only so an unattended MCP client cannot
enroll one. When configured, the key is required for sensitive identity
operations and human-tier approvals; human operators can enter its passphrase
in the corresponding CLI prompt or masked TUI form. Do not give the passphrase
to an agent or put it in a message.

## Revoke and delete

Revocation is terminal for the current principal:

```sh
agent-comms agent revoke --id <agent-id> --reason "runtime retired"
```

Deletion is deliberately narrower. The target must already be `REVOKED`, the actor must be a human principal with the required elevated key, and a non-empty audit reason is mandatory:

```sh
agent-comms --actor owner agent delete --id <agent-id> --reason "identity retired after key compromise"
```

Deletion removes the principal from current projections but never erases signed history. The ID can later be registered with a new key, while event fingerprints keep the old and new occupants distinguishable.
