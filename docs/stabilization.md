# Project stabilization

Agent Comms is in a stabilization phase. Work in this phase reduces ambiguity,
removes contradictory behavior, and strengthens existing contracts before new
capabilities are added.

## Rules

- An agent ID, runtime ID, profile name, and provider session ID are distinct
  identifiers. Commands and documentation must name which one they accept.
- A durable authoritative event is the only proof that a mutation succeeded.
  Notifications and terminal delivery remain secondary, observable side
  effects.
- Commands must be bounded. A busy or unavailable peer must not indefinitely
  hold the requesting agent.
- CLI, MCP, TUI, daemon, and documentation must use the same transition rules
  and vocabulary.
- Ambiguity fails visibly. The system must not silently choose an actor,
  runtime, project, or session when multiple valid choices exist.

## Current work

### Signing identity resolution

- Resolution precedence is explicit: `--actor`, `--profile`,
  `AGENT_COMMS_ACTOR`, unique project-and-host binding, same-project active
  profile, then project owner.
- An explicitly selected profile from another project is rejected.
- A host label bound to multiple actors is rejected instead of falling
  through to an unrelated active profile or owner.
- A new host with no binding resolves to the project owner only for the
  existing first-identity bootstrap flow.
- `agent-comms profile current --json` reports the resolved actor and source.
- The MCP `identity` tool reports the actor bound to that connection.

### Invocation routing and delivery

- Invocation `target` and CLI `--to` mean an agent ID.
- Registered runtime ownership is used when the runtime ID differs from the
  agent ID.
- A same-ID runtime remains the simple local fallback.
- Multiple live runtimes require an explicit `invocation redeliver --runtime`
  selection.
- Busy interactive sessions return a retryable warning quickly instead of
  holding the requester through the target's active turn.
- Socket request deadlines honor caller cancellation after connecting.

### Registration and role-escalation authorization

- Self-registration (`id` equal to the caller's own resolved actor) is
  always permitted, over both CLI and MCP.
- Registering a different `id` requires the caller to be an active
  orchestrator or human principal (`Service.CanSponsorRegistration`),
  enforced identically at the CLI's `agent register` and the MCP
  `agent_register` tool — the CLI previously had no check here at all.
- Granting the Orchestrator role via `agent activate`/`agent_activate`
  requires the granting actor to be an active HUMAN principal, on top of
  the ordinary owner-or-orchestrator elevation already required for any
  activation. An AGENT-principal orchestrator cannot mint further
  orchestrators on its own. Enforced once in the shared transition
  validator (`internal/protocol/transitions.go`), covering CLI, MCP, TUI,
  and the daemon across both authority backends.

### Interface error consistency

- CLI and MCP failures use one shared classifier for `VALIDATION`,
  `AUTHORIZATION`, `INTEGRITY`, `CONFLICT`, `STALE_PRECONDITION`,
  `RATE_LIMITED`, `OFFLINE`, and `UNAVAILABLE`.
- MCP JSON-RPC errors expose the stable Agent Comms code in `error.data.code`
  while retaining the protocol-level JSON-RPC error number.
- CLI exit statuses are derived from the same classification.

## Next stabilization areas

1. Extend stable error codes and action-precondition explanations through
   TUI and worker status surfaces.
2. Make delivery state and retry ownership explicit across interactive,
   connector, and MCP-only runtimes.
3. Audit every list/search/history surface for bounded results and consistent
   cursor semantics.
4. Reduce TUI action ambiguity by showing why an action is available, disabled,
   awaiting approval, or blocked by connectivity.
5. Add contract tests that run equivalent workflows through CLI and MCP and
   compare authoritative outcomes.
