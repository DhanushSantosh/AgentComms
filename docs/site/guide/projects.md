---
title: Projects and settings
description: Inspect project identity, change governed policy, and understand what belongs in the hidden runtime.
section: User guide
order: 1
audience: Human operators
lastVerified: 2026-09-02
related: [start/quickstart, guide/maintenance]
---

An initialized project has one project ID, owner, runtime mode, signed history, and set of governed policies. Run commands from the project root or supply `--project /path/to/project`.

## Inspect the control plane

```sh
agent-comms status --details
agent-comms attention
agent-comms config --details
```

`status` combines workforce, active work, and recent activity, with the per-status breakdown behind `--details`. `attention` is the shortest path to blocks, approvals, stale work, and failed delivery. `config --details` reports local runtime configuration and the control-plane limits rather than governed project policy.

## Governed project settings

Project settings are signed policy changes, not edits to a local preferences file. The owner or an orchestrator changes them through the TUI project settings form or the corresponding event-producing command surface.

Current settings include default lease length, stale grace, active retention, summary length, artifact size limits, and review requirements. Policy validation occurs inside the authority transaction so a stale client cannot bypass a concurrent change.

## The hidden runtime

`.agent-comms/` is hidden by default and contains managed project state, sockets or endpoint metadata, caches, drafts, artifacts, and lifecycle records depending on mode. Treat it as implementation state:

- do not commit it unless a project deliberately defines another policy;
- do not open or mutate its databases directly;
- do not delete individual files to repair a daemon;
- use `doctor`, managed upgrade commands, export, and verification tools instead.

## Read state from another directory

```sh
agent-comms --project /work/project status
agent-comms --project /work/project --json task list
```

`--actor` chooses an identity only when the local credential matches. It is not an impersonation flag.
