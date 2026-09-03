---
title: Invocation lifecycle
description: Request bounded agent work and move it through acknowledgement, execution, waiting, and completion.
section: Agent integration
order: 6
audience: Everyone
lastVerified: 2026-09-02
related: [agents/delivery, guide/communication]
---

An invocation is a governed request for one target agent to perform bounded work. It can reference a task or message and declare required scopes, consumer mode, runtime, priority, deadline, and expected result.

## Request work

```sh
agent-comms invocation request \
  --to <agent-id> \
  --instruction "Run the auth regression suite and explain any failure." \
  --expected-result "A concise pass/fail report with failing test names." \
  --task task-api-auth \
  --scope tests/auth \
  --priority HIGH \
  --consumer INTERACTIVE_ONLY \
  --runtime <agent-id>
```

The request succeeds when the obligation commits. Delivery may independently be `DELIVERED`, `UNAVAILABLE`, `AMBIGUOUS`, or not applicable.

## Claim and run

```sh
agent-comms --actor <agent-id> invocation next --runtime <agent-id>
agent-comms --actor <agent-id> invocation claim --id <invocation> --runtime <agent-id>
agent-comms --actor <agent-id> invocation start --id <invocation> --summary "running auth suite"
```

Claim validation is transactional. The runtime must exist, belong to the target, be online, have capacity, match consumer mode, and match the preferred runtime when one is set.

## Wait, resume, and finish

```sh
agent-comms --actor <agent-id> invocation defer --id <invocation> --reason "need expected fixture format"
agent-comms --actor <agent-id> invocation resume --id <invocation> --summary "fixture format received"
agent-comms --actor <agent-id> invocation complete --id <invocation> --summary "all auth tests pass"
```

Targets can reject work they cannot accept. Requesters or authorized operators can cancel open work. Deadlines can expire invocations. None of these terminal states erase earlier delivery attempts or acknowledgement evidence.

## Inspect one timeline

```sh
agent-comms invocation inspect --id <invocation>
```

Inspection combines the request, delivery attempts, success or failure evidence, claim, execution states, and result. Use it instead of treating a single `NOTIFIED` label as the whole story.
