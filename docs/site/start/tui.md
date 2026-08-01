---
title: Use the TUI control room
description: Navigate the terminal interface, manage the project, and understand which sensitive actions remain CLI-only.
section: Start here
order: 5
audience: Human operators
lastVerified: 2026-08-01
related: [guide/agents, guide/governance, guide/maintenance]
---

Launch the terminal control room from any initialized project:

```sh
agent-comms tui
```

![Agent Comms terminal control room showing tasks, agents, and invocation delivery](/tui-demo.gif)

## Navigation

| Key | Action |
|---|---|
| Arrow keys | Move between navigation items, rows, and controls. |
| Enter | Open the selected section, record, or action. |
| Tab / Shift+Tab | Move through fields inside a form. |
| Escape | Close a form or return to the previous view. |
| `?` | Show the current help surface. |

The highlighted row and section marker are the authoritative navigation indicators. The sidebar's command label opens an action surface; it is not a slash-command text box.

## What you can control

The TUI exposes project overview and attention queues, tasks, inbox messages, agents, runtimes, approvals, invocations, project settings, documents, decisions, blockers, audit health, activity, and archive search.

Agent management includes registration, activation, suspension, revocation, deletion when eligible, role/scope updates, and runtime inspection. Invocation views expose consumer routing, preferred runtimes, delivery evidence, acknowledgement, lifecycle state, and explicit redelivery.

## Real project states

These captures come from the current TUI running against an isolated personal-mode project. The overview separates workforce state, urgent obligations, and append-only activity; the agents view keeps management actions beside the selected identity.

![Current Agent Comms overview with workforce, attention, and activity panels](/tui-overview.png)

![Current Agent Comms agents view with selection and management actions](/tui-agents.png)

## Sensitive actions

Actions that require a passphrase-protected elevated human key do not read the passphrase from Bubble Tea's raw terminal input. The TUI refuses those actions with an exact CLI command instead of hanging or weakening the authorization rule.

Use the CLI for elevated identity deletion, orchestrator grants, or human-tier approvals until a dedicated masked passphrase form is available.

## Freshness and connectivity

The header reports local/service mode, connectivity, cache sequence, and server sequence where applicable. A cached team-mode read is useful context, but it is not permission to report a governed mutation as successful while offline.
