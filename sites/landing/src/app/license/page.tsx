import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { readRepositoryFile } from "@/lib/repo";
import { site, utilityNavItems } from "@/lib/site";
import styles from "./license.module.css";

const pageTitle = "Agent Comms license";
const pageDescription = "Agent Comms is free and open source under the Apache License 2.0 — what that permits, requires, and limits, plus the full license text.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/license" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/license",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: pageTitle }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

const permissions = ["Commercial use", "Modification", "Distribution", "Patent use", "Private use"];
const conditions = ["License and copyright notice", "State changes"];
const limitations = ["Liability", "Warranty", "Trademark use"];

const licenseText = readRepositoryFile("LICENSE");

// Presentational-only pass over the verbatim license text: every character
// is preserved exactly as read from disk, this only wraps section-heading
// lines ("N. Title.", the top-level terms line, and the closing copyright
// line) in a span for emphasis so the wall of legal text has scannable
// structure.
const sectionHeadingPattern = /^(\s*\d+\.\s+[^.]+\.)(.*)$/;
const licenseLines = licenseText.split("\n").map((line, index) => {
  const sectionMatch = line.match(sectionHeadingPattern);
  if (sectionMatch) {
    return { key: index, line, heading: sectionMatch[1], rest: sectionMatch[2] };
  }
  if (line.trim() === "TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION" || /^Copyright \d{4}/.test(line)) {
    return { key: index, line, heading: line, rest: "" };
  }
  return { key: index, line, heading: null, rest: null };
});

export default function LicensePage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={utilityNavItems} />

      <main id="main-content" tabIndex={-1} className={styles.page}>
        <PageBreadcrumb label="License" />

        <header className={styles.intro} data-reveal="license-intro">
          <p className="eyebrow">Open source, Apache License 2.0</p>
          <h1>Use it. Modify it.<br />Ship it.</h1>
          <p>Agent Comms is free and open source software. The Apache 2.0 license below is the entire agreement — no separate terms, no dual licensing, no per-seat clauses.</p>
        </header>

        <section className={styles.summary} aria-label="License summary" data-reveal="license-summary">
          <div>
            <h2>Permissions</h2>
            <ul>{permissions.map((item) => <li key={item}>{item}</li>)}</ul>
          </div>
          <div>
            <h2>Conditions</h2>
            <ul>{conditions.map((item) => <li key={item}>{item}</li>)}</ul>
          </div>
          <div>
            <h2>Limitations</h2>
            <ul>{limitations.map((item) => <li key={item}>{item}</li>)}</ul>
          </div>
        </section>

        <section className={styles.fullText} aria-label="Full license text" data-reveal="license-text">
          <header>
            <p className="eyebrow">Full text</p>
          </header>
          <pre>
            {licenseLines.map(({ key, line, heading, rest }, index) => (
              <span key={key} className={heading ? styles.sectionHeading : undefined}>
                {heading ? <strong>{heading}</strong> : line}
                {rest}
                {index < licenseLines.length - 1 ? "\n" : ""}
              </span>
            ))}
          </pre>
        </section>
      </main>

      <SiteFooter />
    </>
  );
}
