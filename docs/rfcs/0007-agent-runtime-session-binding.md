# RFC 0007: Agent Runtime Session Binding

## Status

Accepted

## Context

An autonomous runtime worker can complete an invocation without human
intervention, but an ephemeral agent process has no conversational continuity.
Every invocation starts without the agent's prior working context, so a series
of durable messages does not become an ongoing collaboration.

Selecting the most recent local conversation is unsafe because multiple agent
identities and projects can share one host. Agent conversation identifiers are
operational routing metadata rather than project authority or credentials.

## Decision

Allow `runtime worker` to bind explicitly to an existing Claude or Codex
conversation UUID with `--session-id`.

For Claude, the worker uses `--resume <session-id>` and enables normal local
session persistence. For Codex, it uses
`codex exec ... resume <session-id>`. The worker never uses an implicit
"continue most recent" selector.

The binding is supplied by the local process supervisor and is not written to
the signed project event chain. The registered runtime ID remains the durable
project identity; the session ID only selects which local agent conversation
executes work. Existing workers without a binding retain isolated one-shot
behavior for backward compatibility.

Claude workers may opt into `--claude-allow-agent-comms`. This adds one
provider allow rule scoped to the absolute path of the currently running
Agent Comms executable, enabling the agent to create follow-up invocations
without granting general unattended shell access. Permission bypass remains
prohibited.

Codex workers may declare bounded additional writable directories with
`--codex-add-dir`, primarily for Agent Comms' per-user runtime state. Paths
must be existing absolute directories; the rest of the configured Codex
sandbox remains enforced.

Codex workers may ignore user configuration while resuming a bound session.
This isolates autonomous execution from unrelated MCP authentication failures
without making the session ephemeral or bypassing the sandbox.

Provider tools are not a reliable control-plane dependency: resumed sessions
may lack the Agent Comms MCP server, and nested CLI calls may not have keyring
or daemon access inside a sandbox. The worker therefore accepts one bounded
`AGENT_COMMS_INVOKE: {json}` action in an agent result. It validates and submits
that action through the worker's authenticated service, then records the
created invocation ID in the durable response. Unknown fields, malformed
actions, multiple actions, and out-of-range expiries fail the current
invocation into `WAITING`.

Only one worker may own a bound session at a time. Operators must not bind a
worker to a conversation that is simultaneously processing another turn.
Agent Comms continues to serialize invocations per worker and records the
request, response, and lifecycle independently of the provider's session
storage.

## Consequences

Successive invocations to an agent can resume the same model conversation and
retain its accumulated working context. Two agents with separately bound
runtimes can exchange durable messages and follow-up invocations without a
human relaying them.

Provider session contents remain in the provider's local session store and are
subject to its retention behavior. The Agent Comms audit log remains portable
and contains outcomes rather than private model reasoning. Moving a runtime to
another host requires moving or replacing its provider session binding.
