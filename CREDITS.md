# Credits

Agent Comms is built on the work of other open projects. This file gives
credit where it's due.

## Agent Client Protocol (ACP)

Agent Comms' worker runtimes (`claude-acp`, `codex-acp`, `opencode-acp`) speak
the [Agent Client Protocol](https://agentclientprotocol.com) — an open,
JSON-RPC-based standard for editor/agent communication, originally published
by [Zed Industries](https://zed.dev) in August 2025 and now community-governed
at [github.com/agentclientprotocol](https://github.com/agentclientprotocol/agent-client-protocol)
under Apache-2.0. Before ACP, every editor needed a bespoke integration for
every coding agent; Agent Comms uses the same open standard JetBrains and
Google's Gemini CLI adopted, rather than a proprietary one of its own.

Concretely, Agent Comms uses:

- [`github.com/coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) for the
  Go-side ACP client plumbing (`internal/acpclient`).
- The official `@agentclientprotocol/claude-agent-acp` and
  `@agentclientprotocol/codex-acp` packages as the actual agent subprocess
  behind the `claude-acp` and `codex-acp` worker adapters.

See [`docs/agent-invocations.md`](docs/agent-invocations.md) for the full
worker adapter comparison.
