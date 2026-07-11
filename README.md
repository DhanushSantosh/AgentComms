# Agent Comms

Agent Comms is a standalone, project-agnostic coordination system for cooperative agents sharing one filesystem. It combines a deterministic JSON CLI with a governed terminal UI. Durable state is immutable and event-sourced; every durable command is atomically written, signed, hash-chained, and committed to the runtime's isolated Git repository.

## Quick start

```sh
agent-comms init --owner owner
agent-comms agent register --id=builder --json
agent-comms agent activate --id=builder --capabilities=go,test --scopes=src,docs --json
agent-comms task create --id=task-1 --title="Implement feature" --repository=local --branch=feature --resources=internal/service --json
agent-comms task claim --id=task-1 --actor=builder --json
agent-comms verify --json
agent-comms tui
```

The target receives only `.agents` and `.agent-comms`. The runtime is its own Git repository. A configured remote is checkpoint and recovery transport, never a lock. Heartbeats and caches are transient. No telemetry is collected.

## Governance

Agents self-register as `PENDING`; the owner activates them with capabilities and scopes. Write leases default to four hours and enter a one-hour stale grace period without automatic reassignment. Handoffs keep ownership until acceptance. Shared writes, takeovers, and contracts require governed approval and affected acknowledgements. Destructive, irreversible, external, force-push, production-data, and credential operations require a human approval record.

The trust model is cooperative. Signatures and hash chaining make tampering evident; hostile processes with the same OS account are outside scope.

## Commands

Top-level commands are `init`, `version`, `doctor`, `verify`, `migrate`, `agent`, `session`, `task`, `message`, `decision`, `approval`, `artifact`, `status`, `history`, `search`, `archive`, `checkpoint`, `sync`, and `tui`. Add `--json` anywhere for a stable machine-readable envelope.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

See [docs/architecture.md](docs/architecture.md), [docs/governance.md](docs/governance.md), and [IMPLEMENTATION.md](IMPLEMENTATION.md).
