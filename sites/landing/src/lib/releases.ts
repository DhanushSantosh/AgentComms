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
