import { BrandMark } from "@/components/BrandMark";

type SiteHeaderProperties = {
  documentationUrl: string;
};

export function SiteHeader({ documentationUrl }: SiteHeaderProperties) {
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
        <a href="/#collision">Collision control</a>
        <a href="/#protocol">Protocol</a>
        <a href="/#relay">Agent relay</a>
        <a href="/#control">Control room</a>
        <a href={documentationUrl}>Docs</a>
      </nav>
      <a className="header-action" href="/download"><span>Download</span><i aria-hidden="true">↘</i></a>
    </header>
  );
}
