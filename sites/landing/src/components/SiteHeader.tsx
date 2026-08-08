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
  { label: "Collision control", href: "/#collision" },
  { label: "Protocol", href: "/#protocol" },
  { label: "Agent relay", href: "/#relay" },
  { label: "Control room", href: "/#control" }
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
