import Image from "next/image";
import { BrandMark } from "@/components/BrandMark";
import { CollisionLab } from "@/components/CollisionLab";
import { CoordinationField } from "@/components/CoordinationField";
import { CopyInstallButton } from "@/components/CopyInstallButton";
import { ProtocolInstrument } from "@/components/ProtocolInstrument";
import { SiteHeader } from "@/components/SiteHeader";
import { documentationPage, site } from "@/lib/site";

const installCommand = "curl -fsSL https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main/install.sh | sh";

export default function HomePage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} />

      <main id="main-content" tabIndex={-1}>
        <section className="hero" id="top" data-reveal="hero">
          <div className="hero-grain" aria-hidden="true" />
          <div className="hero-copy">
            <p className="hero-kicker"><span>Project authority</span><span>for concurrent coding agents</span></p>
            <h1><span>Let agents work</span><span>at once.</span><strong>Keep the project in one piece.</strong></h1>
            <p className="hero-summary">Agent Comms gives every person and agent the same live answer to three questions: who owns the work, who has been reached, and what the project can prove.</p>
            <div className="hero-actions">
              <a className="action action--ink" href="#install">Install Agent Comms <span>↘</span></a>
              <a className="action action--line" href={documentationPage("/start/overview/")}>Read the operating model <span>↗</span></a>
            </div>
          </div>

          <div className="hero-field-stage" data-motion-stage="hero-field">
            <CoordinationField />
          </div>

          <div className="hero-foot">
            <span>LOCAL FIRST</span><span>APACHE 2.0</span><span>NO TELEMETRY</span><strong>AC / {site.productVersion}</strong>
          </div>
        </section>

        <section className="statement" aria-label="Product thesis" data-reveal="statement">
          <p>Chat is where agents <em>talk.</em></p>
          <p>Agent Comms is where the project <strong>decides.</strong></p>
        </section>

        <section className="collision" id="collision" data-reveal="collision">
          <div className="section-index" aria-hidden="true">CONCURRENCY / OWNERSHIP</div>
          <header className="collision-copy">
            <p className="eyebrow">Collision control</p>
            <h2>Parallel work without parallel confusion.</h2>
            <p>When two agents reach for the same scope, the project—not the fastest terminal—decides who owns it.</p>
          </header>
          <CollisionLab />
        </section>

        <section className="protocol" id="protocol" data-reveal="protocol">
          <div className="protocol-intro">
            <p className="eyebrow">One lifecycle, five different facts</p>
            <h2>“Done” is not a state.</h2>
            <p>A transport can succeed while the agent never acknowledges the work. Agent Comms keeps every boundary explicit.</p>
          </div>
          <ProtocolInstrument />
        </section>

        <section className="relay" id="relay" data-reveal="relay">
          <div className="relay-copy">
            <p className="eyebrow">Direct agent relay</p>
            <h2>Take yourself out of the message loop.</h2>
            <p>Bind a live Codex, Claude, or OpenCode session once. Agents can deliver bounded work to each other, while you keep the evidence and the final say.</p>
            <a href={documentationPage("/agents/interactive/")}>Connect an interactive session <span>↗</span></a>
          </div>
          <div className="relay-sequence" aria-label="DAMON sends an invocation to GORGE and GORGE acknowledges it">
            <div className="relay-party relay-party--source"><span>REQUESTER</span><strong>DAMON</strong><small>CODEX / INTERACTIVE</small></div>
            <div className="relay-message"><span>Verify the auth session changes.</span><small>invocation.request</small></div>
            <div className="relay-evidence"><i /><span>PTY_TEXT_ECHOED</span><i /><span>PTY_ENTER_SENT</span><i /></div>
            <div className="relay-party relay-party--target"><span>TARGET</span><strong>GORGE</strong><small>OPENCODE / INTERACTIVE</small></div>
            <div className="relay-claim"><b>ACKNOWLEDGED</b><span>invocation.claim</span></div>
          </div>
        </section>

        <section className="control" id="control" data-reveal="control">
          <header className="control-heading">
            <p className="eyebrow">One human control surface</p>
            <h2>See the whole project move.</h2>
            <p>Agents, work leases, invocations, approvals, blockers, messages, and signed activity—without opening every terminal to reconstruct the truth.</p>
          </header>
          <figure className="control-frame">
            <div className="frame-chrome"><span>AGENT COMMS / CONTROL ROOM</span><span><i /> LIVE · LOCAL · VERIFIED</span></div>
            <Image src="/images/control-room.png" width={1280} height={800} sizes="(max-width: 960px) 100vw, 90vw" alt="Agent Comms control room showing agents, project attention, and signed activity" />
            <figcaption><span>REAL PRODUCT CAPTURE</span><span>PERSONAL MODE / SEQ 5</span></figcaption>
          </figure>
          <div className="control-capabilities">
            <article><span>ATTENTION</span><strong>Know what needs you now.</strong><p>Approvals, blocked work, ambiguous delivery, and runtime health come forward.</p></article>
            <article><span>AUTHORITY</span><strong>Control who can do what.</strong><p>Roles, scopes, identities, runtimes, suspensions, revocations, and elevated actions stay governed.</p></article>
            <article><span>HISTORY</span><strong>Verify without trusting the screen.</strong><p>Actor signatures, authority receipts, and the append-only event chain remain independently checkable.</p></article>
          </div>
        </section>

        <section className="modes" id="modes" data-reveal="modes">
          <header><p className="eyebrow">Infrastructure that grows only when needed</p><h2>Start on your machine.<br />Move to a shared authority.</h2></header>
          <div className="mode-split">
            <article className="mode mode--personal">
              <div><span>PERSONAL</span><i>DEFAULT</i></div>
              <h3>No account.<br />No database setup.</h3>
              <p>A project-local authority and per-user daemon start on demand. One person can coordinate many local agents immediately.</p>
              <ul><li>Local authoritative writes</li><li>Hidden managed runtime</li><li>Offline cached reads</li><li>Automatic reconciliation</li></ul>
            </article>
            <article className="mode mode--team">
              <div><span>TEAM</span><i>SHARED</i></div>
              <h3>One PostgreSQL authority across hosts.</h3>
              <p>When people and agents span machines, governed mutations move into the service while local daemons keep reads fast.</p>
              <ul><li>Transactional conflict checks</li><li>Server-signed receipts</li><li>Resumable project streams</li><li>Health, metrics, backup, recovery</li></ul>
            </article>
          </div>
        </section>

        <section className="trust" data-reveal="trust">
          <div className="trust-lede"><p className="eyebrow">Trust is not a badge</p><h2>It is the shape of every write.</h2></div>
          <div className="trust-sequence"><span>actor signs intent</span><i>→</i><span>authority checks rules</span><i>→</i><span>event commits</span><i>→</i><span>receipt signs the head</span></div>
        </section>

        <section className="releases" id="releases" data-reveal="releases">
          <header className="releases-heading">
            <p className="eyebrow">Every release, dated and signed</p>
            <h2>Nothing ships without a changelog.</h2>
            <p>Two tagged releases so far, each backed by a signed history and a written record of what actually changed — not a marketing recap.</p>
          </header>
          <ol className="release-list">
            <li className="release">
              <div className="release-head">
                <span className="release-version">v0.2.0</span>
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
            <a className="action action--line" href="https://github.com/DhanushSantosh/AgentComms/releases">View tagged releases <span>↗</span></a>
          </div>
        </section>

        <section className="install" id="install" data-reveal="install">
          <div className="install-copy">
            <p className="eyebrow">Agent Comms v{site.productVersion}</p>
            <h2>Give the project a memory everyone must respect.</h2>
            <p>Install at user level. Initialize once. Every managed project stays reconciled as the binary evolves.</p>
          </div>
          <div className="install-terminal">
            <div><span>USER-LEVEL INSTALL</span><span>LINUX / macOS</span></div>
            <p><i>$</i><code data-install-command>{installCommand}</code></p>
            <CopyInstallButton />
            <footer><span>agent-comms init</span><i>→</i><span>agent-comms tui</span><i>→</i><strong>PROJECT LIVE</strong></footer>
          </div>
          <div className="install-links">
            <a className="action action--paper" href={documentationPage("/start/install/")}>Open installation guide <span>↗</span></a>
            <a className="action action--line-light" href="https://github.com/DhanushSantosh/AgentComms">View source <span>↗</span></a>
          </div>
        </section>
      </main>

      <footer className="site-footer" data-reveal="footer">
        <a className="brand brand--footer" href="#top" aria-label="Agent Comms home"><BrandMark /><span>Agent Comms</span></a>
        <p>Governed coordination for concurrent coding agents and the people directing them.</p>
        <nav aria-label="Footer navigation"><a href={site.documentationUrl}>Docs</a><a href="https://github.com/DhanushSantosh/AgentComms/releases">Releases</a><a href="https://github.com/DhanushSantosh/AgentComms/blob/main/LICENSE">Apache 2.0</a><a href="https://github.com/DhanushSantosh/AgentComms">GitHub</a></nav>
        <span>AC / {site.productVersion}</span>
      </footer>
    </>
  );
}
