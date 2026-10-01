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

## Scope of this file

These rules are the project's standing invariants, not a phase that ended.
Everything else this file used to carry has moved: the August 2026 "current
work" log described work that has since shipped and was removed, and the
"next stabilization areas" list moved to [`backlog.md`](backlog.md), where
open work is tracked.
