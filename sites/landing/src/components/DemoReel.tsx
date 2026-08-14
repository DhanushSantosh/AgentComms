const scenes = [
  {
    step: "01",
    tag: "CLAIM",
    caption: "An agent claims work under a lease.",
    content: (
      <div className="reel-claim">
        <div className="reel-agent"><i />DAMON</div>
        <div className="reel-arrow">claims →</div>
        <div className="reel-lease"><span>task/auth-session</span><b>LEASE ACTIVE · 4h</b></div>
      </div>
    )
  },
  {
    step: "02",
    tag: "COLLIDE",
    caption: "A second agent reaches for the same scope — the project decides, not the fastest terminal.",
    content: (
      <div className="reel-collide">
        <div className="reel-agent"><i />AXIOM</div>
        <div className="reel-zone"><span>STALE_PRECONDITION</span></div>
        <div className="reel-agent reel-agent--owner"><i />DAMON<b>OWNER</b></div>
      </div>
    )
  },
  {
    step: "03",
    tag: "ELEVATE",
    caption: "A sensitive action still needs a human, typing an elevated passphrase, physically present.",
    content: (
      <div className="reel-elevate">
        <div className="reel-passphrase"><span>Elevated-key passphrase</span><b>••••••••••••</b></div>
        <div className="reel-stamp">OWNER APPROVED · agent.switch-role → ORCHESTRATOR</div>
      </div>
    )
  },
  {
    step: "04",
    tag: "VERIFY",
    caption: "Every write lands in one signed, append-only chain — checkable without trusting the screen.",
    content: (
      <div className="reel-verify">
        <div className="reel-chain">
          <span>0144</span><span>0145</span><span className="is-active">0146</span>
        </div>
        <div className="reel-receipt"><b>c3283c…6cdc9</b><small>RECEIPT SIGNED · CHAIN VERIFIED</small></div>
      </div>
    )
  }
] as const;

export function DemoReel() {
  return (
    <div className="demo-reel" data-demo-reel>
      <div className="reel-chrome"><span>AGENT COMMS / WALKTHROUGH</span><span>4 CUTS · NO EDITING</span></div>
      <div className="reel-stage">
        {scenes.map((scene, index) => (
          <div className="reel-scene" data-reel-scene={index} key={scene.step}>
            <div className="reel-tag"><b>{scene.step}</b>{scene.tag}</div>
            {scene.content}
          </div>
        ))}
      </div>
      <div className="reel-foot">
        <div className="reel-dots" aria-hidden="true">
          {scenes.map((scene, index) => (
            <i data-reel-dot={index} key={scene.step} />
          ))}
        </div>
        <p aria-live="polite">
          {scenes.map((scene, index) => (
            <span data-reel-caption={index} key={scene.step}>{scene.caption}</span>
          ))}
        </p>
      </div>
    </div>
  );
}
