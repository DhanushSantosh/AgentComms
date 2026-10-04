---
title: Run autonomous workers
description: Let Claude, Codex, or OpenCode claim and complete invocations without a human prompting the session to check.
section: Agent integration
order: 4
audience: Operators
lastVerified: 2026-10-01
related: [agents/invocations, agents/delivery]
---

`runtime worker` is a foreground supervisor loop. It listens for eligible invocations, claims one transactionally, launches the configured provider, publishes the result, and completes or waits the invocation with evidence.

## Default adapters

Use the direct adapters unless you need a specific alternative:

```sh
agent-comms --actor <agent-id> runtime worker \
  --id <runtime-id> \
  --adapter codex \
  --executable /usr/local/bin/codex \
  --codex-sandbox workspace-write \
  --execution-timeout 30m
```

```sh
agent-comms --actor <agent-id> runtime worker \
  --id <runtime-id> \
  --adapter claude \
  --executable /usr/local/bin/claude \
  --claude-permission-mode acceptEdits \
  --claude-max-budget-usd 1 \
  --execution-timeout 30m
```

OpenCode uses `--adapter opencode`. Its session continuity is stored in a local runtime cache because OpenCode mints non-UUID session IDs.

## Adapter choices

- `claude`, `codex`, `opencode`: proven direct CLI execution.
- `claude-live`, `codex-live`, `opencode-live`: persistent provider processes. The supported broker event viewer is `live attach --provider claude|codex --runtime <runtime-id>`; there is no OpenCode attach provider.

Codex live applies `--codex-sandbox` and `--codex-add-dir`, but uses the
provider's normal user configuration. `--codex-ignore-user-config` is rejected
before launch for `codex-live` and `codex-acp`; use `--adapter codex` for exec-based runs that
require user MCP/tool configuration isolation.
- `claude-acp`, `codex-acp`, `opencode-acp`: Agent Client Protocol integrations with provider-specific permission limits.

Claude and Codex can bind a valid existing conversation with `--session-id`. Provider rules differ: Claude can create a caller-chosen UUID; Codex normally resumes an ID it previously minted. Never process an interactive turn in the same conversation while its worker is active.

`live tail` is removed. The surviving `live serve` / `live attach` path
subscribes to a broker-managed runtime, not an arbitrary session log. It is
distinct from `runtime interactive-serve`, which wraps a provider's native
terminal UI for host-local delivery.

Codex and Claude managed live workers route by stored project ID plus runtime
ID. The same runtime ID can be used in independent projects sharing a broker.
Run `live attach` in that project's root, use `--project <root>`, or copy the
worker's printed `--project-id <stored-id>` command when viewing elsewhere.
A missing scoped runtime errors; it never falls back to a host-wide runtime.
For legacy/manual raw broker registrations, use `live attach --unscoped`.
This flag is mutually exclusive with `--project-id`. Outside a project, with
neither flag, attachment retains legacy literal-ID behavior.

During upgrade, stop old live workers and recycle your owned brokers before
starting project-scoped workers. Old bare-ID processes are not silently
adopted. Logical runtime IDs and existing per-project conversation caches
remain unchanged. Namespace keys are not authentication or secrets; the
broker's local-host trust assumptions still apply.

## Follow-up invocations

Provider shell or MCP access is not required for one agent to request another. A worker prompt accepts one bounded `AGENT_COMMS_INVOKE: {json}` action. The worker validates and signs the follow-up, submits it, and records the new invocation ID. Only one follow-up is accepted per completed turn to bound fan-out.

## Supervise the process

Workers remain foreground processes. Use systemd, launchd, a container runtime, or your existing supervisor for restart and shutdown policy. `--once` processes at most one receive attempt and is intended for tests or bounded automation—not continuous autonomy.

Permission-bypassing provider modes are rejected. Output, execution time, listen intervals, and budgets remain bounded.
