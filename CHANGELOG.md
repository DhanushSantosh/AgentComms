# Changelog

All notable user-facing changes are documented here. This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and Semantic Versioning.

## [Unreleased]

### Added

- Governed, byte-preserving adoption and recovery for legacy `.agents` communication history.
- Living document events and terminal views for shared reference material.

### Changed

- Legacy context extraction produces unverified review candidates and never activates prose as current truth.

### Security

- Initialization refuses an existing `.agents` and blocks work during incomplete or split-brain cutover states.

## [0.2.0-preview.1] - 2026-07-12

### Added

- Preview terminal application with deterministic JSON CLI, governed TUI, MCP server, signed event history, leases, typed messages, approvals, artifacts, installers, and signed cross-platform releases.

[Unreleased]: https://github.com/DhanushSantosh/AgentComms/compare/v0.2.0-preview.1...HEAD
[0.2.0-preview.1]: https://github.com/DhanushSantosh/AgentComms/releases/tag/v0.2.0-preview.1
