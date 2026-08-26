---
title: Routing and delivery evidence
description: Configure consumer policy, distinguish wake-up from acknowledgement, and redeliver safely.
section: Agent integration
order: 7
audience: Operators
lastVerified: 2026-08-01
related: [agents/interactive, security/integrity]
---

Consumer mode controls which kind of runtime may receive and claim an invocation.

## Consumer modes

- `INTERACTIVE_ONLY`: only an online local interactive runtime may receive or claim.
- `WORKER_ONLY`: PTY delivery is skipped and only worker runtimes may claim.
- `EITHER`: either kind may claim. This compatibility mode permits a race by design.

An explicit preferred runtime narrows both delivery and claim eligibility. Multiple eligible interactive runtimes without a preferred runtime are ambiguous; the coordinator never guesses.

## Set target policy

```sh
agent-comms invocation policy set \
  --agent <agent-id> \
  --mode TRUSTED \
  --trusted-actor PRICE \
  --default-consumer INTERACTIVE_ONLY \
  --allow-consumer INTERACTIVE_ONLY \
  --interactive-runtime <agent-id> \
  --require-human-for-sensitive
```

Policy modes are `MANUAL`, `TRUSTED`, `AUTOMATIC`, and `DISABLED`. Scope and sensitive-work rules apply in addition to consumer routing.

## Evidence model

Delivery reserves a bounded attempt before external I/O. A successful PTY attempt may record:

- `CONNECTOR_ACCEPTED`;
- `PTY_TEXT_ECHOED`;
- `PTY_ENTER_SENT`;
- transport and opaque endpoint identifiers.

These records prove transport actions only. `CLAIMED` proves that an eligible target accepted the obligation.

`MANUAL`, `MCP`, and `QUEUE` do not create notification-success events without a transport action. Local process success requires exit zero; webhook success requires an accepted HTTP response.

## Explicit redelivery

```sh
agent-comms invocation redeliver --id <invocation> --runtime <agent-id>
```

Redelivery is allowed for open, unclaimed `PENDING` or `NOTIFIED` invocations. A failed later attempt does not erase an earlier successful delivery. Automatic attempt leases prevent duplicate active deliveries and allow abandoned attempts to recover after expiry.
