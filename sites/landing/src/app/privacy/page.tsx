import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { site, utilityNavItems } from "@/lib/site";
import styles from "@/styles/content-page.module.css";

const pageTitle = "Privacy";
const pageDescription = "This site sets no cookies, runs no analytics or tracking scripts, and the Agent Comms CLI itself contains no telemetry.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/privacy" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/privacy",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: pageTitle }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

export default function PrivacyPage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={styles.page}>
        <PageBreadcrumb label="Privacy" />

        <header className={styles.intro} data-reveal="privacy-intro">
          <p className="eyebrow">Privacy</p>
          <h1>Nothing to disclose,<br />because nothing is collected.</h1>
          <p>This site sets no cookies and runs no analytics, tracking, or advertising scripts. There are no accounts, no forms, and no newsletter to sign up for — this page describes exactly what happens when you visit.</p>
        </header>

        <section className={styles.section} data-reveal="privacy-site">
          <h2>What this website collects</h2>
          <p>Nothing beyond what any web server sees to deliver a page. Vercel, the host, processes standard request logs (IP address, user agent, requested path) to operate the service — we don't add anything on top of that: no analytics package, no cookie banner because there are no cookies, no fingerprinting, no third-party ad trackers.</p>
        </section>

        <section className={styles.section} data-reveal="privacy-cli">
          <h2>The CLI itself</h2>
          <p>Agent Comms, the software you install, contains no telemetry and never phones home. Project history — signed events, hashes, messages — stays in your own Git repository. See the <a href="/security">security policy</a> for how that's enforced.</p>
        </section>

        <section className={styles.section} data-reveal="privacy-changes">
          <h2>If that ever changes</h2>
          <p>This page changes with it — no separate legal notice, no silent policy drift. What's written above is what's true today.</p>
        </section>
      </main>

      <SiteFooter />
    </>
  );
}
