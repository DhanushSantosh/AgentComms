# RFC 0013 — Truthful Interactive Delivery and Consumer Isolation

Status: Implemented

## Problem

An invocation is a durable governed obligation. Delivery is only a best-effort
wake-up mechanism for that obligation. The previous implementation blurred
those facts: `invocation.notify` could be committed before a connector did
anything, `MANUAL` and `MCP` could produce a false `NOTIFIED`, and terminal
injection left no durable evidence. A worker and a live terminal could also
race for work even when the requester intended one specific consumer.

This RFC separates four facts that must never be presented as equivalent:

1. `invocation.request` proves that the obligation was committed.
2. Delivery evidence proves that a transport performed bounded wake-up actions.
3. `invocation.claim` is the target's first authoritative acknowledgement.
4. `invocation.complete` proves that the target closed the obligation.

A request remains successful when delivery is unavailable. The response reports
the committed request and the delivery outcome separately.

## Runtime and routing model

Every runtime has a `kind`:

- `WORKER` is a headless or pull-based consumer.
- `INTERACTIVE` is a supervised local terminal session.

`connector` remains the transport. `INTERACTIVE` runtimes use the
`INTERACTIVE` connector. Existing runtime events without a kind replay as
`WORKER`.

Every invocation has a normalized consumer mode:

- `INTERACTIVE_ONLY` permits only an eligible interactive runtime.
- `WORKER_ONLY` permits only an eligible worker runtime and never attempts PTY
  delivery.
- `EITHER` permits either kind. This is the compatibility default and
  intentionally remains race-permitted.

An optional preferred runtime narrows both delivery and claim eligibility. If
more than one local interactive runtime is eligible and no preferred runtime is
set, automatic interactive delivery is ambiguous and the coordinator does not
guess.

Invocation policies define a default consumer mode, the allowed modes, and an
optional preferred interactive runtime. Projects upgraded from 2.0 retain
`EITHER` until an operator tightens their policies.

## Delivery state machine

The daemon is the only delivery coordinator. CLI, TUI, MCP, cache
synchronization, and follow-up requests all enter the same path:

1. Commit `invocation.request`.
2. Resolve one policy-compatible runtime.
3. Commit `invocation.delivery-attempt`.
4. Execute the external transport.
5. Commit `invocation.notify` with evidence, or
   `invocation.delivery-failed`.

`invocation.delivery-attempt` reserves a delivery ID, runtime, transport,
ordinal, origin, host, and bounded lease. Only one unexpired automatic attempt
for an invocation/runtime pair is valid. An expired attempt is closed as failed
before another attempt is reserved.

For new history, `invocation.notify` means transport success. It must reference
an active attempt and include monotonic evidence within that attempt's time
window:

- `CONNECTOR_ACCEPTED` after a local process exits zero or a webhook returns
  2xx;
- `PTY_TEXT_ECHOED` and `PTY_ENTER_SENT` for interactive delivery.

The endpoint and host values are opaque routing evidence, not semantic proof.
Historical 2.0 `invocation.notify` events still replay as legacy notifications;
new unreserved notifications are rejected.

`invocation.delivery-failed` closes only the selected attempt. It never
terminates the invocation and never erases an earlier successful delivery.
Automatic attempts are bounded; an operator can explicitly redeliver an open,
unclaimed invocation to a named runtime.

`MANUAL`, `MCP`, and `QUEUE` do not perform push delivery and cannot create a
notification-success event. MCP runtimes still receive work through bounded
listen/claim operations.

## Claims and runtime lifecycle

Claims are validated transactionally against current projection state. The
runtime must:

- exist and belong to the invocation target;
- be online and healthy;
- have authoritative capacity;
- match the invocation's consumer mode;
- match its preferred runtime when one is set.

`runtime.configure` repairs an `OFFLINE` or `DRAINING` runtime. It cannot change
the owner and is rejected while either runtime presence or authoritative
invocation assignments show active work. `runtime.offline` records a clean
process exit; heartbeat expiry remains the crash fallback.

`runtime interactive-serve` is the supervised lifecycle for an interactive
runtime. It performs normal project and identity bootstrap, creates a missing
runtime, validates an existing runtime's owner/kind/connector/host, heartbeats
with its current endpoint, and records `runtime.offline` while removing the
socket on clean exit.

Every process for the same OS user derives interactive socket paths below an
owner-only, UID-scoped shared Unix directory. On Linux this is
`/tmp/agent-comms-<uid>/interactive`; the literal system path intentionally
does not consult `TMPDIR`, so a desktop-launched provider and the daemon cannot
silently choose different control sockets. Runtime IDs that are not safe
filename components are hashed before use.

Each installation has one random 128-bit host ID in the user configuration
directory. It is created with owner-only permissions and is never derived from
a hostname or machine identifier. PTY delivery is local-host only. Foreign-host
interactive runtimes remain visible but unavailable.

## Connector configuration

`LOCAL_PROCESS` and `WEBHOOK` runtimes require a non-secret configuration
reference in governed state. The local daemon resolves that reference at
registration/configuration and again before each dispatch.

A local process configuration must identify an absolute, existing, executable
regular file and an existing absolute working directory when supplied. Webhooks
must use HTTPS, except loopback HTTP for local development, and redirects are
disabled. Configuration failures close the delivery attempt without weakening
the durable invocation.

## Operator surfaces

The CLI exposes runtime kind/configuration, invocation consumer/runtime
selection, policy routing, targeted redelivery, and evidence inspection. MCP
provides equivalent request, policy, runtime, inspect, and redelivery fields.
The TUI exposes the same routing forms, runtime repair actions, and a lifecycle
panel containing delivery attempts, transport evidence, and target
acknowledgement.

`doctor` reports unresolved connector references, malformed interactive
runtimes, foreign-host sessions, ambiguous routing, dead sockets, and stale
attempts. It gives a governed `runtime configure` repair path rather than
rewriting existing runtime records.

## Storage and upgrade

- Event/model schema: `2.1.0`
- PostgreSQL schema: `3`
- projection cache: `3`
- local daemon protocol: `4`

PostgreSQL schema 3 adds indexed runtime kind/host, invocation
consumer/preferred-runtime, and delivery transport columns. Personal SQLite
continues storing canonical JSON state. Its projection cache is rebuildable and
is invalidated at schema 3.

Managed project upgrade creates a backup before updating runtime metadata and
rebuilding the cache. Existing invalid runtimes are retained for diagnosis.
After new RFC 0013 events have been written, rollback requires restoring the
pre-upgrade backup because a 2.0 runtime cannot interpret the new event types.

## Integrity boundary

PTY echo and Enter evidence says only that the local terminal transport
performed those actions. It does not say that a model read, understood, or
accepted the instruction. `CLAIMED` is the first authoritative target
acknowledgement, and `COMPLETED` remains the only successful terminal state.
