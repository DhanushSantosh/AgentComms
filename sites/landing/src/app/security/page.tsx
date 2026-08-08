import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { site, utilityNavItems } from "@/lib/site";
import styles from "@/styles/content-page.module.css";

const pageTitle = "Agent Comms security policy";
const pageDescription = "How to report a vulnerability in Agent Comms privately, the trust model that already protects project integrity, and the supported-version policy.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/security" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/security",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: pageTitle }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

export default function SecurityPage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={styles.page}>
        <PageBreadcrumb label="Security" />

        <header className={styles.intro} data-reveal="security-intro">
          <p className="eyebrow">Responsible disclosure</p>
          <h1>Found a flaw?<br />Tell us before anyone else.</h1>
          <p>Report suspected vulnerabilities privately through GitHub Security Advisories. Do not open a public issue containing exploit details or credentials.</p>
        </header>

        <section className={styles.section} data-reveal="security-process">
          <h2>What happens after you report</h2>
          <p>Maintainers aim to acknowledge credible reports within three business days. Validation and fix development happen privately, disclosure is coordinated with the reporter, and fixes ship before any detailed publication. CVEs and contributor credit are given when appropriate.</p>
          <div className={styles.cta}>
            <a className="action action--ink" href="https://github.com/DhanushSantosh/AgentComms/security/advisories/new">Open a private advisory <span>↗</span></a>
          </div>
        </section>

        <section className={styles.section} data-reveal="security-model">
          <h2>What the trust model already protects</h2>
          <p>The cooperative trust model protects integrity with actor-bound signatures, an immutable hash chain, scoped authorization, and atomic Git transactions. Hostile processes running as the same operating-system account are outside the isolation boundary. Agent Comms contains no telemetry and never stores private actor keys in project history.</p>
          <ul className={styles.propertyList}>
            <li>Actor-bound signatures</li>
            <li>Immutable hash chain</li>
            <li>Scoped authorization</li>
            <li>Atomic Git transactions</li>
            <li>No telemetry</li>
          </ul>
        </section>

        <section className={styles.section} data-reveal="security-versions">
          <h2>Supported versions</h2>
          <p>Before v1, the newest preview line receives best-effort fixes. After v1, the current stable minor is supported, and the previous stable minor receives critical security fixes for six months after replacement.</p>
        </section>
      </main>

      <SiteFooter />
    </>
  );
}
