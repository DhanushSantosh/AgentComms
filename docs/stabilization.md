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
- Interactive delivery requires a first-class registered `INTERACTIVE`
  runtime; matching an agent ID is only a naming convenience, never a fallback
  registration.
- Multiple eligible live runtimes require a preferred runtime. Explicit
  redelivery always names the runtime.
- Busy interactive sessions return a retryable warning quickly instead of
  holding the requester through the target's active turn.
- Socket request deadlines honor caller cancellation after connecting.
- A successful transport, a target claim, and completion are separate durable
  facts with separate evidence.

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
- The principal-type check above is satisfiable by an unregistered agent
  operating over the ambient owner-fallback identity, so it alone doesn't
  guarantee a human decided the grant in the moment. Granting the
  Orchestrator role additionally requires a pre-existing, separately
  approved, HUMAN-tier approval record for that exact grant
  (`approval.action` == `agent.activate:<id>`) — a real two-step
  apply-then-approve control, enforced in the same shared validator, that
  a single self-contained command cannot satisfy unattended.
- Both controls above are still credential-based, which has its own limit:
  a locally-running agent with OS-level access to the same credential store
  can complete every step itself, in the human's name — proven live. A
  passphrase-encrypted "elevated" Ed25519 keypair per HUMAN principal
  (`agent-comms agent elevate-key`, `internal/identity`) closes that gap for
  the transitions classified by `protocol.RequiresElevatedKey`: granting
  ORCHESTRATOR, approving a HUMAN-tier approval, revoking another
  ORCHESTRATOR or HUMAN principal, and deleting any revoked principal.
  Both authority backends enforce the same classification. Decrypting it requires
  an interactive terminal passphrase prompt that refuses outright — not
  hangs or silently reads garbage — when stdin isn't a real TTY. **Registering**
  the key (`agent elevate-key`) is CLI-only by design, never exposed as an
  MCP tool or a TUI form, since it would otherwise race another consumer of
  the same stdin fd (bubbletea's own raw-mode reader, or a pty an MCP host
  might allocate). **Using** an already-registered key for `agent activate`
  (ORCHESTRATOR), `approval approve` (HUMAN-tier), `agent.revoke`, or
  `agent.delete` is different: the TUI later gained a masked
  "Elevated-key passphrase" form field for each of these that completes the
  signed transition directly, without needing a real raw-terminal prompt —
  MCP alone still refuses outright, since no MCP tool takes a passphrase
  parameter at all. See docs/governance.md for the current, authoritative
  description of this split.
- `agent.revoke` of an ORCHESTRATOR or HUMAN principal, once an elevated key
  is registered for the actor, requires that same elevated-key signature —
  symmetric with the grant side, closing the identical credential-only gap
  on the revoke path.
- `agent.delete` is a separate HUMAN-only, elevated-key-gated transition
  after revocation. It releases the ID for reuse without deleting history;
  each new event now hash-attests the exact verified actor-key fingerprint
  so occupants on either side of reuse remain distinguishable.
- `agent.suspend` now blocks targeting the OWNER outright, and requires a
  HUMAN principal (ordinary credential, not the elevated key) to suspend an
  ORCHESTRATOR or HUMAN principal — mirroring `agent.revoke`'s existing
  protection, which `agent.suspend` had lacked. Unprotected, a suspended
  owner has no path back except trusting another principal to reactivate
  them, since a suspended principal fails every subsequent action including
  reactivating itself.
- `agent.rotate-key` targeting a principal other than the caller is rejected
  outright, for every actor including the owner — no shipped interface has
  ever used this, and unrestricted it would let an elevated actor replace
  another principal's public key with one it controls, a full identity
  takeover with no consent check.
- `project.settings.update` requires a HUMAN principal (not the elevated
  key), closing a path where an AGENT-principal orchestrator could
  unilaterally disable `RequireReview` project-wide.
- `env.set`/`env.delete` now require ordinary owner-or-orchestrator
  elevation; previously any active principal, including an OBSERVER-role
  one, could write or delete arbitrary key/value data in the shared,
  append-only signed log.

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
2. Exercise delivery coordinator recovery and cache lag under sustained
   multi-runtime load.
3. Audit every list/search/history surface for bounded results and consistent
   cursor semantics.
4. Reduce TUI action ambiguity by showing why an action is available, disabled,
   awaiting approval, or blocked by connectivity.
5. Add contract tests that run equivalent workflows through CLI and MCP and
   compare authoritative outcomes.
