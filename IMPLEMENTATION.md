# Implementation checklist

- [x] Standalone, project-agnostic Go module and cross-platform entrypoint
- [x] `.agents` bootstrap and isolated `.agent-comms` Git runtime initialization
- [x] Versioned schemas, configuration, signing keys, and generic templates
- [x] Atomic immutable event writes, one durable command per Git commit, crash recovery
- [x] SHA-256 hash chain and Ed25519 signatures with full verification
- [x] Event-reduced state and governed authorization service
- [x] Agent lifecycle, sessions, tasks, leases, handoffs, messages, decisions, approvals
- [x] Artifact content addressing and size/storage policy
- [x] Status, history, search, archive, checkpoint, sync, doctor, migrate framework
- [x] Deterministic non-interactive CLI and stable `--json` envelopes
- [x] Governed terminal TUI with all required views and shared write service
- [x] Unit, state-machine, concurrency, recovery, tamper, routing, approval, Git, archive, artifact, TUI, migration tests
- [x] Documentation, contamination guard, CI, lint, cross-builds, checksums, private releases
- [x] Full local verification before any remote creation or publication
