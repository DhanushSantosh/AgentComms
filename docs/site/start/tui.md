---
title: Use the TUI control room
description: Navigate the terminal interface, manage the project, and complete elevated-key-signed actions safely from the TUI.
section: Start here
order: 5
audience: Human operators
lastVerified: 2026-10-01
related: [guide/agents, guide/governance, guide/maintenance, security/identity]
---

Launch the terminal control room from any initialized project:

```sh
agent-comms tui
```

![Current overview separating message readiness and runtime presence, with work, attention, and signed history](/tui-overview.png)

[Watch the terminal tour](/tui-demo.mp4) or open the
[animated preview](/tui-demo.gif). These captures use the current real TUI and
an isolated demo project; navigation pauses are shortened. There is no audio.

## Navigation

| Key | Action |
|---|---|
| Arrow keys | Move between navigation items, rows, and controls. |
| Enter | Open the selected section, record, or action. |
| Tab / Shift+Tab | Move through fields inside a form. |
| Escape | Close a form or return to the previous view. |
| Page Up / Page Down | Scroll the current overview, information page, settings page, or long form. |
| Shift+Page Up / Shift+Page Down | Scroll a table's lower inspector or delivery-detail pane without moving the selected row. |
| `m` on the overview | Open the inbox to act on the message obligations summarized in Needs Attention. |
| `?` | Show the current help surface. |

The highlighted row and section marker are the authoritative navigation indicators. The sidebar's command label opens an action surface; it is not a slash-command text box.

## What you can control

The TUI exposes project overview and attention queues, tasks, inbox messages, agents, runtimes, approvals, invocations, project settings, documents, decisions, local drafts, blockers, audit health, activity, and archive search. In Drafts, you can save or delete one local draft at a time; deletion asks for confirmation and frees that draft's storage quota.

Agent management includes registration, activation, suspension, revocation, deletion when eligible, role/scope updates, and runtime inspection. Invocation views expose consumer routing, preferred runtimes, delivery evidence, acknowledgement, lifecycle state, and explicit redelivery.

The overview shows message readiness and execution-runtime presence separately.
An active registered agent can exchange durable messages and receive an
invocation request even when it has `NO RUNTIME`; an eligible online runtime
is needed for automatic invocation delivery and execution claims. `PENDING`
means the request exists, not that the agent has started work. Bounded panes
show a position marker when more content can be scrolled into view. The
overview previews the three highest-priority attention items with a count of
the rest; the Inbox places messages needing your action before FYIs.

Approval requests can specify an expiry duration for any approval action. It is optional for task takeover and shared-write, and required for bound contract and invocation approvals. The Approvals view shows and marks an elapsed expiry even when the recorded status remains APPROVED; an expired approval cannot authorize a new transition. An already-granted task lease is not revoked when its approval later expires.

## Real project states

The live overview separates message readiness from runtime presence and previews the highest-priority items needing attention. Open the Inbox for the full action queue, or select an agent to inspect its identity and runtime details. The layout adapts to terminal size; use the on-screen scroll marker when a pane has more content than fits.

## Sensitive actions

![Current agent list with lifecycle, role, principal type, and scopes](/tui-agents.png)

![Human-tier approval inspector showing the actual action, requester, and reason](/tui-approvals.png)

Actions that require a passphrase-protected elevated human key -- granting Orchestrator, approving a HUMAN-tier approval, revoking another Orchestrator or HUMAN principal, and deleting a revoked identity -- have a masked "Elevated-key passphrase" field right in their TUI form. Typing your passphrase there completes the transition in the TUI itself; it is not a stand-in for something the CLI still has to finish. Leave the field blank and the TUI refuses cleanly with an exact CLI command instead, the same way it always has.

**Registering** a new elevated key (`agent elevate-key`) is the one genuinely CLI-only step in this whole story: neither the TUI nor MCP offers a form for it, by design (see [Identity and authority](/security/identity)).

## Freshness and connectivity

The header reports local/service mode, connectivity, cache sequence, and server sequence where applicable. A cached team-mode read is useful context, but it is not permission to report a governed mutation as successful while offline.
