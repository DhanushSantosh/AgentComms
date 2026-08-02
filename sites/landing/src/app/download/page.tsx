import type { Metadata } from "next";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { downloadRelease, installerMethods } from "@/lib/downloads";
import { documentationPage, site } from "@/lib/site";
import styles from "./download.module.css";

const pageTitle = `Install Agent Comms ${downloadRelease.tag}`;
const pageDescription = "Install the Agent Comms CLI on Linux, macOS, or Windows with the official verified installer.";

export const metadata: Metadata = {
  title: pageTitle,
  description: pageDescription,
  alternates: { canonical: "/download" },
  openGraph: {
    type: "website",
    title: pageTitle,
    description: pageDescription,
    url: "/download",
    images: [{ url: "/social-card.svg", width: 1200, height: 630, alt: "Install Agent Comms" }]
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription, images: ["/social-card.svg"] }
};

export default function DownloadPage() {
  return (
    <>
      <a className="skip-link" href="#installer">Skip to installer</a>
      <SiteHeader documentationUrl={site.documentationUrl} />

      <main className={styles.page} id="main-content">
        <section className={styles.downloadDesk}>
          <header className={styles.intro} data-reveal="download-intro">
            <div className={styles.releaseLine}>
              <span>DOWNLOAD DESK</span>
              <span>{downloadRelease.channel.toUpperCase()} / {downloadRelease.tag}</span>
            </div>
            <div className={styles.introGrid}>
              <h1>Agent Comms,<br /><em>ready to run.</em></h1>
              <div className={styles.introNote}>
                <p>One user-level install for every governed project. The official installer selects the right build and verifies it before replacement.</p>
                <a href={downloadRelease.releaseUrl}>Open the release record <span>↗</span></a>
              </div>
            </div>
          </header>

          <section className={styles.buildPicker} id="installer" aria-label="Agent Comms installers" data-reveal="download-installers">
            <aside className={styles.buildIndex}>
              <p>INSTALL INDEX</p>
              <ol>
                {installerMethods.map((method, index) => (
                  <li key={method.id}>
                    <a href={`#${method.id}`}><span>{String(index + 1).padStart(2, "0")}</span>{method.name}</a>
                  </li>
                ))}
              </ol>
              <div className={styles.releaseStatus}>
                <span aria-hidden="true">!</span>
                <p><strong>{downloadRelease.installerStatus}</strong>{downloadRelease.installerDetail}</p>
              </div>
            </aside>

            <div className={styles.platformShelf}>
              {installerMethods.map((method, index) => {
                const commandID = `install-command-${method.id}`;
                return (
                  <article className={styles.platform} id={method.id} key={method.id} data-reveal={`download-${method.id}`}>
                    <header>
                      <span>{String(index + 1).padStart(2, "0")}</span>
                      <div><p>{method.environment}</p><h2>{method.name}</h2></div>
                      <small>{method.requirements}</small>
                    </header>
                    <div className={styles.installCommand}>
                      <pre><code id={commandID}>{method.command}</code></pre>
                      <button
                        type="button"
                        aria-live="polite"
                        aria-label={`Copy ${method.name} install command`}
                        data-command-source={commandID}
                        data-copy-command
                      ><span data-copy-label>Copy</span><b aria-hidden="true" /></button>
                    </div>
                  </article>
                );
              })}
            </div>
          </section>
        </section>

        <section className={styles.handoff} aria-labelledby="handoff-heading" data-reveal="download-handoff">
          <header>
            <p>AFTER INSTALL</p>
            <h2 id="handoff-heading">Three moves.<br />Then direct.</h2>
          </header>
          <ol>
            <li><span>01 / VERIFY</span><p>Confirm the active executable and release.</p><code><b>$</b> agent-comms version</code></li>
            <li><span>02 / INITIALIZE</span><p>Give the current project a governed control plane.</p><code><b>$</b> agent-comms init</code></li>
            <li><span>03 / OPEN</span><p>Enter the human control room.</p><code><b>$</b> agent-comms tui</code></li>
          </ol>
          <aside>
            <p>Already installed?</p>
            <strong>Running the installer again upgrades the user binary and reconciles managed projects.</strong>
          </aside>
        </section>

        <section className="releases" id="releases" data-reveal="releases">
          <header className="releases-heading">
            <p className="eyebrow">Every release, dated and signed</p>
            <h2>Nothing ships without a changelog.</h2>
            <p>Three tagged releases so far, each backed by a signed history and a written record of what actually changed — not a marketing recap. Every one is <strong>Beta</strong>: before v1.0.0, anything may still change without notice.</p>
          </header>
          <ol className="release-list">
            <li className="release">
              <div className="release-head">
                <span className="release-version">v0.2.1</span>
                <span className="release-channel">BETA</span>
                <span className="release-name">“The Missing Bundle”</span>
                <time className="release-date" dateTime="2026-08-02">2 Aug 2026</time>
              </div>
              <ul className="release-highlights">
                <li>Hotfix: restored the Cosign-signed CLI installer bundles v0.2.0's release was missing, so install.sh/install.ps1 work again.</li>
              </ul>
            </li>
            <li className="release">
              <div className="release-head">
                <span className="release-version">v0.2.0</span>
                <span className="release-channel">BETA</span>
                <span className="release-name">“Chain of Custody”</span>
                <time className="release-date" dateTime="2026-07-31">31 Jul 2026</time>
              </div>
              <ul className="release-highlights">
                <li>One-command project upgrades, with automatic backup and full post-upgrade verification.</li>
                <li>Orchestrator grants now require a separate, human-approved decision.</li>
                <li>A passphrase-protected elevated key gates the most sensitive actions.</li>
                <li>Interactive delivery is a real, auditable state machine — no connector can fake a delivery.</li>
              </ul>
            </li>
            <li className="release">
              <div className="release-head">
                <span className="release-version">v0.1.0</span>
                <span className="release-channel">BETA</span>
                <span className="release-name">“The Control Room”</span>
                <time className="release-date" dateTime="2026-07-19">19 Jul 2026</time>
              </div>
              <ul className="release-highlights">
                <li>First tagged release: signed events, protected work leases, typed messages, approvals.</li>
                <li>Zero-setup SQLite personal authority, or a shared PostgreSQL team authority.</li>
                <li>Full console TUI across Command, Work, Team, Relay, and Project hubs.</li>
              </ul>
            </li>
          </ol>
          <div className="releases-links">
            <a className="action action--ink" href={documentationPage("/releases/changelog/")}>Read the full changelog <span>↗</span></a>
          </div>
        </section>

        <nav className={styles.supportLinks} aria-label="Installation support links" data-reveal="download-links">
          <a href={documentationPage("/start/install/")}><span>Installation guide</span><i>Paths and prerequisites</i><b>↗</b></a>
          <a href={downloadRelease.allReleasesUrl}><span>All releases</span><i>Channels and history</i><b>↗</b></a>
          <a href={downloadRelease.sourceUrl}><span>Release source</span><i>Inspect the tagged code</i><b>↗</b></a>
          <a href={documentationPage("/security/releases/#nightly-builds-developers-not-for-regular-use")}><span>Nightly builds</span><i>Unstable, for developers only</i><b>↗</b></a>
        </nav>
      </main>

      <SiteFooter />
    </>
  );
}
