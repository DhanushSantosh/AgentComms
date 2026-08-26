const scenes = [
  { step: "01", tag: "REQUEST", title: "Bounded intent", event: "invocation.request", proof: "Actor-signed · scope auth/session", caption: "REVIEWER commits a bounded verification request to the project record." },
  { step: "02", tag: "DELIVERY", title: "Transport evidence", event: "transport.delivered", proof: "PTY text echoed · enter sent", caption: "The transport succeeds—but delivery evidence is not acknowledgement." },
  { step: "03", tag: "AGENT ACK", title: "Target accepts", event: "invocation.claim", proof: "DEVELOPER acknowledged · lease active", caption: "DEVELOPER explicitly accepts the obligation and begins the verification scope." },
  { step: "04", tag: "RESULT", title: "Verified result", event: "invocation.complete", proof: "24 / 24 tests pass · receipt signed", caption: "The verified result returns and closes the signed handoff chain." }
] as const;

export function DemoReel() {
  return (
    <div className="demo-reel evidence-film" data-demo-reel data-scene="0">
      <div className="evidence-film-label"><span>HANDOFF EVIDENCE</span><span>ONE STORY · FOUR RECORDED FACTS</span></div>
      <div className="evidence-film-track">
        <div className="evidence-film-spine" aria-hidden="true" />
        {scenes.map((scene, index) => (
          <div className="evidence-film-cut" key={scene.step}>
            <button type="button" className="evidence-film-frame" data-reel-scene={index} data-reel-select={index} aria-pressed={index === 0}>
              <span className="evidence-film-time">T+0{index === 0 ? "0:00" : index === 1 ? "0:06" : index === 2 ? "7:41" : "7:55"}</span>
              <span className="evidence-film-index">{scene.step}<i />{scene.tag}</span>
              <span className="evidence-film-art" aria-hidden="true"><i /><i /><i /></span>
              <span className="evidence-film-copy"><small>{scene.event}</small><strong>{scene.title}</strong><b>{scene.proof}</b></span>
            </button>
            {index === 1 && <div className="evidence-film-gap"><span>WAITING FOR ACKNOWLEDGEMENT</span><strong>DELIVERED ≠ ACKNOWLEDGED</strong></div>}
          </div>
        ))}
      </div>
      <div className="reel-foot">
        <div className="evidence-film-progress" aria-hidden="true"><i /></div>
        <button type="button" className="reel-replay" data-reel-replay aria-label="Replay handoff evidence film">PLAY FILM ↻</button>
        <p tabIndex={-1} aria-live="polite" data-reel-live>{scenes[0].caption}</p>
        <div hidden aria-hidden="true">{scenes.map((scene, index) => <span data-reel-caption-source={index} key={scene.step}>{scene.caption}</span>)}</div>
      </div>
    </div>
  );
}
