# RFC 0004: Operator Console and Daemon Negotiation

## Status

Accepted

## Context

The original TUI exposed every projection as a peer navigation item. That made
the interface describe storage rather than the operator's work and produced a
long, difficult-to-scan menu. Forms expanded into visually fragmented canvases
and did not clearly distinguish editing from publishing signed project truth.

The local daemon also had no protocol negotiation. Replacing the CLI binary
while a previous daemon remained alive allowed a new client to send an event
type the old process did not understand.

## Decision

Organize the TUI into five operational hubs:

- Command: overview, personal work, blockers, and approvals;
- Work: tasks, documents, decisions, and archive search;
- Team: principals and runtimes;
- Relay: inbox, invocations, and activity;
- Project: governed settings and audit health.

Each hub has contextual tabs. A persistent command rail shows connectivity,
sequence freshness, and the current actor's authority. Signed mutations use an
explicit edit, review, and sign flow. Existing projection views and actions
remain available through the new hierarchy.

Add a local daemon protocol version to the health contract. A client reuses a
daemon only when project identity, runtime mode, and protocol version all match.
Otherwise it requests graceful shutdown and starts the installed binary.

Add `.agent-comms/` to `.git/info/exclude` when a Git worktree is present. This
keeps private runtime state out of normal repository status without changing the
project's shared ignore policy.

## Consequences

The TUI navigation model changes, but CLI commands, MCP tools, event semantics,
and row-level workflows remain compatible. Future daemon protocol changes must
increment `LocalDaemonProtocolVersion`.

The runtime directory remains accessible for explicit diagnostics and migration,
but it is hidden from default graphical and Git work surfaces.
