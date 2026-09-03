---
title: Approvals and decisions
description: Record durable decisions and require the right level of authority before sensitive actions proceed.
section: User guide
order: 5
audience: Human operators
lastVerified: 2026-09-02
related: [security/identity, security/integrity]
---

Decisions explain what the project chose. Approvals authorize a proposed action. Neither is a chat reaction.

## Record a decision

A decision is a governed document tagged `decision`. `--notify` posts a
`DECISION` message to each principal expected to acknowledge it.

```sh
agent-comms document create \
  --id decision-auth-format \
  --decision \
  --title "Use rotating refresh tokens" \
  --body "Access tokens remain short-lived; refresh tokens rotate on use." \
  --notify <agent-a> \
  --notify <agent-b>
```

When a decision changes, publish a new one and supersede the old
document rather than editing history:

```sh
agent-comms document create \
  --id decision-auth-format-v2 \
  --decision \
  --title "Bind refresh tokens to device keys" \
  --body "Refresh rotation also verifies the registered device key."
agent-comms document supersede \
  --id decision-auth-format \
  --replacement decision-auth-format-v2
```

## Request approval

```sh
agent-comms approval request \
  --id approval-shared-auth \
  --tier ORCHESTRATOR \
  --action "share-write:internal/auth" \
  --reason "<agent-a> implements while <agent-b> verifies" \
  --affected <agent-a> \
  --affected <agent-b>
```

Tiers are:

- `ORCHESTRATOR` for ordinary governed coordination exceptions.
- `HUMAN` for sensitive actions that must not be authorized by an unattended agent.

Approve or reject explicitly:

```sh
agent-comms approval approve --id approval-shared-auth
agent-comms approval reject --id approval-shared-auth
```

The transition that consumes approval rechecks the approval, actor authority, and current target state. Creating an approval record does not make an otherwise invalid action succeed.

> The current protocol does not require the approver to differ from the requester. If your governance requires two distinct people, enforce that operationally until multi-party approval is added.
