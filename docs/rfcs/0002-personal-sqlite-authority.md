# RFC 0002: Personal SQLite Authority

## Status

Accepted for implementation.

## Motivation

Requiring PostgreSQL or a container runtime prevents local users from adopting
the transactional control plane. Retaining the filesystem engine as the
default also leaves local multi-agent projects on the architecture that the
control plane replaces.

Personal mode provides the same signed-command, sequenced-event, projection,
idempotency, and receipt contracts as service mode without requiring a network
database. It targets one user account and one machine. Service mode remains the
deployment for coordination across machines and sustained distributed load.

## Architecture

The per-project daemon is the sole SQLite writer. CLI, TUI, and MCP clients use
its existing local socket and transport-neutral application interface.

Each accepted command executes in one SQLite `BEGIN IMMEDIATE` transaction:

1. load and lock the project head by obtaining the database write transaction;
2. validate the command envelope and actor signature;
3. replay or reject the idempotency key;
4. reload the current projection and revalidate authorization and conflicts;
5. append the event, update the projection and head, and store the receipt;
6. commit before returning success.

SQLite runs in WAL mode with foreign keys, full synchronous durability, a
bounded busy timeout, and one writer connection. Events and receipts remain
append-only. Reads use the same database through the daemon and report
`PERSONAL_AUTHORITATIVE` consistency and `LOCAL` connectivity.

The personal authority uses a project-scoped Ed25519 signing key stored in the
platform credential store. Only its public key and fingerprint appear in
project configuration. Startup fails if the private key is unavailable.

## Modes

- `personal`: local daemon and authoritative SQLite; default for new projects.
- `service`: local daemon/cache and remote PostgreSQL authority.
- `legacy`: read-only compatibility, inspection, and explicit migration source.

Personal mode does not support simultaneous writers from multiple machines or
claim PostgreSQL service-mode load targets. Agents on the same machine remain
safe because every client shares the daemon.

## Migration

Legacy-to-personal migration verifies and locks the complete legacy chain,
imports original event bytes into a temporary SQLite database, builds and
compares the projection, records a signed import receipt, atomically activates
personal mode, and makes legacy writes read-only. Interrupted imports never
activate the temporary database.

Personal-to-service migration streams the signed personal history through the
existing resumable import protocol, compares projections, switches the
bootstrap, and retains the personal database as read-only rollback evidence.

## Installation and lifecycle

`agent-comms init` creates personal mode by default. The first command starts
the daemon on demand; concurrent clients converge on the same socket and losing
startup races reconnect to the winner. `agent-comms init --mode legacy` remains
available during the preview compatibility window.

No container runtime, database server, open TCP port, or background system
service is required. The daemon exits gracefully and restarts on demand.

## Security and recovery

Database and socket parents use user-only permissions. Receipt verification is
mandatory when rebuilding projections. Backups use SQLite's online backup
mechanism rather than copying live WAL files independently. Doctor reports
missing keys, insecure permissions, failed integrity checks, stale daemon
ownership, and incomplete cutovers.

