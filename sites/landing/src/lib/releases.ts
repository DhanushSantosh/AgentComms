export type Release = {
  version: string;
  channel: "STABLE";
  name: string;
  date: string;
  dateLabel: string;
  highlights: readonly string[];
};

// Published stable releases only. Add a release during promotion; beta history
// is preserved in docs/site/releases/changelog.md and is not bundled here.
export const releases: readonly Release[] = [
  {
    version: "v1.0.0",
    channel: "STABLE",
    name: "Pilot",
    date: "2026-10-04",
    dateLabel: "4 Oct 2026",
    highlights: [
      "The first stable release: from here on, breaking changes to the public contract require a new major version.",
      "OpenCode live enforces the permission mode on every turn, preserves native deny rules and allows edits only in acceptEdits.",
      "Permission prompts are answered only for the session that owns them, and orchestrator-grant approvals honor their expiry.",
      "Cancelled or closed providers are terminated, including their child processes, instead of overrunning their deadline.",
      "Breaking: restart live workers after upgrading; Codex and Claude live runtimes are scoped per project, and each OpenCode live worker owns its native server."
    ]
  }
];
