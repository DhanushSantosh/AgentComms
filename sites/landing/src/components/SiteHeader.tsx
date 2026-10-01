import { BrandMark } from "@/components/BrandMark";

export type SiteHeaderNavItem = {
  label: string;
  href: string;
};

type SiteHeaderProperties = {
  documentationUrl: string;
  navItems?: readonly SiteHeaderNavItem[];
};

const defaultNavItems: readonly SiteHeaderNavItem[] = [
  { label: "Ownership", href: "/#ownership" },
  { label: "Coordination", href: "/#coordination" },
  { label: "Governance", href: "/#control" },
  // The live TUI in the hero, not the governance section: this pointed
  // at #control, so "Control room" landed on approvals instead.
  { label: "Control room", href: "/#live-tui" }
];

export function SiteHeader({ documentationUrl, navItems = defaultNavItems }: SiteHeaderProperties) {
  return (
    <header className="site-header" data-site-header>
      <a className="brand" href="/" aria-label="Agent Comms home">
        <BrandMark />
        <span>Agent Comms</span>
      </a>
      <button
        className="menu-toggle"
        type="button"
        aria-expanded="false"
        aria-controls="site-navigation"
        aria-label="Open navigation"
        data-menu-toggle
      >
        <span aria-hidden="true" />
        <span aria-hidden="true" />
      </button>
      <nav
        className="site-navigation"
        id="site-navigation"
        aria-label="Primary navigation"
        data-site-navigation
      >
        {navItems.map((item) => (
          <a key={item.href} href={item.href}>{item.label}</a>
        ))}
        <a href={documentationUrl}>Docs</a>
      </nav>
    </header>
  );
}
