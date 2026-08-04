---
title: Serve an interactive session
description: Wrap a real Claude, Codex, or OpenCode terminal so the daemon can wake it without losing its live conversation.
section: Agent integration
order: 5
audience: Operators
lastVerified: 2026-08-04
related: [agents/delivery, agents/invocations]
---

`interactive-serve` owns a real PTY and runs the provider inside it. The terminal remains the provider's native UI while Agent Comms supervises registration, heartbeat, endpoint publication, delivery serialization, and clean offline state.

Resuming a specific provider conversation (`resume --last`, `--continue`,
`--resume <id>`, and equivalents) after the `--` works like any other
wrapped argument — but only when that session has actually ended. Pointing
a second, `interactive-serve`-wrapped process at a session ID that's
*still running elsewhere* collides: two processes end up attached to one
provider-side session lock, and killing either one disrupts the other.
See "Migrating a live session" below before resuming a session you aren't
certain has already finished.

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

## Opening a dedicated window automatically

Add `--launch-terminal` to skip manually opening a terminal and retyping the command: it re-execs the same invocation, minus that flag, inside a freshly opened window (a short list of known terminal programs per OS — `gnome-terminal`/`konsole`/`kitty`/`foot`/`alacritty`/`xterm` on Linux, `Terminal.app` on macOS, Windows Terminal on Windows), then exits, leaving the current terminal free:

```sh
agent-comms --actor AXIOM runtime interactive-serve \
  --id AXIOM \
  --launch-terminal \
  --claude-allow-agent-comms \
  -- claude
```

This is a convenience over the manual step only. The session still needs a real, dedicated terminal for the reason "Delivery discipline" below explains; nothing about that requirement changes.

## Migrating a live session

Turning a session you're *already sitting in* into a dedicated, wakeable `interactive-serve` runtime — in place, preserving its conversation — needs the old session gone before the new one resumes it, never both alive at once. `--takeover-pid <pid>` does exactly that: it sends `SIGTERM` to `pid`, waits for it to fully exit (escalating to `SIGKILL` if it hasn't), and only then proceeds, so the wrapped command's own resume flag never races a still-live copy:

```sh
agent-comms --actor AXIOM runtime interactive-serve \
  --id AXIOM \
  --launch-terminal --takeover-pid 48213 \
  --claude-allow-agent-comms \
  -- claude --continue
```

Find `pid` yourself (`ps`, `pgrep`, or whatever your shell offers) — Agent Comms has no way to infer "the session I'm currently typing into." Always pair `--takeover-pid` with `--launch-terminal`: the process doing the terminating must not itself be a descendant of the pid it's terminating, and a freshly spawned terminal window never is. Running `--takeover-pid` directly from the same session you're asking it to replace risks the terminate signal reaching your own process too.

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

Interactive PTY delivery is host-local and Unix-only. A foreign-host runtime is unavailable and never marked delivered.
