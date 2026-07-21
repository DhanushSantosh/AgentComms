# RFC 0006: Governed Autonomous Runtime Worker

## Status

Accepted

## Context

Connected runtime listeners can receive and claim invocations without an
operator, but a listener has no reasoning capability. A shell loop can maintain
presence and advance lifecycle state, yet it cannot safely turn an invocation
instruction into Claude or Codex work and a durable response.

Launching an agent from an authorized invocation introduces cost, process,
prompt-injection, workspace, and failure-management boundaries. Those controls
must be explicit rather than embedded in an ad hoc background script.

## Decision

Add `agent-comms runtime worker`, a foreground supervised worker for one
registered runtime and actor identity. The worker:

1. maintains bounded runtime heartbeats and listens for eligible work;
2. claims and starts one invocation through authoritative signed events;
3. executes a configured Claude or Codex binary directly without a shell;
4. supplies a bounded governance preamble and invocation instruction on
   standard input;
5. caps execution time, captured output, and Claude spend;
6. posts the bounded result to the requester as a durable message; and
7. completes the invocation with that result message as evidence.

Executables and working directories must be absolute. Codex defaults to
workspace-write sandboxing with approvals disabled inside that sandbox. Claude
defaults to `acceptEdits`, never enables permission bypass, and requires a
positive per-invocation budget cap. Model selection is optional and explicit.

If execution, result publication, or completion fails after work starts, the
worker moves the invocation to `WAITING` with a bounded diagnostic reason.
It never records a failed agent process as successful work. Worker concurrency
is one invocation per process; deployments scale through separately registered
runtimes and existing authoritative capacity controls.

## Consequences

A connected Claude or Codex identity can receive, execute, answer, and complete
an invocation without a user prompting the interactive agent to check.
Lifecycle state and the final response remain auditable.

The worker deliberately runs in the foreground so process supervision remains
the responsibility of systemd, launchd, a container runtime, or the invoking
agent host. Agent Comms does not silently install persistent background
services. Authorized instructions can still cause an agent to modify files
within its configured permissions, so users must scope agent identities,
invocation policies, workspaces, models, budgets, and runtime capacity
appropriately.
