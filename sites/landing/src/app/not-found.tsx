import { BrandMark } from "@/components/BrandMark";
import { site } from "@/lib/site";

export default function NotFoundPage() {
  return (
    <main className="not-found" id="main-content">
      <div className="not-found__mark"><BrandMark /><span>Agent Comms</span></div>
      <p className="eyebrow">404 / SCOPE NOT FOUND</p>
      <h1>This page left the project scope.</h1>
      <p>The route is not part of the current authority. Return to the project overview or continue in the operating manual.</p>
      <div className="hero-actions">
        <a className="action action--paper" href="/">Return home <span>↗</span></a>
        <a className="action action--line-light" href={site.documentationUrl}>Open docs <span>↗</span></a>
      </div>
    </main>
  );
}
