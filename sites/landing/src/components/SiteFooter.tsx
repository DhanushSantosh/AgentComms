import { BrandMark } from "@/components/BrandMark";
import { documentationPage, site } from "@/lib/site";

export function SiteFooter() {
  return (
    <footer className="site-footer" data-reveal="footer">
      <div className="site-footer-main">
        <div className="site-footer-about">
          <a className="brand brand--footer" href="/" aria-label="Agent Comms home">
            <BrandMark />
            <span>Agent Comms</span>
          </a>
          <p>Governed coordination for concurrent coding agents and the people directing them.</p>
        </div>

        <nav aria-label="Footer navigation" className="site-footer-nav">
          <div>
            <p>Product</p>
            <a href="/download">Download</a>
            <a href="/releases">Releases</a>
            <a href={documentationPage("/releases/changelog/")}>Changelog</a>
            <a href={site.documentationUrl}>Docs</a>
          </div>
          <div>
            <p>Community</p>
            <a href="/support">Contact</a>
            <a href="/support#report-issue">Report an issue</a>
            <a href="https://github.com/DhanushSantosh/AgentComms">GitHub</a>
          </div>
          <div>
            <p>Legal</p>
            <a href="/security">Security</a>
            <a href="/license">License</a>
            <a href="/privacy">Privacy</a>
          </div>
        </nav>
      </div>

      <div className="site-footer-bottom">
        <span>Agent Comms is free and open source.</span>
        <span>AC / {site.productVersion}</span>
      </div>
    </footer>
  );
}
