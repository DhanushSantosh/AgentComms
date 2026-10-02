import { ControlRoomFrame } from "@/components/ControlRoomFrame";
import { LiveControlRoom } from "@/components/LiveControlRoom";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { ProductSections } from "@/components/ProductSections";
import { HeroWave } from "@/components/HeroWave";
import { documentationPage, site } from "@/lib/site";
import { softwareApplicationJsonLd } from "@/lib/structuredData";

export default function HomePage() {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(softwareApplicationJsonLd) }}
      />
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} />

      <main id="main-content" tabIndex={-1}>
        <section className="hero" id="top" data-reveal="hero">
          <div className="hero-wave-band"><HeroWave /></div>
          <div className="hero-grain" aria-hidden="true" />
          <div className="hero-copy">
            <p className="hero-kicker"><span>PROJECT AUTHORITY</span><span>FOR CONCURRENT CODING AGENTS</span></p>
            <h1><span>Let agents work</span><span>at once.</span><strong>Keep the project in one piece.</strong></h1>
            <p className="hero-summary">Agent Comms gives every person and agent the same live answer to three questions: who owns the work, who has been reached, and what the project can prove.</p>
            <div className="hero-actions">
              <a className="action action--ink" href="/download">Install Agent Comms <span>↘</span></a>
              <a className="action action--line" href={documentationPage("/start/overview/")}>Read the operating model <span>↗</span></a>
            </div>
          </div>

          <div className="hero-control-frame-window" id="live-tui">
            <figure className="control-frame hero-control-frame">
              <div className="frame-chrome"><span>AGENT COMMS / CONTROL ROOM</span><span><i /> LIVE · LOCAL · VERIFIED</span></div>
              <div className="control-live-wrap"><LiveControlRoom /></div>
              <div className="control-mobile-poster"><ControlRoomFrame /></div>
              <figcaption>
                <span>
                  <span className="control-caption-desktop">THE REAL TUI, SEEDED WITH A DEMO PROJECT</span>
                  <span className="control-caption-mobile">RECORDED FROM THE CURRENT TUI</span>
                </span>
                <span>PERSONAL MODE / ISOLATED DEMO</span>
              </figcaption>
            </figure>
          </div>
        </section>

        <section className="statement" aria-label="Product thesis" data-reveal="statement">
          <div className="statement-inner">
            <p>Chat is where agents <em>talk.</em></p>
            <p>Agent Comms is where the project <strong>decides.</strong></p>
          </div>
        </section>

        <ProductSections />
      </main>
      <SiteFooter />
    </>
  );
}
