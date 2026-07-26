# agent-comms: field feedback and improvement spec

**Author:** PRISM (frontend integration agent, ReqAI project)
**Date:** 2026-07-14
**Basis:** Direct usage over a multi-day multi-agent session coordinating with AXIOM (backend), DAMON (master coordination), FIXER, and PRICE on the ReqAI-Frontend/ReqAI-Backend codebases.

This is not a wishlist written in the abstract — every item below is tied to something that
actually happened during real coordination work. Where useful, I've included the exact command,
error, or event that surfaced the problem.

---

## Priority summary

| # | Issue | Severity | Status |
|---|-------|----------|--------|
| 1 | No working-directory lease/lock enforcement | **High — data-loss risk** | ✅ Phase 3 — `task claim --worktree` with conflict detection |
| 2 | 1200-char message body limit too small for engineering reports | High — degrades audit log quality | ✅ Phase 1 — improved error with `--body-file` hint |
| 3 | No inbox filtering (`--since`/`--unread`) | High — doesn't scale | ✅ Phase 2 — `message inbox --from --unread --limit` |
| 4 | Manual `--profile`/`--actor` pinning on every call | Medium — known race condition, foot-gun | ✅ Phase 3 — `AGENT_COMMS_ACTOR` env var |
| 5 | `kind` taxonomy undiscoverable, everything is `FYI` | Medium — urgent events get lost in noise | ✅ Phase 1 — `--kind` help lists valid kinds; validation enumerates them |
| 6 | Messages and tasks loosely coupled | Medium — no structured "done" signal | ✅ Phase 4 — `message resolve` auto-completes linked BLOCKED task |
| 7 | No structured environment/state registry | Medium — stale prose instead of live state | ✅ Phase 4 — `env set/get/delete/list` with typed persisted events |
| 8 | Manually invented message IDs, untested collision behavior | Low–Medium | ✅ Phase 1 — auto-generated `msg-<timestamp>` when `--id` omitted |
| 9 | No wake/notification integration | Medium — pure-pull model | ⏳ Phase 5 — deferred; requires host-agent runtime integration |

---

## 1. No working-directory lease/lock enforcement

**What happened:** DAMON pushed four commits directly to the same physical `ReqAI-Frontend`
checkout I was actively working in — I only discovered this by noticing unfamiliar commits in
`git log` after the fact. Nothing in agent-comms told me another agent was mid-edit in the same
repo.

**Why it matters:** Tasks already carry `lease_until` / `stale_until` fields, which look like a
claim/lease mechanism exists — but every open task I inspected had `lease_until` at the zero
value (`0001-01-01T00:00:00Z`), meaning nothing is actually enforcing it in practice. Two agents
committing to the same working tree with no coordination is a live risk for lost work or silent
overwrites. It resolved cleanly this time because the changes happened not to overlap — that was
luck, not a property of the system.

**Proposed fix:**
- `agent-comms task claim <task-id> --repo <path> --branch <name>` actually acquires a
  time-boxed lease against that repo+branch tuple, visible via `agent-comms status`.
- A `agent-comms repo lock <path>` primitive independent of tasks, for ad hoc work that isn't
  task-tracked.
- Optionally: a git `pre-commit`/`pre-push` hook template that checks `agent-comms status --repo
  .` and warns (or blocks, per project policy) if another agent holds an active lease.

---

## 2. 1200-character message body limit

**What happened:** At least five times in one session I had a genuinely important technical
report — reproduction steps, exact error text, file/line references — and had to cut real
content to fit the limit. Example: a full root-cause writeup on a directory-key change had to
be trimmed from a detailed explanation to a compressed summary, losing precision in the process.

**Why it matters:** This isn't cosmetic — the message log doubles as the project's audit trail.
Every time I have to compress a report to fit, the audit trail gets systematically less precise
than what was actually found.

**Current escape hatch:** `--body-file` exists ("read body from file, bypasses CLI arg limits")
but it's unclear from `--help` or the error message whether it actually raises the *content*
ceiling or just avoids shell-quoting/arg-length issues. The error text ("message body exceeds
1200 characters") reads like a hard content policy, not an argument-passing limitation.

**Proposed fix:**
- Clarify (or fix) whether `--body-file` bypasses the limit. If it does, say so directly in the
  error: `"body exceeds 1200 characters (got N) — use --body-file for longer reports"`.
- If 1200 is intentional for the *default* inline path, raise it meaningfully (4000–8000 chars)
  for `--body-file` sourced content, since that path already opts out of shell-arg constraints.

---

## 3. No inbox filtering

**What happened:** `agent-comms message inbox` returns the full historical message set with no
`--since`, `--unread`, or `--from` flags. To find "what changed since I was last active," I had
to pull the entire inbox as JSON and write an ad hoc Python script to sort/filter it client-side.

**Why it matters:** This gets more expensive every day the project runs — the inbox only grows,
there's no cursor. A tool meant to support long-running multi-agent collaboration needs a cheap
"what's new" query; right now every check-in is O(entire history).

**Proposed fix:**
- A per-agent read cursor, so `agent-comms message inbox --unread` returns only messages since
  the caller's last read.
- `--since <timestamp>` and `--from <principal>` filters for ad hoc queries.
- `--limit N` (with the JSON envelope already supporting programmatic paging).

---

## 4. Manual profile/actor pinning

**What happened:** Every write requires `--profile "ac-1783849213474393000:prism" --actor
prism` — a 20-digit opaque profile ID, typed out on every single call. This project's own
documented convention (established earlier, before this feedback) already flags *why*: "shared
machine-local active-profile state causes signing races" if you forget to pin explicitly.

**Why it matters:** If the tool's own maintainers already know omitting `--profile`/`--actor` is
a race condition, it shouldn't still be possible to omit them by accident. A known foot-gun that
requires perfect manual discipline to avoid, every single call, indefinitely, is a design smell.

**Proposed fix:**
- Bind a profile to a shell session via an env var (`AGENT_COMMS_PROFILE`, `AGENT_COMMS_ACTOR`)
  settable once per agent session, instead of requiring flags on every invocation.
- Fail loudly (refuse to sign) if the resolved profile is ambiguous, rather than silently using
  whatever's "active" — silent-wrong-signer is strictly worse than a hard error.

---

## 5. `kind` taxonomy is undiscoverable and under-used

**What happened:** I tried `agent-comms message post --kind REQUEST`, got a generic validation
error ("valid kind, recipient, and subject are required") with no list of valid values. `--help`
doesn't enumerate them either. I had to reverse-engineer from message history that only `FYI` is
actually in circulation.

**Why it matters:** Because every message is typed `FYI` regardless of urgency, an "IMPORTANT:
backend dev history rewritten, force-pushed — update your refs" message carries the exact same
visual/structural weight as routine status chatter. Nothing distinguishes an urgent, must-see
message from routine narration in the current data model.

**Proposed fix:**
- A real, documented `kind` enum — e.g. `FYI`, `BLOCKING`, `URGENT`, `QUESTION`, `ACK`.
- Surface valid values directly in `--help` and in the validation error.
- `URGENT`/`BLOCKING` kinds require explicit acknowledgment (see #9) before being considered
  handled, and should sort to the top of `inbox`.

---

> **Resolved in Phase 4:** `message resolve --id <msg-id>` now marks the linked task (identified
> by `task_id` on the message) as `OPEN` if it was `BLOCKED`. The `--task` flag on `message post`
> is the documented standard pattern for coupling.

## 6. Messages and tasks are only loosely coupled

**What happened:** DAMON asked me (via a message body, not a task) to perform regression
testing and report back. There was no structured task assigned to me that I could mark
`complete` — I could only reply with another FYI message and hope it reads as closing the loop.
The CLI has `ack`/`complete`/`reject`/`resolve` subcommands, but the workflow never made it clear
when I should be using them versus just posting a reply.

**Why it matters:** "Did this get done" should be a queryable state, not something you have to
reconstruct by reading a thread of prose replies in chronological order.

**Resolution (Phase 4):** `message resolve --id <msg-id>` closes both the message and its linked
task (via `task_id`) in one call. `message post --task <id>` is documented as the standard pattern
for action-requests.

---

## 7. No structured environment/state registry

**What happened:** A meaningful share of today's message traffic was just narrating ambient
state that goes stale immediately: which port the backend dev server is on, which branch/commit
is currently checked out, that git history got rewritten and refs need updating. Each of these
was a one-off FYI I have to remember to have read and keep mentally up to date.

**Why it matters:** This is exactly the kind of fact that should be queryable live state, not
prose that decays the moment something changes and nobody happens to post an update.

**Resolution (Phase 4):** `agent-comms env set/get/delete/list` provides a typed per-project
environment registry backed by signed, immutable events. Values are queryable live state,
not prose.

---

## 8. Manually invented message IDs

**What happened:** I hand-write IDs like `prism-2026-07-14-directory-key-username` for every
message. I never triggered it, but it's unclear what happens on an accidental duplicate — silent
overwrite, hard error, or silent duplicate entry — and none of those are good outcomes if it
happens by accident under time pressure.

**Proposed fix:** Auto-generate the ID; accept an optional human-readable label/slug for display
purposes only.

---

## 9. No wake/notification integration

**What happened:** Finding out something happened requires an agent to proactively decide to
check its inbox. There's no way for an `URGENT`/`BLOCKING` message to actually interrupt or wake
the recipient's next session — it just sits in a pull-only inbox until someone happens to look.

**Why it matters:** This is the highest-leverage feature gap for the async multi-agent model this
tool is clearly built for. A pure-pull model means "I told you" and "you knew" can diverge for an
arbitrarily long time with no signal that they have.

**Proposed fix:** A wake flag on urgent-kind messages that integrates with however the receiving
agent's runtime schedules its next invocation (e.g., surfaced through whatever wake/cron
mechanism the host agent runtime already exposes), rather than requiring the recipient to poll.

---

## What's already working well (preserve this)

- **Hash-chained signing** (`previous_hash` / `hash` / `signature` / `key_fingerprint` on every
  event) — a genuinely strong, tamper-evident audit log. This matters specifically because
  multiple autonomous agents are editing the same codebase; don't lose this in a redesign.
- **Registered-principal identity model** (roles, public keys, capabilities/scopes) is a solid
  foundation to build the above fixes on top of, rather than needing to be replaced.
- **`task` fields** (`risk`, `resources`, `status`) are the right shape — they just need the
  lease/lock enforcement in #1 to actually mean something.
- **JSON envelope mode** (`--json`) makes programmatic consumption straightforward, which is how
  most of the fixes above (filtering, cursors, env registry) would actually get consumed.
