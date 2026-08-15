import { CollisionLab } from "@/components/CollisionLab";
import { ControlRoomFrame } from "@/components/ControlRoomFrame";
import { DemoReel } from "@/components/DemoReel";
import { ModeBridge } from "@/components/ModeBridge";
import { SiteFooter } from "@/components/SiteFooter";
import { SiteHeader } from "@/components/SiteHeader";
import { LiveAuthorityStream } from "@/components/LiveAuthorityStream";
import { LifecycleOrbit } from "@/components/LifecycleOrbit";
import { Reveal } from "@/components/Reveal";
import { documentationPage, site } from "@/lib/site";

export default function HomePage() {
  return (
    <>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <SiteHeader documentationUrl={site.documentationUrl} />

      <main id="main-content" tabIndex={-1}>
        <section className="hero" id="top" data-reveal="hero">
          <div className="hero-grain" aria-hidden="true" />
          <div className="hero-glow" aria-hidden="true" />
          <div className="hero-copy">
            <p className="hero-kicker"><span>PROJECT AUTHORITY</span><span>FOR CONCURRENT CODING AGENTS</span></p>
            <h1><span>Let agents work</span><span>at once.</span><strong>Keep the project in one piece.</strong></h1>
            <p className="hero-summary">Agent Comms gives every person and agent the same live answer to three questions: who owns the work, who has been reached, and what the project can prove.</p>
            <div className="hero-actions">
              <a className="action action--ink" href="/download">Install Agent Comms <span>↘</span></a>
              <a className="action action--line" href={documentationPage("/start/overview/")}>Read the operating model <span>↗</span></a>
            </div>
          </div>

          <div className="hero-field-stage" data-motion-stage="hero-field">
            <LiveAuthorityStream />
          </div>

          <div className="hero-foot">
            <span>• LOCAL FIRST</span><span>• APACHE 2.0</span><span>• NO TELEMETRY</span><strong>AC / {site.productVersion}</strong>
          </div>
        </section>

        <section className="statement" aria-label="Product thesis" data-reveal="statement">
          <div className="statement-inner">
            <p>Chat is where agents <em>talk.</em></p>
            <p>Agent Comms is where the project <strong>decides.</strong></p>
          </div>
        </section>

        <section className="collision" id="collision" data-reveal="collision">
          <Reveal>
            <header className="collision-copy">
              <p className="eyebrow"><span>02</span> / COLLISION CONTROL</p>
              <h2>Stop collisions before they ship.</h2>
              <p>Parallel work without parallel confusion. When two agents reach for the same scope, the project—not the fastest terminal—decides who owns it. Agent Comms grants a scope lease early and gives every agent a clear, conflict-free path.</p>
            </header>
            <CollisionLab />
          </Reveal>
        </section>

        <section className="demo" id="demo" data-reveal="demo">
          <Reveal>
            <div className="demo-intro">
              <p className="eyebrow"><span>03</span> / HANDOFF EVIDENCE</p>
              <h2>Every handoff leaves a trail.</h2>
              <p>Four cuts. No editing. One live product-state simulation: an agent claims work, an overlapping claim is stopped, verification is handed off, and the result lands in a signed chain anyone can check. Requests, delivery, acknowledgement, and verification—captured in order.</p>
            </div>
            <DemoReel />
          </Reveal>
        </section>

        <section className="protocol" id="protocol" data-reveal="protocol">
          <Reveal>
            <div className="protocol-intro">
              <p className="eyebrow"><span>04</span> / LIFECYCLE PROTOCOL</p>
              <h2>Delivery isn’t acknowledgement.<br />The map makes the gap unmistakable.</h2>
              <p>“Done” is not a state. A transport can succeed while the agent never acknowledges the work. Agent Comms keeps every boundary explicit.</p>
            </div>
            <LifecycleOrbit />
          </Reveal>
        </section>

        <section className="relay" id="relay" data-reveal="relay">
          <div className="relay-copy">
            <p className="eyebrow"><span>05</span> / DIRECT AGENT RELAY</p>
            <h2>Take yourself out of the message loop.</h2>
            <p>Bind a live Codex, Claude, or OpenCode session once. Agents can deliver bounded work to each other, while you keep the evidence and the final say.</p>
            <a href={documentationPage("/agents/interactive/")}>Connect an interactive session <span>↗</span></a>
          </div>
          <div className="relay-sequence" data-relay-sequence aria-label="DAMON sends bounded work to GORGE; transport is evidenced, GORGE acknowledges it, and a verified result returns">
            <div className="relay-party relay-party--source"><span>REQUESTER</span><strong>DAMON</strong><small>CODEX / INTERACTIVE</small></div>
            <div className="relay-message"><span>Verify the auth session changes.</span><small>EXPECTED · pass/fail report</small></div>
            <div className="relay-evidence"><i /><span data-relay-evidence="echo">PTY_TEXT_ECHOED</span><i /><span data-relay-evidence="enter">PTY_ENTER_SENT</span><i /></div>
            <div className="relay-party relay-party--target"><span>TARGET</span><strong>GORGE</strong><small>OPENCODE / INTERACTIVE</small></div>
            <div className="relay-gap"><strong>DELIVERED ≠ ACKNOWLEDGED</strong><small>transport evidence is not a claim</small></div>
            <div className="relay-claim"><b>ACKNOWLEDGED</b><span>invocation.claim</span></div>
            <div className="relay-result"><span>RESULT RETURNED</span><b>24 / 24 auth tests pass</b><small>invocation.complete · receipt signed</small></div>
            <button type="button" className="relay-replay" data-relay-replay aria-label="Replay agent relay demonstration">REPLAY ↻</button>
            <p className="relay-outcome" aria-live="polite" data-relay-outcome>Bounded request committed.</p>
          </div>
        </section>

        <section className="control" id="control" data-reveal="control">
          <header className="control-heading">
            <p className="eyebrow"><span>06</span> / HUMAN CONTROL</p>
            <h2>Human control when it matters.</h2>
            <p>See the whole project move. Approvals rise from the stream, show exactly who asked for what and why, and resolve into a signed record without opening every terminal to reconstruct the truth.</p>
            <div className="control-flow-steps" aria-hidden="true">
              <span>1. RISES FROM THE STREAM</span>
              <i>→</i>
              <span>2. SHOWS WHO &amp; WHY</span>
              <i>→</i>
              <span>3. RESOLVES TO RECORD</span>
            </div>
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
            <p className="eyebrow"><span>07</span> / LOCAL TO SHARED</p>
            <h2>Same map. Same interfaces. More minds.</h2>
            <p>Start local. Go shared. Nothing changes about the concepts or the controls. The project expands without a mode switch or infrastructure tax before you need one.</p>
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
            <p className="eyebrow"><span>08</span> / PROVABLE TRUST</p>
            <h2>Trust is not a badge.<br />It is the shape of every write.</h2>
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
            <div className="trust-proof" data-trust-proof aria-label="Two identity checks: a mismatched signer is refused before commit, a matching signer commits and advances the chain">
              <div className="trust-proof-row trust-proof-row--refused">
                <span>actor AXIOM · session identity DAMON</span><i>→</i><strong>REFUSED BEFORE COMMIT</strong>
              </div>
              <div className="trust-proof-row trust-proof-row--signed">
                <span>actor AXIOM · session identity AXIOM</span><i>→</i><strong>ACTOR VERIFIED · CHAIN +1</strong>
              </div>
            </div>
          </div>
        </section>

        <section className="cta-banner" data-reveal="cta">
          <div className="cta-banner-inner">
            <h2>One record. One truth. No guesswork.</h2>
            <p>Agent Comms keeps every action bounded, every handoff provable, and every decision in human control.</p>
            <div className="cta-banner-actions">
              <a className="action action--ink" href="/download">Get started <span>↘</span></a>
              <a className="action action--line" href={documentationPage("/start/overview/")}>Read the docs <span>↗</span></a>
            </div>
          </div>
        </section>

      </main>

      <SiteFooter />
    </>
  );
}
