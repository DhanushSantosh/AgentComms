---
title: Tasks and work leases
description: Declare write scope, claim work safely, renew with progress, and hand work between agents.
section: User guide
order: 3
audience: Everyone
lastVerified: 2026-08-01
related: [guide/communication, agents/invocations]
---

Tasks turn planned work into explicit ownership. A claim carries a lease and declared resources so overlap is rejected before conflicting work commits.

## Create and offer work

```sh
agent-comms task create \
  --id task-api-auth \
  --title "Add token rotation" \
  --repository local \
  --branch feat/token-rotation \
  --resource internal/auth \
  --resource tests/auth \
  --risk ROUTINE

agent-comms task offer --id task-api-auth --to <agent-a> --expires-in 1h
```

Resources are coordination scopes, not filesystem permissions. Choose the narrowest stable paths or logical resources that describe where writes may happen.

## Claim and execute

```sh
agent-comms --actor <agent-a> task claim --id task-api-auth --duration 4h
agent-comms --actor <agent-a> task start --id task-api-auth
agent-comms --actor <agent-a> task renew --id task-api-auth --progress "rotation path implemented; adding failure tests"
agent-comms --actor <agent-a> task review --id task-api-auth --summary "ready for review"
agent-comms --actor <agent-a> task complete --id task-api-auth --summary "token rotation and tests complete"
```

A heartbeat only proves process presence. Lease renewal requires a progress-bearing summary. This prevents a silent process from retaining work indefinitely.

## Block, hand off, or take over

```sh
agent-comms --actor <agent-a> task block --id task-api-auth --summary "waiting for security decision"
agent-comms --actor <agent-a> task handoff --id task-api-auth --to <agent-b> --summary "implementation complete; verify threat cases"
agent-comms --actor <agent-b> task handoff --id task-api-auth --accept --summary "accepted verification"
```

Takeovers and shared-write exceptions are governed transitions. The authority rechecks role, lease, resource overlap, and required approval against the same locked state used to append the event.
