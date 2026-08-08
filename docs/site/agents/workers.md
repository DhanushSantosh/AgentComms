---
title: Run autonomous workers
description: Let Claude, Codex, or OpenCode claim and complete invocations without a human prompting the session to check.
section: Agent integration
order: 4
audience: Operators
lastVerified: 2026-08-01
related: [agents/invocations, agents/delivery]
---

`runtime worker` is a foreground supervisor loop. It listens for eligible invocations, claims one transactionally, launches the configured provider, publishes the result, and completes or waits the invocation with evidence.

## Default adapters

Use the direct adapters unless you need a specific alternative:

```sh
agent-comms --actor DAMON runtime worker \
  --id damon-runtime \
  --adapter codex \
  --executable /usr/local/bin/codex \
  --codex-sandbox workspace-write \
  --execution-timeout 30m
```

```sh
agent-comms --actor AXIOM runtime worker \
  --id axiom-runtime \
  --adapter claude \
  --executable /usr/local/bin/claude \
  --claude-permission-mode acceptEdits \
  --claude-max-budget-usd 1 \
  --execution-timeout 30m
```

OpenCode uses `--adapter opencode`. Its session continuity is stored in a local runtime cache because OpenCode mints non-UUID session IDs.

## Adapter choices

- `claude`, `codex`, `opencode`: proven direct CLI execution.
- `claude-live`, `codex-live`, `opencode-live`: persistent processes with live viewer support.
- `claude-acp`, `codex-acp`, `opencode-acp`: Agent Client Protocol integrations with provider-specific permission limits.

Claude and Codex can bind a valid existing conversation with `--session-id`. Provider rules differ: Claude can create a caller-chosen UUID; Codex normally resumes an ID it previously minted. Never process an interactive turn in the same conversation while its worker is active.

## Follow-up invocations

Provider shell or MCP access is not required for one agent to request another. A worker prompt accepts one bounded `AGENT_COMMS_INVOKE: {json}` action. The worker validates and signs the follow-up, submits it, and records the new invocation ID. Only one follow-up is accepted per completed turn to bound fan-out.

## Supervise the process

Workers remain foreground processes. Use systemd, launchd, a container runtime, or your existing supervisor for restart and shutdown policy. `--once` processes at most one receive attempt and is intended for tests or bounded automation—not continuous autonomy.

Permission-bypassing provider modes are rejected. Output, execution time, listen intervals, and budgets remain bounded.
