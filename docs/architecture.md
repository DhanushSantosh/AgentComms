# Architecture

CLI, TUI, and stdio MCP are adapters around one typed authorization and validation service. The service reduces immutable signed events into current state and sends every durable mutation through one cross-process transaction boundary.

Each event has a versioned typed payload, actor key fingerprint, Ed25519 signature, SHA-256 hash, and previous-event hash. Writers acquire a local runtime lock, wait with jitter for at most ten seconds by default, fsync a temporary event, atomically rename it, and make one local Git commit. Interrupted temporary files are recoverable. Unknown future events remain in integrity/history views but cannot mutate projections.

Private keys live in platform keyrings. The target repository receives a compact `.agents` bootstrap and an isolated `.agent-comms` Git runtime containing public identities, schemas, events, artifacts, caches, and migration journals. Caches and heartbeats are transient. Git remotes are optional fast-forward checkpoint transport, never locking.
