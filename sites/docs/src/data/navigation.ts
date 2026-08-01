export type NavigationItem = {
  title: string;
  href: string;
};

export type NavigationSection = {
  title: string;
  shortTitle: string;
  items: NavigationItem[];
};

export const navigation: NavigationSection[] = [
  {
    title: "Start here",
    shortTitle: "Start",
    items: [
      { title: "What Agent Comms does", href: "/start/overview/" },
      { title: "Install", href: "/start/install/" },
      { title: "Quickstart", href: "/start/quickstart/" },
      { title: "Personal and team modes", href: "/start/modes/" },
      { title: "TUI control room", href: "/start/tui/" }
    ]
  },
  {
    title: "User guide",
    shortTitle: "Guide",
    items: [
      { title: "Projects and settings", href: "/guide/projects/" },
      { title: "Agents and access", href: "/guide/agents/" },
      { title: "Tasks and work leases", href: "/guide/work/" },
      { title: "Messages and blockers", href: "/guide/communication/" },
      { title: "Approvals and decisions", href: "/guide/governance/" },
      { title: "Documents and artifacts", href: "/guide/records/" },
      { title: "Updates and diagnostics", href: "/guide/maintenance/" }
    ]
  },
  {
    title: "Agent integration",
    shortTitle: "Agents",
    items: [
      { title: "Choose an integration", href: "/agents/integrations/" },
      { title: "MCP server", href: "/agents/mcp/" },
      { title: "CLI and JSON", href: "/agents/cli-json/" },
      { title: "Runtime workers", href: "/agents/workers/" },
      { title: "Interactive sessions", href: "/agents/interactive/" },
      { title: "Invocation lifecycle", href: "/agents/invocations/" },
      { title: "Routing and delivery", href: "/agents/delivery/" }
    ]
  },
  {
    title: "Team operations",
    shortTitle: "Operate",
    items: [
      { title: "Deploy the service", href: "/operations/deploy/" },
      { title: "Operate and recover", href: "/operations/recovery/" }
    ]
  },
  {
    title: "Security and trust",
    shortTitle: "Trust",
    items: [
      { title: "Identity and authority", href: "/security/identity/" },
      { title: "Audit and integrity", href: "/security/integrity/" },
      { title: "Threat model", href: "/security/threat-model/" },
      { title: "Verify a release", href: "/security/releases/" }
    ]
  },
  {
    title: "Reference",
    shortTitle: "Reference",
    items: [
      { title: "CLI commands", href: "/reference/cli/" },
      { title: "MCP tools", href: "/reference/mcp/" },
      { title: "Configuration and errors", href: "/reference/configuration/" },
      { title: "Protocol glossary", href: "/reference/glossary/" }
    ]
  },
  {
    title: "Releases",
    shortTitle: "Releases",
    items: [
      { title: "Changelog", href: "/releases/changelog/" }
    ]
  }
];

export const flatNavigation = navigation.flatMap((section) => section.items);

export function findNavigationItem(pathname: string) {
  return flatNavigation.find((item) => item.href === pathname);
}
