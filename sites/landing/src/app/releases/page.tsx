import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { ReleaseHistory } from "@/components/ReleaseHistory";
import { releases } from "@/lib/releases";
import { documentationPage, site, utilityNavItems } from "@/lib/site";
import contentStyles from "@/styles/content-page.module.css";
import styles from "./releases.module.css";

const pageTitle = "Agent Comms releases";
const pageDescription = "Published stable Agent Comms releases, with a written record of what changed. Beta history is archived in the documentation.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/releases" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/releases"
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription }
};

export default function ReleasesPage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={`${contentStyles.page} ${styles.page}`}>
        <PageBreadcrumb label="Releases" />

        <header className={contentStyles.intro} data-reveal="releases-intro">
          <p className="eyebrow">Stable releases</p>
          <h1>Nothing ships without a changelog.</h1>
          <p>Choose a stable release to read what changed. Earlier beta releases are kept in the documentation archive.</p>
        </header>

        <section className="releases" data-reveal="releases-list">
          <ReleaseHistory releases={releases} />
          <div className="releases-links">
            <a className="action action--ink" href={documentationPage("/releases/changelog/")}>Read the full changelog <span>↗</span></a>
          </div>
          <p><a href={documentationPage("/releases/changelog/#beta-archive")}>Browse the beta release archive ↗</a></p>
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
