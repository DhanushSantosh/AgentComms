---
title: Serve an interactive session
description: Wrap a real Claude, Codex, or OpenCode terminal so the daemon can wake it without losing its live conversation.
section: Agent integration
order: 5
audience: Operators
lastVerified: 2026-08-10
related: [agents/delivery, agents/invocations]
---

`interactive-serve` owns a real pseudo-terminal and runs the provider inside it. The terminal remains the provider's native UI while Agent Comms supervises registration, heartbeat, endpoint publication, delivery serialization, and clean offline state. On Linux and macOS this is a real pty (`github.com/creack/pty`); on Windows it's ConPTY (`github.com/charmbracelet/x/conpty`) — same behavior, platform-appropriate mechanism underneath.

> [!NOTE]
> Windows requires **Windows 10 version 1809 (October 2018 Update) or
> later** — ConPTY's own platform floor. `interactive-serve` reports a
> clear error naming this requirement if the pseudo console can't be
> allocated; it does not fail silently or behave unreliably on an
> unsupported build.

Resuming a specific provider conversation (`resume --last`, `--continue`,
`--resume <id>`, and equivalents) after the `--` works like any other
wrapped argument — but only when that session has actually ended. Pointing
a second, `interactive-serve`-wrapped process at a session ID that's
*still running elsewhere* collides: two processes end up attached to one
provider-side session lock, and killing either one disrupts the other. See
[agent-invocations.md's "Migrating a live, ordinary session into
interactive-serve"](https://github.com/DhanushSantosh/AgentComms/blob/main/docs/agent-invocations.md#migrating-a-live-ordinary-session-into-interactive-serve)
for `--takeover-pid`, the safe way to hand one off in place, before
resuming a session you aren't certain has already finished.

## Codex

```sh
agent-comms --actor DAMON runtime interactive-serve \
  --id DAMON \
  -- codex resume --last
```

## OpenCode

```sh
agent-comms --actor GORGE runtime interactive-serve \
  --id GORGE \
  -- opencode
```

## Claude Code

```sh
agent-comms --actor AXIOM runtime interactive-serve \
  --id AXIOM \
  --claude-allow-agent-comms \
  -- claude --continue
```

The `--` separator is required. Everything after it belongs to the wrapped provider. `--claude-allow-agent-comms` grants unattended Bash permission only for the resolved Agent Comms executable and its basename; it does not grant general shell access or override Claude's prompt-injection judgment.

## Runtime repair

If an existing runtime has the wrong kind or connector, stop it and apply the exact `doctor` repair while it is offline:

```sh
agent-comms --actor DAMON runtime configure \
  --id DAMON \
  --kind INTERACTIVE \
  --connector INTERACTIVE \
  --max-concurrent 1
```

`interactive-serve` then validates owner, kind, connector, and host before advertising its endpoint. The socket location is a canonical per-user runtime path and does not depend on `TMPDIR`.

## Delivery discipline

The wrapped terminal must be dedicated to the runtime. Agent Comms cannot distinguish a human actively typing from an otherwise idle provider. The delivery coordinator serializes writers, waits for the provider's busy marker to clear, rejects embedded newlines, verifies that the full notification text echoed, and then submits Enter.

Interactive PTY delivery is host-local. A foreign-host runtime is unavailable and never marked delivered — including across a Linux/macOS host and a Windows host, which never share a control socket even for the same project.
