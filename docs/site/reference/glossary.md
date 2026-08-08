---
title: Protocol glossary
description: Use Agent Comms terms consistently across the CLI, TUI, MCP, service, and project documentation.
section: Reference
order: 4
audience: Everyone
lastVerified: 2026-08-01
related: [start/overview, security/integrity]
---

## Authority

The component that sequences and commits governed mutations. Personal mode uses the local daemon and SQLite; team mode uses the PostgreSQL service.

## Actor and principal

The actor is the identity signing a command. A principal is its durable project record, typed as human or agent and carrying lifecycle, role, scopes, and key history.

## Runtime

A supervised process presence owned by an agent. `WORKER` runtimes autonomously consume invocations. `INTERACTIVE` runtimes represent live host-local PTY sessions.

## Invocation

A bounded governed request for a target agent to act. Request commitment, transport delivery, target claim, execution, and completion are separate events.

## Delivery attempt

A leased reservation to perform one external wake-up action against one eligible runtime. It closes with evidence-backed success, failure, or timeout.

## Claim

The first authoritative target acknowledgement. Claiming is exclusive and transactionally validates runtime ownership, presence, capacity, consumer mode, and preferred runtime.

## Lease

Time-bounded ownership of task resources. Renewal requires progress. Expiry makes stale work eligible for governed recovery.

## Projection

Current state derived from immutable events. PostgreSQL uses normalized authority projections; local daemons maintain rebuildable cached projections.

## Receipt

The authority's signature over the committed project, sequence, event hash, actor-intent hash, and time.

## Draft

Bounded local preparation that creates no event, lease, obligation, or current truth until successfully submitted.

## Consumer mode

The routing constraint `INTERACTIVE_ONLY`, `WORKER_ONLY`, or `EITHER` that controls which runtime kind may receive and claim an invocation.
