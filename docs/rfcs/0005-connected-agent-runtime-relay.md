# RFC 0005: Connected Agent Runtime Relay

## Status

Accepted

## Context

The invocation event model already provided authorization, target policies,
exclusive claims, runtime capacity, retries, and lifecycle transitions.
However, MCP and CLI runtimes read current state directly and could only poll
for work. MCP notifications did not wake an agent host, while webhook runtimes
were accepted by the event model but skipped by the daemon dispatcher.

The missing delivery boundary forced a human operator to notice an invocation
and manually alert the target agent.

## Decision

Add a bounded connected-runtime listen contract to the local daemon. A runtime
may wait for up to ten seconds for its next eligible invocation. CLI and MCP
adapters expose this as `invocation listen` and `invocation_listen`. Both can
claim the returned invocation transactionally, and auto-claim is the MCP
default. Agent hosts maintain connectivity by repeating bounded listens and
runtime heartbeats.

Limit each daemon to 128 concurrent runtime listeners. Existing runtime
capacity, target identity, deadline, policy, scope, and claim checks remain
authoritative.

Enable webhook connector delivery from the daemon. Webhooks receive the same
bounded invocation envelope as local processes. Remote endpoints require
HTTPS; loopback HTTP remains available for local adapters. Redirects are
rejected, response bodies and private configuration are bounded, and connector
headers remain outside project history.

Increment the local daemon protocol version so a new CLI replaces any daemon
that predates the listen endpoint contract.

## Consequences

Agent hosts can remain connected without rapid polling and can acknowledge work
through the existing signed claim event. External orchestrators can wake an
agent through HTTPS webhook delivery, while local-process and manual connectors
remain compatible.

MCP transports cannot force a model host to schedule a turn. The host must keep
an invocation listen active or expose a webhook adapter that schedules the
target agent. Queue connectors remain unsupported until a broker-neutral
delivery and acknowledgement contract is defined.
