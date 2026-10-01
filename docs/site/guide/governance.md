---
title: Approvals and decisions
description: Record durable decisions and require the right level of authority before sensitive actions proceed.
section: User guide
order: 5
audience: Human operators
lastVerified: 2026-10-01
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
  --action "shared-write:task-api-auth:task-auth-tests" \
  --expires-in 1h \
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

Shared-write approval authorizes overlap between the two named tasks, not
unrestricted writes to a path. Both tasks must exist. The arrangement remains
reusable for that task pair until any stated expiry; task takeover instead
consumes one eligible approval per takeover (`task.takeover:<task-id>`).

Contract publication and approval-gated invocation requests bind approval to
the canonical operation payload and require a future expiry. Use the command's
approval-required guidance to obtain the exact subject JSON and digest; an
action string alone cannot authorize these operations. Review that payload
and expiry with `approval show --id <approval-id>` before approving.

The authorized transition rechecks the approval, actor authority, and current
target state. Creating an approval record does not make an otherwise invalid
action succeed. An expired approval cannot authorize a new transition, even
if its stored status still reads APPROVED; expiry does not revoke a lease
already granted.

> The current protocol does not require the approver to differ from the requester. If your governance requires two distinct people, enforce that operationally until multi-party approval is added.
