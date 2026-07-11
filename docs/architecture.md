# Architecture

The CLI and TUI call one service layer. That layer authorizes commands against a state projection reduced from immutable events. The store serializes local writes, writes and fsyncs a temporary file, atomically renames it, and creates exactly one local Git commit. Startup removes interrupted temporary writes. Ed25519 signatures cover SHA-256 event hashes, and each event includes the prior hash.

Runtime layout:

```text
.agents
.agent-comms/
  .git/
  config.json
  signing.key       # local, ignored
  signing.pub
  events/
  artifacts/sha256/
  tmp/              # transient
  cache/            # transient
```

Version one assumes concurrent cooperative agents on a shared filesystem. Git remotes provide checkpoints and recovery only.
