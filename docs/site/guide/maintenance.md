---
title: Updates and diagnostics
description: Update the user-level installation, reconcile managed projects, inspect health, and export evidence safely.
section: User guide
order: 7
audience: Human operators
lastVerified: 2026-08-01
related: [start/install, operations/recovery]
---

Agent Comms separates updating the user-level binary from reconciling a project's managed runtime. Ordinary commands perform safe reconciliation automatically.

## Check and apply updates

```sh
agent-comms update check
agent-comms update apply
```

The installer replaces the user-level binary once. It does not copy executables into every project. The next eligible command in a managed project checks product version, build ID, project format, cache schema, draft schema, and managed files.

## Inspect an explicit project upgrade

```sh
agent-comms project upgrade plan
agent-comms project upgrade --yes
```

The lifecycle manager plans the change, creates a backup when required, stops incompatible daemons, applies supported migrations, restarts runtime services, and verifies the result. Confirmation-required changes never apply silently.

## Diagnose before editing files

```sh
agent-comms doctor
agent-comms verify
agent-comms control attention
agent-comms runtime list
agent-comms invocation list
```

`doctor` detects project compatibility problems, invalid connector references, runtime/owner mismatches, foreign-host interactive endpoints, ambiguous routing, and stale delivery attempts. Follow its repair command exactly rather than modifying `.agent-comms/`.

## Search and export

```sh
agent-comms search "token rotation"
agent-comms history --limit 50
agent-comms export markdown
agent-comms export jsonl
```

Search and history are bounded. JSONL export preserves machine-readable event evidence; Markdown export is intended for review, not cryptographic verification.

## Recovery boundary

Keep managed backups until the documented rollback window closes. After the project writes event types or schemas unknown to an older binary, rollback requires restoring the matching pre-upgrade backup—not simply replacing the executable.
