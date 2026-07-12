# Safe adoption of a legacy `.agents`

Never run plain `agent-comms init` over a project that already has `.agents` communication history. Agent Comms refuses that operation before creating runtime state. `--yes` and `--non-interactive` do not bypass the refusal.

The legacy adoption workflow is deliberately staged. It does not infer identities, ownership, current work, decisions, blockers, or contracts from prose.

## 1. Prepare and inspect

Run from the existing Git project with the intended release binary:

```sh
agent-comms migrate adopt --owner <verified-human-id> --json
agent-comms migrate status --json
agent-comms doctor --json
```

Preparation leaves the root `.agents` unchanged. It creates or upgrades the isolated `.agent-comms` runtime, generates `AGENT_INSTRUCTIONS.md`, and stores:

- a byte-identical content-addressed archive under `legacy/agents/<sha256>/.agents`;
- a manifest containing the SHA-256, byte size, source path, archive path, and preservation time;
- a searchable JSONL index whose records are explicitly `UNVERIFIED` and never current truth;
- a `PREPARED` cutover record containing the exact proposed managed bootstrap.

If a schema-v1 runtime exists, its verified event files remain byte-identical under `.agent-comms/legacy/v1/events`. They are searchable evidence only and do not populate the schema-v2 projection.

## 2. Seed current truth explicitly

Using verified people and current repository evidence, explicitly register and activate the owner, orchestrators, and agents. Explicitly create only the tasks, decisions, blockers, and contracts that are still current.

During cutover, ordinary work is blocked. Only inspection, migration, identity activation, and the limited state-seeding commands are available.

After reviewing every category, confirm that seeding is complete:

```sh
agent-comms migrate seed-complete --confirm --json
```

This confirmation means the operator reviewed owner, orchestrators, agents, active tasks, decisions, blockers, and contracts. It does not assert that legacy prose was parsed as truth.

## 3. Collect acknowledgements

Name every active agent that must acknowledge the cutover:

```sh
agent-comms migrate require-acks --agent agent-a --agent agent-b --json
```

Each named agent selects its own actor profile and runs:

```sh
agent-comms migrate ack --json
```

The state advances from `AGENT_ACK_PENDING` to `READY` only after every required active agent acknowledges.

## 4. Preview and activate

Preview prints the exact bootstrap, archive manifest, replacement path, and recovery command without writing the root bootstrap:

```sh
agent-comms migrate activate --json
```

After verifying the preview, activate explicitly:

```sh
agent-comms migrate activate --yes --json
agent-comms doctor --json
agent-comms verify --json
```

Only this `ACTIVATED` step replaces the root `.agents`. The content-addressed archive remains in immutable runtime history.

## Rollback and recovery

Before activation, rollback removes the prepared runtime only when the root `.agents` still hashes to the preserved legacy file:

```sh
agent-comms migrate rollback --json
```

After activation—or after an interrupted activation that published the bootstrap but did not finalize metadata—restore the exact legacy bytes with:

```sh
agent-comms migrate recover --json
```

Recovery verifies the archive hash before atomically restoring `.agents`. Normal work remains blocked until a new governed cutover is completed.

Do not manually copy, rewrite, normalize line endings, or delete the archive. If `doctor` reports split brain, stop work and use migration status plus the documented recovery flow.
