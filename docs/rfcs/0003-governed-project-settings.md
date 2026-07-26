# RFC 0003: Governed Project Settings Workspace

## Status

Accepted

## Context

The project settings TUI was a read-only report. Operators could manage agents and
runtimes only by discovering controls in separate views, while local
configuration appeared to be policy even when the authority did not enforce it.

Project-wide settings must remain consistent across personal SQLite and team
PostgreSQL authority modes. Local interface preferences must not become shared
project truth.

## Decision

Add `project.settings.update` as an owner-or-orchestrator-authorized signed event.
Its projection controls:

- task lease duration and stale-owner grace;
- active-history retention;
- summary and artifact boundaries;
- the project-wide review gate.

PostgreSQL stores the current projection in a one-row `project_settings` table.
Personal authority and local cache retain the same projection in their existing
state snapshots. Older projects receive deterministic defaults until their first
settings event.

Replace the read-only TUI page with a responsive settings workspace. It separates
shared signed governance from per-user interface preferences and links directly
to agent and runtime administration.

The `.agent-comms` runtime remains a dot-prefixed, private implementation
directory for local configuration and cache data. Product screens hide its
physical path by default; diagnostics expose only bounded operational metadata.

## Consequences

Settings changes are auditable, idempotent through the existing command path, and
transactional in team mode. A project setting cannot silently diverge between
clients. Existing event histories remain valid because defaults are applied
deterministically.

The settings schema is intentionally bounded. New controls require an enforced
runtime behavior and a compatible projection update; decorative or
non-functional toggles are not admitted.
