---
title: Approvals and decisions
description: Record durable decisions and require the right level of authority before sensitive actions proceed.
section: User guide
order: 5
audience: Human operators
lastVerified: 2026-08-01
related: [security/identity, security/integrity]
---

Decisions explain what the project chose. Approvals authorize a proposed action. Neither is a chat reaction.

## Record a decision

```sh
agent-comms decision create \
  --id decision-auth-format \
  --title "Use rotating refresh tokens" \
  --statement "Access tokens remain short-lived; refresh tokens rotate on use." \
  --to DAMON \
  --to AXIOM
```

When a decision changes, supersede it rather than editing history:

```sh
agent-comms decision supersede \
  --id decision-auth-format-v2 \
  --title "Bind refresh tokens to device keys" \
  --statement "Refresh rotation also verifies the registered device key." \
  --supersedes decision-auth-format
```

## Request approval

```sh
agent-comms approval request \
  --id approval-shared-auth \
  --tier ORCHESTRATOR \
  --action "share-write:internal/auth" \
  --reason "DAMON implements while AXIOM verifies" \
  --affected DAMON \
  --affected AXIOM
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
