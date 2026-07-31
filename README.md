# Agent Comms

Agent Comms is a terminal-native coordination system for humans and automated agents working in one shared repository. It provides protected work leases, typed durable messages, governed approvals, actor-bound signatures, an immutable audit trail, a deterministic JSON CLI, and a rich terminal control room.

> v0.1 is a preview. Projects use the current personal or service authority
> directly; obsolete filesystem runtimes are not supported.

## Install

Official releases are verified with SHA-256 and Sigstore. The preview currently requires `cosign`; native Windows Authenticode and macOS notarization are planned after v0.1.

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

Initialise anywhere — no target Git repo needed. New projects use zero-setup
personal mode: a per-project daemon owns an authoritative SQLite WAL database,
and starts automatically on the first command. PostgreSQL and Docker are not
required for agents running on one machine.

```sh
agent-comms init
agent-comms tui
```

If the project already contains a `.agents` file, initialization refuses to
overwrite it. Remove or rename that file only after verifying it is not an
active Agent Comms bootstrap.

Interactive setup previews the `.agents` bootstrap and isolated `.agent-comms` runtime before writing them. Automation uses explicit, non-interactive flags:

```sh
agent-comms init --owner owner --non-interactive --yes --json
```

The hidden runtime contains local configuration, cache data, artifacts, and
generated agent instructions. PostgreSQL or SQLite—not Git—is authoritative.

## Daily workflow

```sh
agent-comms agent register --id reviewer --principal-type AGENT
agent-comms agent activate --id reviewer --role AGENT --scope src --capability go

agent-comms task create --id review-task-1 --title "Implement API" \
  --repository local --branch feature/api --resource src/api
agent-comms task offer --id review-task-1 --to reviewer
agent-comms task claim --id review-task-1 --actor reviewer
agent-comms task start --id review-task-1 --actor reviewer
agent-comms task renew --id review-task-1 --actor reviewer --progress "Handlers complete"

agent-comms message post --id action-001 --kind ACTION --to reviewer \
  --subject "Run integration tests" --body "Attach the result as evidence."

agent-comms message resolve --id blocker-001  # auto-closes linked BLOCKED task

agent-comms env set --key CI_BRANCH --value main
agent-comms env get --key CI_BRANCH

agent-comms control overview
agent-comms invocation request --to reviewer \
  --instruction "Review the current change" --scope src \
  --consumer WORKER_ONLY
agent-comms invocation next --actor reviewer --runtime reviewer-runtime

agent-comms verify --json
agent-comms export markdown --output audit.md
```

Private actor keys are stored in Windows Credential Manager, macOS Keychain, or Linux Secret Service. Headless environments may explicitly configure `AGENT_COMMS_CREDENTIAL_DIR` outside project history or inject `AGENT_COMMS_CREDENTIAL`.

## Interfaces

- `agent-comms tui` opens the Project Control interface.
- `--json` produces a versioned `agent-comms/v1` envelope and stable error class.
- `agent-comms mcp` runs a stdio MCP server using the same authorization service.
- `agent-comms completion <shell>` generates PowerShell, Bash, Zsh, or Fish completion.
- `agent-comms doctor --explain-config` shows resolved configuration and provenance.
- `agent-comms env set/get/delete/list` manages a typed per-project environment registry.
- `agent-comms update check --channel stable|preview` performs an explicit, telemetry-free release check.
- `agent-comms update apply` atomically installs the verified binary and reconciles every initialized project recorded in the user profile registry through the newly installed build.
- `agent-comms project upgrade` is the single explicit inspect, backup, migrate, resume, restart, and verify operation; compatible projects reconcile automatically on their next normal command.
- `agent-comms project upgrade status|plan` are optional read-only diagnostics, and `--all-known` targets distinct project roots recorded in identity profiles without scanning the filesystem.

The user-level reconciliation marker is keyed by binary build and profile-registry hash. An externally installed build therefore upgrades all registered projects once on first use, while ordinary commands avoid repeatedly walking every project. Use `agent-comms update apply --current-project-only` only when deliberately limiting maintenance to the active project.

## Runtime modes

Personal mode is the default for one user account and one machine. CLI, TUI,
MCP, and local agent runtimes share one daemon and authoritative SQLite
database. Commands remain actor-signed, transactional, idempotent, sequenced,
receipt-signed, and locally streamable.

Invocation commitment, wake-up delivery, target claim, and completion are
reported as separate facts. Requests can isolate consumption to a supervised
interactive runtime or a worker runtime, while existing projects retain
`EITHER` routing until their policy is tightened.

For multi-host team coordination, the PostgreSQL authority serializes mutations
and returns service-signed receipts while a per-user daemon maintains a
rebuildable SQLite WAL cache. Governed mutations fail closed while offline;
explicit document, message, and artifact-metadata drafts remain local until
submitted.

See the [team service deployment guide](docs/service-deployment.md).
See [getting started](docs/agent-onboarding.md) for the sequential
human/agent walkthrough, and the [agent invocation protocol](docs/agent-invocations.md)
for the deep reference on runtime registration, wakeups, delivery
guarantees, and invocation policy.

## Governance defaults

- Principals are immutable `HUMAN` or `AGENT` identities with Owner, Orchestrator, Agent, or Observer roles.
- Leases last four hours and require explicit progress-bearing renewal. Heartbeats never renew ownership.
- FYI, ACTION, CONTRACT, BLOCKER, and DECISION messages have typed per-recipient obligations.
- Shared writes, takeovers, contracts, and scope changes require orchestrator governance.
- Destructive, irreversible, external, production-data, credential, and force-push actions require a HUMAN approver.
- Completed work remains active for seven days and then archives without deleting history.
- Evidence is SHA-256 addressed and constrained by the configured artifact limit.
- No telemetry is collected. Update checks are explicit unless the user opts in.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

See [stabilization priorities](docs/stabilization.md), [development workflow](docs/development-workflow.md), [contributing](CONTRIBUTING.md), [architecture](docs/architecture.md), [governance](docs/governance.md), [threat model](docs/threat-model.md), [release process](docs/releasing.md), and [release verification](docs/release-verification.md).

Worker runtimes speak the open [Agent Client Protocol](https://agentclientprotocol.com), originally published by [Zed Industries](https://zed.dev) — see [CREDITS.md](CREDITS.md).

Licensed under [Apache-2.0](LICENSE).
