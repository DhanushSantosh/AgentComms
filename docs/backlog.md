# Backlog

Deferred, real work items surfaced during development but intentionally not
built yet — each was a deliberate decision to defer, not an oversight. When
one is picked up, remove it from here and note the landing commit.

## Security / governance

- **Approval self-approval is not prevented.** A human (or any elevated
  actor) can both request and approve the same approval record today —
  nothing requires the approver to differ from the requester, for any
  approval, not just orchestrator grants. The two-step orchestrator-grant
  gate (`internal/protocol/transitions.go`'s `hasHumanApproval`) still
  defends against what it was built for (an unattended agent completing the
  whole escalation alone), since self-approval still requires a human to
  consciously type both steps. But it's a real, separate hardening if
  stronger multi-party control is wanted: require `approval.Approver !=
  approval.Requester`, or specifically that the *activating* actor differ
  from whoever requested the approval it's relying on.

- **`agent.rename` display-name impersonation is an accepted, low-severity
  risk, not fixed.** An orchestrator (including an AGENT-principal one) can
  rename another principal's cosmetic `DisplayName` to impersonate a
  trusted human in TUI/CLI listings. IDs stay authoritative underneath —
  this is a social-engineering vector, not a privilege bypass — so it was
  deliberately left as ordinary owner-or-orchestrator-gated rather than
  restricted further, to avoid breaking legitimate managerial renaming.
  Revisit if it's ever actually exploited.

## Test / CI infrastructure

- **`internal/protocol`'s `ValidateTransition` is mostly untested
  in-package.** `transitions_test.go` covers the elevated-key/orchestrator
  work directly; the other ~800 lines (task lifecycle, invocation
  lifecycle, message routing, resource-overlap checks) have only indirect
  coverage through `internal/service`/`internal/app`/`internal/mcp`/`internal/tui`
  integration tests. Not urgent — the indirect coverage is real — but a gap
  worth closing with direct unit tests for the highest-value paths.

## Possibly-a-bug, not yet root-caused

- **`doctor`'s `REVOKED_AGENT_HAS_OPEN_WORK` false positive investigated
  2026-08-06.** The check logic (`internal/doctor/doctor.go:149-174`) is
  correct — `invocationTerminal` covers all possible statuses and the
  matching checks both `RequestedBy` and `Target`. Eight dedicated tests
  (`internal/doctor/doctor_test.go`) confirm correct behavior across
  terminal invocations, terminal tasks, mixed statuses, and no-work
  scenarios. The live observation was most likely caused by stale daemon
  cache state that hadn't yet synced terminal statuses from the authority.

## Runtime workers / agent-spawns-agent

- **No first-class "agent spawns and supervises another agent worker"
  feature, despite the primitive already existing.** `runtime worker`
  (`internal/worker/worker.go`) is already a fully general process any actor
  with shell access can start — including an agent itself, since starting a
  subprocess is an ordinary capability, not something gated by AgentComms.
  In principle an orchestrator agent could already `exec` a `runtime worker`
  process for a *different* registered identity, supervise it, restart it on
  crash, and treat it like a managed subagent pool — the default (looping)
  mode gives "stay alive and keep listening," and `--once`
  (`process at most one invocation and exit`) gives "spawn, do this one
  thing, exit." But nothing surfaces this as an intended, documented
  workflow: no CLI command frames it as "spawn a worker," no guidance in
  `docs/agent-invocations.md` describes an agent doing this to another
  identity, and no supervision/restart/lifecycle-management helper exists —
  a user or agent would have to independently discover and hand-roll process
  supervision around `runtime worker` themselves to get this.
  Investigated live 2026-07-31 while explaining the adapter/`-live`/
  `interactive-serve` architecture to the user; confirmed ACP is unrelated to
  this specific gap (ACP is about editor/agent wire communication for a
  single agent process, not about one agent spawning and managing another's
  process lifecycle) — the actual missing piece is closer to a lightweight
  process-supervisor primitive layered on top of the existing `runtime
  worker`/`--once` mechanics, not a new invocation/delivery concept. Marked
  important by the user; worth a real design pass before building, not an
  ad hoc addition.

## Interactive-serve / multi-agent delivery

- **`interactive-serve` delivery is raw text typed into a PTY and read back
  via heuristics, with no structured acknowledgment of exact content.**
  `interactiveserve/matcher.go`'s `echoed()` decides whether a delivered
  message actually registered as input before pressing Enter using
  progressively looser heuristics: an exact normalized substring match,
  tokenized n-gram sequence matching, then an invocation-ID substring
  fallback. Each layer was added to fix a real, observed failure (box-
  drawing wrap characters, interleaved cursor-movement sequences, TUI status
  headers breaking a delivered message across redraws) — but the underlying
  design property stays the same no matter how many heuristics are layered
  on: there is no way to confirm the *exact* content arrived, only that
  something matching-enough did. Confirmed live 2026-08-06: this exact class
  of fragility bit twice in one session, from two different angles —
  `matcher.go`'s own heuristics needed hardening against real TUI redraws,
  and a live agent's own reply arrived with markdown code spans silently
  stripped in transit (almost certainly shell command-substitution eating
  backtick-quoted text somewhere in how the reply's CLI invocation was
  constructed, a related but distinct fragility of moving text through a
  shell/terminal rather than a structured channel).

  This project already has a more robust answer for *some* adapters: ACP
  (Agent Client Protocol, used by `claude-acp`/`opencode-acp`/`codex-acp`)
  is a structured wire protocol, not text-into-a-pty. Interactive-serve
  sessions have no equivalent today. Worth a real design pass on whether
  interactive-serve-managed sessions could offer a structured channel
  alongside (not necessarily replacing) raw PTY injection, at least for
  delivering the invocation payload itself — removing this whole class of
  transcription/echo bugs rather than continuing to patch `echoed()`'s
  heuristics one observed failure mode at a time. Not scoped or estimated;
  flagged as a design candidate, not a task, in the joint HENRY/HULK/PETER
  field-feedback brainstorm
  ([docs/agent-comms-feedback-2026-08-06.md](agent-comms-feedback-2026-08-06.md#6-raw-pty-text-injection-has-no-structured-acknowledgment-of-exact-content--two-different-symptoms-same-root-cause-hit-live-today)
  item #6).

## Cross-reference

- [RFC 0012](rfcs/0012-agent-identity-deletion-and-key-fingerprinting.md) —
  implemented: revoked principals can be deliberately deleted by a HUMAN
  through the elevated CLI path, while every new event permanently attests
  the verified actor-key fingerprint so a reused ID's occupants remain
  distinguishable.
- [RFC 0011's "Known gaps"](rfcs/0011-managed-project-lifecycle-and-upgrades.md#known-gaps) —
  the TUI's one-confirmation upgrade-approval UX isn't built; a
  confirmation-required plan just blocks the TUI from launching instead.
