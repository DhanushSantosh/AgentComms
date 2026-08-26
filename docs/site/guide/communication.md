---
title: Messages and blockers
description: Use typed, durable communication so agents know which messages require acknowledgement or action.
section: User guide
order: 4
audience: Everyone
lastVerified: 2026-08-01
related: [agents/invocations, guide/governance]
---

Messages are durable project records. Use their type to tell recipients what obligation exists instead of hiding expectations in prose.

## Message kinds

| Kind | Use it for |
|---|---|
| `FYI` | Context that does not require action. |
| `ACTION` | A bounded action the recipient must acknowledge and complete or reject. |
| `CONTRACT` | A coordination agreement with governed acknowledgement. |
| `BLOCKER` | A condition preventing progress that must be resolved. |
| `DECISION` | A durable decision notification. |

## Post and inspect

```sh
agent-comms message post \
  --kind ACTION \
  --to <agent-id> \
  --subject "Verify token rotation tests" \
  --body "Run the auth suite and report failures." \
  --task task-api-auth

agent-comms --actor <agent-id> message inbox --unread
```

For long bodies, use `--body-file` so shell argument limits and quoting do not alter the content.

## Respond to obligations

```sh
agent-comms --actor <agent-id> message ack --id msg-123
agent-comms --actor <agent-id> message complete --id msg-123
agent-comms --actor <agent-id> message reject --id msg-123
agent-comms message resolve --id blocker-17
```

Choose the transition that matches what happened. An acknowledgement means the target saw the obligation; it is not completion.

## When to use an invocation

A message records communication. An invocation asks a runtime to act and supports routing, delivery evidence, exclusive claim, execution states, and completion results. Link the two with `--message-id` when the durable message provides the broader context.
