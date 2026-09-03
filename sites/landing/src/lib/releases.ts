export type Release = {
  version: string;
  channel: "BETA" | "STABLE";
  name: string;
  date: string;
  dateLabel: string;
  highlights: readonly string[];
};

export const releases: readonly Release[] = [
  {
    version: "v0.6.0",
    channel: "BETA",
    name: "Chain of Trust",
    date: "2026-09-03",
    dateLabel: "3 Sep 2026",
    highlights: [
      "Approvals for contract publication and approval-gated invocations now bind to a SHA-256 digest of the exact operation and an expiry — a later, differing operation can no longer ride an approval issued for something else.",
      "Standalone installers are now version-pinned and verify the release verifier's own digest against a value committed in the release tag, closing the prior circular trust in first-install verification.",
      "The shared authority service now supports an application-level bearer token (AGENT_COMMS_AUTHORITY_TOKEN), required in production alongside existing TLS and signing-key requirements.",
      "Orchestrator-grant and task-takeover approvals are now single-use — a matching approval is consumed once used and can no longer be replayed to re-authorize the same grant or takeover indefinitely.",
      "Fixes two high-severity CVEs (google.golang.org/grpc, fast-uri)."
    ]
  },
  {
    version: "v0.5.0",
    channel: "BETA",
    name: "Plain Speech",
    date: "2026-08-26",
    dateLabel: "26 Aug 2026",
    highlights: [
      "Every command's default human output changes from a raw JSON dump to a readable summary — semantic status, tables, and next-action hints — while --json stays byte-compatible.",
      "New --output human|plain|json|jsonl contract: plain is the stable uncolored fallback for redirected output, jsonl is a new versioned stream contract for watch and invocation listen.",
      "agent-comms project delete permanently deletes a project, local and remote, with no automatic backup — OWNER-only, elevated-key required, no scripted path."
    ]
  },
  {
    version: "v0.4.0",
    channel: "BETA",
    name: "Proof of Presence",
    date: "2026-08-14",
    dateLabel: "14 Aug 2026",
    highlights: [
      "Identity resolution can no longer silently misattribute a signed action to the wrong actor — closes a real, confirmed incident across the CLI, MCP, and TUI.",
      "Self-service role switching (agent switch-role): any principal can relabel its own role, including freeform custom labels, with no owner/orchestrator elevation required.",
      "interactive-serve now works on Windows, built on ConPTY in place of a unix domain socket and POSIX signals.",
      "install.sh/install.ps1 and agent-comms update no longer need a separately installed cosign CLI to verify a release.",
      "A deep TUI interaction audit: real mouse support in the command palette, a dead keybinding collision fixed, and a real focused-tab indicator."
    ]
  },
  {
    version: "v0.3.0",
    channel: "BETA",
    name: "Point and Click",
    date: "2026-08-08",
    dateLabel: "8 Aug 2026",
    highlights: [
      "Full native mouse support across the TUI, which now scales to a real terminal size instead of requiring a desktop-sized minimum.",
      "Session-pinned interactive delivery — a restarted session resumes the exact right conversation instead of racing each provider CLI's own guess.",
      "A declarative JSON adapter system: add a new CLI provider without touching Go.",
      "A public marketing site and docs site."
    ]
  },
  {
    version: "v0.2.1",
    channel: "BETA",
    name: "The Missing Bundle",
    date: "2026-08-02",
    dateLabel: "2 Aug 2026",
    highlights: [
      "Hotfix: restored the Cosign-signed CLI installer bundles v0.2.0's release was missing, so install.sh/install.ps1 work again."
    ]
  },
  {
    version: "v0.2.0",
    channel: "BETA",
    name: "Chain of Custody",
    date: "2026-07-31",
    dateLabel: "31 Jul 2026",
    highlights: [
      "One-command project upgrades, with automatic backup and full post-upgrade verification.",
      "Orchestrator grants now require a separate, human-approved decision.",
      "A passphrase-protected elevated key gates the most sensitive actions.",
      "Interactive delivery is a real, auditable state machine — no connector can fake a delivery."
    ]
  },
  {
    version: "v0.1.0",
    channel: "BETA",
    name: "The Control Room",
    date: "2026-07-19",
    dateLabel: "19 Jul 2026",
    highlights: [
      "First tagged release: signed events, protected work leases, typed messages, approvals.",
      "Zero-setup SQLite personal authority, or a shared PostgreSQL team authority.",
      "Full console TUI across Command, Work, Team, Relay, and Project hubs."
    ]
  }
] as const;
