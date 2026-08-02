import { BrandMark } from "@/components/BrandMark";
import { site } from "@/lib/site";

export function SiteFooter() {
  return (
    <footer className="site-footer" data-reveal="footer">
      <div className="site-footer-about">
        <a className="brand brand--footer" href="/" aria-label="Agent Comms home">
          <BrandMark />
          <span>Agent Comms</span>
        </a>
        <p>Governed coordination for concurrent coding agents and the people directing them.</p>
      </div>
      <div className="site-footer-meta">
        <nav aria-label="Footer navigation">
          <a href="/download">Download</a>
          <a href={site.documentationUrl}>Docs</a>
          <a href="https://github.com/DhanushSantosh/AgentComms/releases">Releases</a>
          <a href="https://github.com/DhanushSantosh/AgentComms/blob/main/LICENSE">Apache 2.0</a>
          <a href="https://github.com/DhanushSantosh/AgentComms">GitHub</a>
        </nav>
        <span>AC / {site.productVersion}</span>
      </div>
    </footer>
  );
}
