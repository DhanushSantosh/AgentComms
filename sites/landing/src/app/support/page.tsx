import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { site, utilityNavItems } from "@/lib/site";
import styles from "@/styles/content-page.module.css";

const pageTitle = "Agent Comms support";
const pageDescription = "Where to report bugs, propose features, ask open-ended questions, and the community standard the project holds everyone to.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/support" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/support",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: pageTitle }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

export default function SupportPage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={styles.page}>
        <PageBreadcrumb label="Support" />

        <header className={styles.intro} data-reveal="support-intro">
          <p className="eyebrow">Get help</p>
          <h1>Stuck? Here's<br />where to go.</h1>
          <p>Security reports belong in a private advisory, not here — see the <a href="/security">security policy</a> instead.</p>
        </header>

        <section className={styles.section} id="report-issue" data-reveal="support-bugs">
          <h2>Reporting a reproducible bug</h2>
          <p>File it in GitHub Issues, and include the Agent Comms version, operating system, <code>doctor --json</code> output with sensitive paths redacted, and exact reproduction steps.</p>
          <p className={styles.externalLabel}>Issue tracker</p>
          <div className={styles.externalAction}>
            <code id="issue-tracker-url">https://github.com/DhanushSantosh/AgentComms/issues/new</code>
            <button type="button" data-copy-command data-command-source="issue-tracker-url" aria-live="polite" aria-label="Copy the issue tracker link"><span data-copy-label>Copy</span></button>
          </div>
        </section>

        <section className={styles.section} id="discussions" data-reveal="support-ideas">
          <h2>Ideas and open-ended questions</h2>
          <p>Product ideas and community questions belong in GitHub Discussions. Maintainers do not automatically close accepted bugs, roadmap work, security work, or accessibility issues solely because they are old.</p>
          <p className={styles.externalLabel}>Discussions</p>
          <div className={styles.externalAction}>
            <code id="discussions-url">https://github.com/DhanushSantosh/AgentComms/discussions</code>
            <button type="button" data-copy-command data-command-source="discussions-url" aria-live="polite" aria-label="Copy the discussions link"><span data-copy-label>Copy</span></button>
          </div>
        </section>

        <section className={styles.section} data-reveal="support-conduct">
          <h2>Code of conduct</h2>
          <p>Be respectful, specific, and constructive. Harassment, discrimination, threats, and disclosure of private information are not accepted. Maintainers may remove harmful contributions or participation to protect the project community.</p>
        </section>
      </main>

      <SiteFooter />
    </>
  );
}
