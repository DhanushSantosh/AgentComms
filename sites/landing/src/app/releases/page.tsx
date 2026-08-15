import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { releases } from "@/lib/releases";
import { documentationPage, site, utilityNavItems } from "@/lib/site";
import contentStyles from "@/styles/content-page.module.css";
import styles from "./releases.module.css";

const pageTitle = "Agent Comms releases";
const pageDescription = "Every tagged Agent Comms release, dated and signed, with a written record of what actually changed.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/releases" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/releases",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: pageTitle }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

export default function ReleasesPage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={`${contentStyles.page} ${styles.page}`}>
        <PageBreadcrumb label="Releases" />

        <header className={contentStyles.intro} data-reveal="releases-intro">
          <p className="eyebrow">Every release, dated and signed</p>
          <h1>Nothing ships without a changelog.</h1>
          <p>{releases.length} tagged releases so far, each backed by a signed history and a written record of what actually changed — not a marketing recap. Every one is <strong>Beta</strong>: before v1.0.0, anything may still change without notice.</p>
        </header>

        <section className="releases" data-reveal="releases-list">
          <ol className="release-list">
            {releases.map((release) => (
              <li className="release" key={release.version}>
                <div className="release-head">
                  <span className="release-version">{release.version}</span>
                  <span className="release-channel">{release.channel}</span>
                  <span className="release-name">“{release.name}”</span>
                  <time className="release-date" dateTime={release.date}>{release.dateLabel}</time>
                </div>
                <ul className="release-highlights">
                  {release.highlights.map((highlight) => (
                    <li key={highlight}>{highlight}</li>
                  ))}
                </ul>
              </li>
            ))}
          </ol>
          <div className="releases-links">
            <a className="action action--ink" href={documentationPage("/releases/changelog/")}>Read the full changelog <span>↗</span></a>
          </div>
          <p className={contentStyles.externalLabel}>Compare tags</p>
          <div className={contentStyles.externalAction}>
            <code id="compare-tags-url">https://github.com/DhanushSantosh/AgentComms/releases</code>
            <button type="button" data-copy-command data-command-source="compare-tags-url" aria-live="polite" aria-label="Copy the compare-tags link"><span data-copy-label>Copy</span></button>
          </div>
        </section>
      </main>

      <SiteFooter />
    </>
  );
}
