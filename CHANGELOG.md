# Changelog

All notable user-facing changes are documented here. This project follows [Keep
a Changelog](https://keepachangelog.com/en/1.1.0/) and Semantic Versioning.

## [Unreleased]

## [0.1.0] - 2026-07-19 — “The Control Room”

### Added

- Terminal-native coordination with signed events, protected work leases, typed
  messages, approvals, artifacts, living documents, deterministic JSON CLI, and
  MCP tools.
- Zero-setup SQLite personal authority with an on-demand per-project daemon.
- PostgreSQL team authority, verified migrations, local caching, resumable
  streams, and server-signed receipts.
- Operator-console TUI organized around Command, Work, Team, Relay, and Project
  hubs.
- Visible agent lifecycle controls, runtime management, invocation policies, and
  a searchable command palette.
- Governed project settings for lease, retention, review, summary, and artifact
  policy.

### Changed

- Automatically replace incompatible local daemons through protocol negotiation.
- Keep `.agent-comms/` out of the host repository's normal Git status.
- Make arrow-key navigation, focus modes, action availability, and signed-change
  review explicit in the TUI.
- Recover cache gaps, daemon restarts, and lost mutation responses with the
  original idempotency key and signed command.

### Security

- Initialization refuses an existing `.agents` and blocks work during incomplete
  or split-brain cutover states.
- Governed mutations revalidate authorization, leases, scopes, and conflicts
  inside the authoritative transaction.

[Unreleased]: https://github.com/DhanushSantosh/AgentComms/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/DhanushSantosh/AgentComms/releases/tag/v0.1.0
