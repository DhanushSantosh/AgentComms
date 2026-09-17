import type { Metadata } from "next";
import { PageBreadcrumb } from "@/components/PageBreadcrumb";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { buildFromSourceMethod, downloadRelease, installerMethods } from "@/lib/downloads";
import { documentationPage, site } from "@/lib/site";
import { softwareApplicationJsonLd } from "@/lib/structuredData";
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
    url: "/download"
  },
  twitter: { card: "summary_large_image", title: pageTitle, description: pageDescription }
};

const downloadNavItems = [
  { label: "Installers", href: "#installer" },
  { label: "Releases", href: "/releases" }
];

// buildFromSourceMethod is deliberately not in `desk` -- it's a
// contributor path, not a third installer for the general audience the
// numbered INSTALL INDEX and platform cards are built for. It gets its
// own, visually distinct treatment lower on the page instead of sitting
// as an equal-weight "03" alongside Linux/macOS and Windows.
const desk = installerMethods;
const sourceCommandID = "install-command-source";

export default function DownloadPage() {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(softwareApplicationJsonLd) }}
      />
      <a className="skip-link" href="#installer">Skip to installer</a>
      <SiteHeader documentationUrl={site.documentationUrl} navItems={downloadNavItems} />

      <main className={styles.page} id="main-content">
        <PageBreadcrumb label="Download" />

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
                {desk.map((method, index) => (
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
              {desk.map((method, index) => {
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

          {/* Build from source: a contributor path, not a third installer
              for the general audience the section above is built for --
              given its own small, clearly-labeled aside instead of an
              equal-weight numbered card, so it reads as "also available,
              for developers" rather than "pick one of three." */}
          <aside className={styles.sourceBuild} id={buildFromSourceMethod.id} aria-label="Build from source" data-reveal="download-source">
            <p className={styles.sourceBuildTag}>FOR CONTRIBUTORS</p>
            <div className={styles.sourceBuildHead}>
              <h2>Building from source?</h2>
              <p>{buildFromSourceMethod.requirements}</p>
            </div>
            <div className={styles.installCommand}>
              <pre><code id={sourceCommandID}>{buildFromSourceMethod.command}</code></pre>
              <button
                type="button"
                aria-live="polite"
                aria-label={`Copy ${buildFromSourceMethod.name} command`}
                data-command-source={sourceCommandID}
                data-copy-command
              ><span data-copy-label>Copy</span><b aria-hidden="true" /></button>
            </div>
            <p className={styles.sourceNote}>
              Unsigned, no <code>agent-comms update</code>, no verified provenance — for trying{" "}
              <code>dev</code> or running your own build.{" "}
              <a href={buildFromSourceMethod.detailUrl}>Other shipped binaries & contributing ↗</a>
            </p>
          </aside>
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

        <nav className={styles.supportLinks} aria-label="Installation support links" data-reveal="download-links">
          <a href={documentationPage("/start/install/")}><span>Installation guide</span><i>Paths and prerequisites</i><b>↗</b></a>
          <a href="/releases"><span>All releases</span><i>Channels and history</i><b>↗</b></a>
          <a href={documentationPage("/security/releases/")}><span>Verify a release</span><i>Checksums, signatures, provenance</i><b>↗</b></a>
        </nav>
      </main>

      <SiteFooter />
    </>
  );
}
