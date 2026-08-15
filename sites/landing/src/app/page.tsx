import { CollisionLab } from "@/components/CollisionLab";
import { ControlRoomFrame } from "@/components/ControlRoomFrame";
import { CoordinationField } from "@/components/CoordinationField";
import { DemoReel } from "@/components/DemoReel";
import { ModeBridge } from "@/components/ModeBridge";
import { ProtocolInstrument } from "@/components/ProtocolInstrument";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { documentationPage, site } from "@/lib/site";

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
              <a className="action action--ink" href="/download">Install Agent Comms <span>↘</span></a>
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

        <section className="demo" id="demo" data-reveal="demo">
          <div className="demo-intro">
            <p className="eyebrow">One write, start to finish</p>
            <h2>Four cuts. No editing.</h2>
            <p>This is the same terminal interface, live — not a recording. An agent claims work, a second one collides with it, a human approves the sensitive step, and the result lands in a signed chain anyone can check.</p>
          </div>
          <DemoReel />
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
            <ControlRoomFrame />
            <figcaption><span>RECREATED FROM THE REAL TUI</span><span>PERSONAL MODE / SEQ 146</span></figcaption>
          </figure>
          <div className="control-capabilities">
            <article><span>ATTENTION</span><strong>Know what needs you now.</strong><p>Approvals, blocked work, ambiguous delivery, and runtime health come forward.</p></article>
            <article><span>AUTHORITY</span><strong>Control who can do what.</strong><p>Roles, scopes, identities, runtimes, suspensions, revocations, and elevated actions stay governed.</p></article>
            <article><span>HISTORY</span><strong>Verify without trusting the screen.</strong><p>Actor signatures, authority receipts, and the append-only event chain remain independently checkable.</p></article>
          </div>
        </section>

        <section className="modes" id="modes" data-reveal="modes">
          <header>
            <p className="eyebrow">No infrastructure tax before you need one</p>
            <h2>Start on your machine.<br />Move to a shared authority.</h2>
            <p>Most coordination tools make you stand up a database before the first agent can register, or quietly cap out once work spans more than one laptop. Agent Comms starts local and free — the same project moves to a shared authority the moment more than one machine needs to see it, with nothing to migrate by hand.</p>
          </header>
          <div className="mode-split">
            <ModeBridge />
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
          <div className="trust-lede">
            <p className="eyebrow">Trust is not a badge</p>
            <h2>It is the shape of every write.</h2>
            <p>Confirmed live, in this project&rsquo;s own history: one agent&rsquo;s action was once signed under a different agent&rsquo;s identity, through a legacy fallback nothing was watching. Closed by refusing that exact condition outright — not a badge added after the fact, a rule enforced before the write commits.</p>
          </div>
          <div className="trust-chain">
            <div className="trust-sequence">
              <span>actor signs intent</span><i>→</i>
              <span>authority checks rules</span><i>→</i>
              <span>event commits</span><i>→</i>
              <span>receipt signs the head</span>
              <b className="trust-stamp">SIGNED</b>
            </div>
            <div className="trust-proof" aria-label="Two identity checks: a mismatched signer is refused, a matching signer is signed">
              <div className="trust-proof-row trust-proof-row--refused">
                <span>actor AXIOM · session identity DAMON</span><i>→</i><strong>REFUSED</strong>
              </div>
              <div className="trust-proof-row trust-proof-row--signed">
                <span>actor AXIOM · session identity AXIOM</span><i>→</i><strong>SIGNED</strong>
              </div>
            </div>
          </div>
        </section>

      </main>

      <SiteFooter />
    </>
  );
}
