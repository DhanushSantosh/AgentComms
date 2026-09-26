# RFC 0038: Let an existing project gain remote participants

## Status and owners

**Proposed, 2026-09-26.** Raised by the project owner while standing up a
real cross-machine test; drafted by claude-main. Not yet accepted — this
records the problem and the shape of a solution, and deliberately stops
short of committing to a migration design.

## Problem and desired outcome

Runtime mode is a property of the **project**, not of each **participant**.
`store.Config.RuntimeMode` is a single field validated as exactly `personal`
or `service` (`internal/store/store.go:70,143`), and everything downstream
branches on it: which authority the daemon talks to, where the cache lives,
whether a personal signing key is resolved.

The consequence is that a project is born into one world and cannot leave it:

- `init --mode service` unconditionally calls `CreateProject`
  (`internal/runtimeinit/runtimeinit.go:213`). There is no join path.
- `agc project` offers only `delete` and `upgrade` — nothing that attaches an
  existing project to an authority, and nothing that adds a remote peer.
- `docs/service-deployment.md` states it outright: "Existing projects are not
  converted in place."

So a personal-mode project that wants one remote teammate has exactly one
option: delete it and re-initialize as service mode. For a project whose
value **is** its signed history, that is not an upgrade path, it is data
loss. The project this RFC was written in carries 263 signed events —
governed decisions, human-tier approvals, an orchestrator takeover. None of
it is regenerable.

The second-order effect is that the two modes are never exercised together.
The bug fixed in the commit preceding this RFC — `daemon serve` never passing
the authority token, so service mode failed for every CLI command — survived
because reaching service mode at all requires throwing a project away first.
Friction on a path is why the path stays broken.

Desired outcome: a project can host local personal-mode participants and
remote service-mode participants at once, and an existing project can gain a
remote participant without being recreated.

## Sketch of a design

Not a specification. The point here is to name the moving parts.

1. **Move mode from the project to the participant.** The project declares
   where authority lives; each participant declares how it reaches it. A
   principal record already exists per participant and is the natural home.
2. **A join command.** Something like `agc project join --authority-url ...
   --service-public-key ...`, which attaches *this* checkout to an existing
   authoritative project rather than creating one. This is the missing verb;
   today users are told to copy `.agent-comms/config.json` by hand, which
   works only because that file happens to carry no secrets.
3. **An attach/promote path for an existing personal project.** The hard
   part, see below.

## The edge cases this has to answer

These are the reason this is an RFC and not a patch. Each one can silently
corrupt a project if guessed at.

1. **Which chain is canonical?** A personal project has a complete signed
   chain in its local personal authority. Attaching it to a service authority
   means either replaying that chain into Postgres, or declaring the remote
   chain canonical and the local one history. Replay must preserve every
   hash, receipt and sequence number or `agc verify` breaks — and receipts
   are signed by the *personal* authority's key, which the service authority
   does not hold.
2. **Divergence.** If a project is attached while local events exist that the
   authority has not seen, the two chains have forked. There is no merge for
   a hash-linked log. The honest answers are refuse-if-diverged or
   fast-forward-only; both need stating.
3. **Offline personal participants.** Personal mode works with no network. If
   a participant is personal-mode inside a service-mode project, does it
   write locally and sync later? That is an offline-write model the current
   design does not have, and it reintroduces divergence by construction.
4. **Key trust.** A personal authority signs with a per-project key
   (`__personal_authority__`); a service authority signs with the operator's
   service key. A project that has had both has receipts under two keys.
   Verification must know which key was authoritative for which range.
5. **Demotion.** Can a project go back to personal? If not, say so, because
   attach becomes irreversible and should warn accordingly.
6. **Mixed versions.** Participants on different machines will drift in
   version. `config.json` already pins `toolkit_version`, `toolkit_build_id`
   and `minimum_toolkit_version`; whose pin wins across a mixed team?
7. **Where the token lives.** Service participants need
   `AGENT_COMMS_AUTHORITY_TOKEN`. It is deliberately env-only and absent from
   `config.json`. Any join flow must not tempt users into committing it —
   related: `.env` was not git-ignored until 2026-09-26 despite `compose.yaml`
   requiring three secrets.

## Alternatives considered

- **Status quo: re-init to change mode.** Acceptable only for disposable
  projects; unacceptable for any project with history worth keeping, which is
  the projects this tool exists to serve.
- **Export/import.** `agc export` already emits JSONL history. Importing into
  a fresh service project preserves the *content* but not the signed chain:
  new receipts, new hashes, integrity restarts from zero. Viable as a
  documented "start fresh, keep the record" path; it is not attachment, and
  should not be described as one.
- **Federate the two authorities.** Keep both chains and have them reference
  each other. Strictly more powerful and much harder; out of scope unless
  mutually untrusted tenants ever become a goal, which
  `docs/service-deployment.md`'s trust boundary currently excludes.

## Unresolved questions

1. Is attaching an existing personal project in scope at all, or is the
   deliverable only "a fresh project can have mixed participants" plus a
   documented export/import path for old ones? The first is far more useful
   and far more dangerous.
2. If replay into a service authority is attempted, can personal-authority
   receipts be preserved verbatim, or does the service authority have to
   counter-sign? This decides whether attachment is lossless.
3. Should `agc doctor` warn when a project is personal-mode but has remote
   participants configured, as an intermediate safety net before any of this
   exists?
