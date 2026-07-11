# v0.2 implementation checklist

- [x] Typed schema-v2 event payloads and forward-compatible projections
- [x] Actor-bound Ed25519 credentials with keyring and explicit headless backend
- [x] Fixed roles, principal types, scopes, approval tiers, and risk-based completion
- [x] Cross-process bounded transaction lock and structured BUSY behavior
- [x] Offers, protected leases, explicit renewals, overlap governance, and two-phase handoffs
- [x] Per-recipient FYI, ACTION, CONTRACT, BLOCKER, and DECISION lifecycles
- [x] SHA-256 artifacts with Git LFS threshold policy
- [x] Versioned JSON CLI, standard flags, profiles, config, sync, exports, completions, update checks, and watcher
- [x] Stdio MCP server using the governed service layer
- [x] Signal Room Bubble Tea v2 TUI with responsive role-oriented views and event-chain rail
- [x] Interactive/non-interactive onboarding and managed agent instructions
- [x] Legacy v1 verification, preservation, owner mapping, and migration journal
- [x] PowerShell/POSIX installers and signed multi-platform release workflow
- [x] Apache-2.0 project policy, security, support, threat-model, and verification documentation
- [x] Synthetic unit, state-machine, contention, recovery, tamper, migration, MCP, CLI, artifact, export, and TUI tests
- [x] Local tests, vet, staticcheck, vulnerability scan, cross-build, installer syntax, and end-to-end smoke verification
- [ ] Linux/macOS race jobs and installer matrix in GitHub CI
- [ ] Public visibility and preview tag after explicit release review
