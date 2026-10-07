# RFC 0050: Project-registered agent providers

## Status and owners

Proposed, 2026-10-07. Author: claude-main, at the maintainer's direction
("hardcoded provider ID must not be the case"). Extends RFC 0039, which
made every AGENT actor ID name its provider, and replaces its deferred
"project-scoped custom providers" item (`docs/backlog.md`). Targets a 1.x
minor release; nothing here breaks the v1.0 public contract.

## Problem and desired outcome

An AGENT principal's ID must be `<provider>` or `<provider>-<suffix>`, and
the only accepted providers are `claude`, `codex` and `opencode`, fixed at
build time in `internal/model/provider.go`. A team running any other agent,
such as a Gemini CLI, Aider or an in-house tool, cannot register it under an
honest identity. `agent register --provider gemini` fails with "unknown
provider", and the only workaround is to misdescribe the agent as one of
the three built-ins. That defeats RFC 0039's purpose: the actor ID in every
signed event is supposed to say which runtime produced it.

RFC 0039 tried to make the set extensible by letting a local declarative
adapter file add a provider name, then removed it. Adapter files load in the
CLI process, but agent IDs are validated in the authority (the local daemon
in personal mode, a remote server in team mode), so the CLI accepted the name
and the authority rejected it. Loading adapter files in the authority would
fix personal mode only, and would let an unsigned local file widen which
identities a signed authority accepts.

Desired outcome: a human principal can register a new provider for a
project as a signed, governed event. Every authority and client then accepts
`<provider>` and `<provider>-<suffix>` agent IDs for it, in personal and team
mode alike, with no rebuild. The three built-ins keep working exactly as in
v1.0.

## Proposed design

### 1. Providers become project state

Add a `Providers` collection to `model.State`, keyed by provider name:

    type Provider struct {
        Name        string // the key, e.g. "gemini"
        DisplayName string // optional, e.g. "Google Gemini CLI"
        Description string // optional, free text
        Status      string // ACTIVE or RETIRED
        AddedBy     string // actor ID of the human who registered it
        RetiredBy   string
        RetireReason string
        model.EntityClock // RFC 0041 created/updated times and sequences
    }

The accepted set for a project is the three built-ins plus every ACTIVE
entry in `State.Providers`. It is derived only from signed history, so every
process that holds the same history reaches the same answer. That's the
property the RFC 0039 attempt lacked.

The built-ins are not stored as events. They remain the build-time floor, so
existing projects need no backfill and a project with no provider events
behaves exactly like v1.0.

### 2. Two new event types

- **`provider.register`**, entity ID = provider name. Payload:
  `{ "display_name": "...", "description": "..." }`, both optional.
  Creates the provider as ACTIVE, or reactivates a RETIRED one, keeping its
  original creation clock.
- **`provider.retire`**, entity ID = provider name. Payload:
  `{ "reason": "..." }`, reason required. Marks it RETIRED.

Both are validated in `ValidateTransition`, the validator shared by the CLI,
MCP, TUI and both authority backends, so there is one enforcement point, as
RFC 0039 §5 requires.

### 3. Who may change the set

Both events require a **human principal**, the same bar
`project.settings.update` already sets. Widening which identities a project
accepts is a governance act; an agent, including an orchestrator agent, must
not be able to invent a provider and then register agents under it. Both
events are added to `elevated()`, so the actor must also be the owner or an
active orchestrator, as for other log-wide writes. They don't require the
elevated signing key.

### 4. Name rules

A provider name must match `^[a-z][a-z0-9]{1,23}$`: 2 to 24 characters,
lower-case letters and digits, starting with a letter.

- **No hyphens.** An agent ID splits at the first hyphen after a known
  provider. If `claude-code` could become a provider, the existing agent
  `claude-code-reviewer` (provider `claude`, suffix `code-reviewer`) would
  change meaning. Forbidding hyphens in provider names makes the split
  unambiguous forever. `model.ProviderOf` keeps its longest-match rule only
  as a defensive tie-break.
- **No collisions with existing principals.** Registration fails if any
  existing principal ID, AGENT or HUMAN, equals the name or starts with
  `<name>-`. This stops the change from reinterpreting a grandfathered
  pre-RFC 0039 agent (for example `reviewer`) or a human ID as belonging to
  the new provider.
- **Reserved names.** The three built-ins (already accepted, so registering
  them is an error that says so) and `agent`, `human`, `system`, `owner`,
  `orchestrator`, `observer`, `worker`, `agc`, `agentcomms`, `unknown`,
  `none`, `all`. These read as roles or tooling, not runtimes.

### 5. Retirement

Retiring a provider stops **new** `agent.register` events that use it.
Existing agents of that provider are untouched: they keep their keys, roles,
leases and history, and continue to act. That's the same grandfathering
RFC 0039 applied to IDs that predate its grammar. Retirement never rewrites
or hides history.

- A provider with no remaining active agents can be retired directly. If
  active agents exist, retirement still succeeds, and the command lists
  them so the human can suspend or revoke them separately if that's the
  intent.
- The three built-ins cannot be retired, so a project can never lock itself
  out of every provider.
- `provider.register` on a RETIRED name reactivates it.

### 6. Agent registration uses the project's set

`model.ValidateAgentActorID` and `ProviderOf` take the accepted set as an
argument (a small `model.ProviderSet` built from built-ins plus the state's
ACTIVE providers) instead of reading a package-level map.

- `ValidateTransition` for `agent.register` builds it from the state it
  already validates against.
- Error messages and `--provider` help list the project's actual set, not
  just the built-ins.
- **Replay is unaffected.** `projection.ApplyEvent` still does not validate
  IDs, so an agent registered while its provider was ACTIVE projects
  identically after the provider is retired.

### 7. CLI, MCP and TUI surface

    agc provider add gemini --display-name "Google Gemini CLI" --description "..."
    agc provider list                      # built-ins and registered, with status
    agc provider show gemini
    agc provider retire gemini --reason "..."
    agc agent register --provider gemini   # -> gemini, then gemini-2, ...

- **Interactive shortcut.** When a human runs `agent register --provider
  <name>` interactively and the name is not yet a provider, the CLI offers to
  register it first ("Provider "gemini" is not registered for this project.
  Register it now? [y/N]"). Accepting issues `provider.register` then
  `agent.register` as two separate signed events. With `--non-interactive`,
  or for an agent actor, it fails as today and adds the exact
  `agc provider add` command to the message.
- **MCP:** a read-only `provider_list` tool. MCP connections normally act as
  agents, which cannot change the set, so no mutating provider tool is added.
  `agent_register` errors name the project's set.
- **TUI:** providers are fully manageable, following the existing
  Environment pattern:
  - a new **Providers** view, opened from Settings › Agents & access and
    from the command palette. Columns: name, status, active agents, display
    name, added (RFC 0041 time). Built-ins appear first, marked built-in;
  - **[n] register provider** opens a form (name, display name,
    description) that issues `provider.register`;
  - a row action **retire** on a registered provider opens a form with a
    required reason. It lists the provider's active agents before
    confirming, as the CLI does. Built-in rows offer no retire action;
  - a RETIRED row offers **reactivate** (`provider.register` again);
  - for a non-human actor, the view is read-only and says why ("a human
    principal must register or retire providers"), instead of opening a
    form the authority would reject;
  - the agent register form gains a provider field listing the project's
    accepted set. When a human enters an unregistered name, the TUI shows
    the same "register it now?" confirmation as the CLI, then issues the
    two events in order;
  - the agent inspector shows the provider and whether it is built-in,
    registered or retired.
- **`doctor`:** use the project's set when it strips the provider from an
  ID. Report an INFO finding for agents whose provider is RETIRED, so it's
  visible without being an error.

### 8. Providers are identity, not execution

Registering a provider does not install or configure anything that runs.
Which program serves an agent is still chosen by the worker's `--adapter`:
the built-in adapters, or a local declarative adapter file
(`.agent-comms/adapters/<name>.json`). A `gemini-main` agent is typically
served by a declarative `gemini` adapter. The two stay separate on purpose.
The provider is a signed statement of what the agent is; the adapter is
local, unsigned configuration of how this machine runs it. This RFC does not
let adapter files add providers, which keeps RFC 0039's correction intact.

### 9. Storage

- **Personal mode:** `Providers` is part of the projected state snapshot;
  there's no schema change. The personal authority and projection cache
  replay the new events like any other.
- **Team mode (PostgreSQL):** automatic, additive migration 8 creates a
  `providers` table (`project_id`, `name`, `state` JSONB, created/updated
  sequences), following the existing per-collection tables, and
  `persistProjectionChanges` writes it. It's non-disruptive: it creates an
  empty table and rewrites nothing.

## Alternatives considered

1. **Keep the set hardcoded and add names in releases.** This is what v1.0
   does. Every new runtime needs a release, and teams with in-house agents
   are never served. The maintainer has rejected it.
2. **Load declarative adapter files in the authority** (RFC 0039's rejected
   repair). It breaks team mode, where a remote authority can't read project
   files, and lets an unsigned local file widen a signed identity rule.
3. **Drop the provider prefix requirement entirely.** This loses RFC 0039's
   guarantee that the runtime is readable from the actor ID in every event,
   table and log line, and invites role-only names like `reviewer` back.
4. **Store providers inside `ProjectSettings`.** This needs no table in
   Postgres, but `project.settings.update` replaces the whole settings
   struct, so every settings update would have to carry or preserve the
   provider list. That couples two unrelated governance surfaces and makes a
   settings edit a way to silently drop providers. A separate collection
   with its own events is clearer and gets RFC 0041 timestamps for free.
5. **A per-machine or per-user allow list.** Two machines in one team could
   disagree about which agents are valid. Rejected for the same reason as 2.

## Compatibility and rollout

- **Additive.** The built-ins, the ID grammar, `--provider`/`--id`
  behaviour and every existing principal are unchanged. A project with no
  provider events behaves exactly as v1.0. This fits a minor release, for
  example v1.1.0.
- **Older binaries.**
  - `ApplyEvent` already projects unknown event types to nothing. A v1.0
    binary replaying a v1.1 project ignores `provider.*` events, and still
    projects agents such as `gemini-main` normally, because projection never
    validates IDs.
  - A v1.0 authority would reject new `gemini-*` registrations and would
    refuse the new event types as unsupported. That's safe and explicit.
  - Mixed versions only matter in team mode, where the server is upgraded
    first, as today.
- **Postgres.** Migration 8 is automatic and additive. A v1.0 server
  refuses a database at schema 8 with the existing "newer than this binary
  supports" error.
- **Public contract additions:** two event types, the `provider` command
  group, the `provider_list` MCP tool, a `providers` collection in JSON
  state, and a derived `provider` field on agents in CLI/MCP output, which
  is computed from the ID and never stored. All are additive.
- **Docs:** the identity/agents pages, generated CLI and MCP references, and
  the changelog. Remove the backlog item when this ships.

## Security and privacy

- **The trust boundary is unchanged.** Only a human principal acting as the
  owner or an active orchestrator can widen the set, and every change is a
  signed, hash-chained event with an author and time. An agent cannot
  register a provider for itself, so it cannot mint a new identity class to
  sidestep an operator's expectations.
- **Name rules prevent reinterpretation.** No hyphens, and no collision
  with existing principal IDs, so registering a provider can never change
  which provider an existing agent belongs to.
- **Retirement only stops new registrations.** It doesn't suspend anyone
  silently; revoking or suspending agents stays an explicit, separately
  signed act.
- **No new secrets, credentials or network calls.** Display names and
  descriptions are free text and are shown verbatim in the CLI, TUI and MCP.
  They're length-bounded (64 and 512 characters) and must not contain
  control characters. Agent display names have no such bound today, so
  these are new rules, applied only to provider fields.
- **The declarative adapter boundary is preserved**, so unsigned local files
  still cannot affect identity rules.

## Test and rollout plan

1. **Model:**
   - name grammar (accepts, rejects, reserved, hyphen);
   - `ProviderSet` construction;
   - `ProviderOf` and `ValidateAgentActorID` against a custom set,
     including longest-match with hyphenated suffixes;
   - suggestions only ever naming accepted providers.
2. **Protocol (`ValidateTransition`):**
   - human-only add and retire;
   - an agent orchestrator rejected;
   - collisions with existing AGENT and HUMAN IDs and the `<name>-` prefix;
   - reserved and built-in names rejected;
   - retire reason required;
   - built-in retirement rejected;
   - re-register reactivates;
   - `agent.register` accepted for an ACTIVE custom provider and rejected
     after retirement;
   - existing agents of a retired provider can still act.
3. **Projection:** a deterministic replay of register, retire and register;
   EntityClock times from signed events; unknown-to-old-binary
   compatibility, by replaying a history that contains `provider.*` with
   those types removed from the known set.
4. **Both authorities:**
   - personal authority and Postgres integration tests for the full
     lifecycle;
   - migration 8 apply, idempotence and checksum;
   - an older-schema refusal;
   - `persistProjectionChanges` round trip.
5. **TUI:**
   - the Providers view's rows and columns, with built-ins first;
   - register, retire and reactivate forms dispatching the right events;
   - required retire reason;
   - no retire action on built-ins;
   - the read-only state and message for an agent actor;
   - the register-provider confirmation from the agent register form;
   - rendered-view assertions at narrow and wide terminal sizes, including
     the final frame staying within bounds.
6. **CLI and MCP:**
   - `provider add/list/show/retire`;
   - the interactive register-then-agent flow, plus its `--non-interactive`
     error with the exact command;
   - JSON envelopes and `order`;
   - `provider_list`;
   - help text listing the project's set.
7. **End to end:**
   - in a disposable personal project, register `gemini`, register
     `gemini-main`, activate it, and run a declarative-adapter worker that
     claims and completes an invocation;
   - retire `gemini`, confirm a new `gemini-2` registration is refused and
     `gemini-main` still works;
   - repeat registration and retirement against a real Postgres authority.
8. **Docs:** generated references are current and the docs build passes.

## Resolved decisions

Settled by the maintainer on 2026-10-07:

1. **Built-in providers are not retirable.** They stay the v1.0 floor.
2. **A human principal is enough** to register or retire a provider; no
   separate payload-bound approval is required.
3. **Keep the interactive offer** to register a missing provider during
   `agent register`, in both the CLI and the TUI.
4. **The TUI manages providers too**, not only the CLI (see §7).

## Unresolved questions

None.
