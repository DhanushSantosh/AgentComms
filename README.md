# Agent Comms

Agent Comms is a terminal-native coordination system for humans and automated agents working in one shared repository. It provides protected work leases, typed durable messages, governed approvals, actor-bound signatures, an immutable audit trail, a deterministic JSON CLI, and a rich terminal control room.

> v0.2 is a preview. Runtime migrations may be required before v1.

## Install

Official releases are verified with SHA-256 and Sigstore. The preview currently requires `cosign`; native Windows Authenticode and macOS notarization are planned after v0.2.

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.ps1 | iex
```

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.sh | sh
```

Source build:

```sh
go install github.com/DhanushSantosh/AgentComms/cmd/agent-comms@latest
go install github.com/DhanushSantosh/AgentComms/cmd/agent-comms-server@latest
```

## Start a project

Initialise anywhere — no target Git repo needed. `.agent-comms/` is its own private Git repository:

```sh
agent-comms init
agent-comms tui
```

If the project already contains a `.agents` file, plain initialization refuses to proceed. Use the governed, byte-preserving [legacy adoption workflow](docs/legacy-adoption.md); no confirmation flag can bypass preservation.

Interactive setup previews the `.agents` bootstrap and isolated `.agent-comms` runtime before writing them. Automation uses explicit, non-interactive flags:

```sh
agent-comms init --owner owner --non-interactive --yes --json
```

The runtime is its own Git repository with auto-generated `tmp/` and `cache/` gitignored. A remote is optional checkpoint/recovery transport and is never a distributed lock.

## Daily workflow

```sh
agent-comms agent register --id builder --principal-type AGENT
agent-comms agent activate --id builder --role AGENT --scope src --capability go

agent-comms task create --id task-001 --title "Implement API" \
  --repository local --branch feature/api --resource src/api
agent-comms task offer --id task-001 --to builder
agent-comms task claim --id task-001 --actor builder
agent-comms task start --id task-001 --actor builder
agent-comms task renew --id task-001 --actor builder --progress "Handlers complete"

agent-comms message post --id action-001 --kind ACTION --to builder \
  --subject "Run integration tests" --body "Attach the result as evidence."

agent-comms message resolve --id blocker-001  # auto-closes linked BLOCKED task

agent-comms env set --key CI_BRANCH --value main
agent-comms env get --key CI_BRANCH

agent-comms verify --json
agent-comms export markdown --output audit.md
```

Private actor keys are stored in Windows Credential Manager, macOS Keychain, or Linux Secret Service. Headless environments may explicitly configure `AGENT_COMMS_CREDENTIAL_DIR` outside project history or inject `AGENT_COMMS_CREDENTIAL`.

## Interfaces

- `agent-comms tui` opens the Signal Room interface.
- `--json` produces a versioned `agent-comms/v1` envelope and stable error class.
- `agent-comms mcp` runs a stdio MCP server using the same authorization service.
- `agent-comms completion <shell>` generates PowerShell, Bash, Zsh, or Fish completion.
- `agent-comms doctor --explain-config` shows resolved configuration and provenance.
- `agent-comms env set/get/delete/list` manages a typed per-project environment registry.
- `agent-comms sync setup/status/push/pull` manages fast-forward-only checkpoints.
- `agent-comms update check --channel stable|preview` performs an explicit, telemetry-free release check.

## Scalable service mode

For multi-host coordination, the PostgreSQL authority serializes mutations
and returns service-signed receipts while a per-user daemon maintains a
rebuildable SQLite WAL cache. Governed mutations fail closed while offline;
explicit document, message, and artifact-metadata drafts remain local until
submitted.

See the [service deployment and migration guide](docs/service-deployment.md).
See the [agent invocation protocol](docs/agent-invocations.md) for runtime
registration, wakeups, delivery guarantees, and invocation policy.

## Governance defaults

- Principals are immutable `HUMAN` or `AGENT` identities with Owner, Orchestrator, Agent, or Observer roles.
- Leases last four hours and require explicit progress-bearing renewal. Heartbeats never renew ownership.
- FYI, ACTION, CONTRACT, BLOCKER, and DECISION messages have typed per-recipient obligations.
- Shared writes, takeovers, contracts, and scope changes require orchestrator governance.
- Destructive, irreversible, external, production-data, credential, and force-push actions require a HUMAN approver.
- Completed work remains active for seven days and then archives without deleting history.
- Evidence is SHA-256 addressed. Files above 5 MiB require configured Git LFS.
- No telemetry is collected. Update checks are explicit unless the user opts in.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

See [development workflow](docs/development-workflow.md), [contributing](CONTRIBUTING.md), [architecture](docs/architecture.md), [governance](docs/governance.md), [threat model](docs/threat-model.md), [release process](docs/releasing.md), and [release verification](docs/release-verification.md).

Licensed under Apache-2.0.
