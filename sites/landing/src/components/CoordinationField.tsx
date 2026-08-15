export function CoordinationField() {
  return (
    <div
      className="coordination-field"
      data-coordination-field
      role="img"
      aria-label="DAMON, AXIOM, and GORGE coordinate scopes through one project authority"
    >
      <div className="field-grid" aria-hidden="true" />
      <div className="field-label field-label--top"><span>LIVE PROJECT FIELD</span><span>SEQ 00142</span></div>
      <div className="scope scope--auth"><span>scope</span><strong>auth/session</strong><i>OWNED</i></div>
      <div className="scope scope--test"><span>scope</span><strong>test/auth</strong><i>AVAILABLE</i></div>
      <div className="agent agent--damon"><b /><span>DAMON</span><small>RUNNING</small></div>
      <div className="agent agent--axiom"><b /><span>AXIOM</span><small>WAITING</small></div>
      <div className="agent agent--gorge"><b /><span>GORGE</span><small>CLAIMED</small></div>
      {/* Right-angle routing, not organic curves -- reads as protocol
          traces on a schematic rather than a generic network diagram. */}
      <svg className="field-path" viewBox="0 0 760 620" preserveAspectRatio="none" aria-hidden="true">
        <path className="path path--damon" d="M92 138 H228 V246 H354" />
        <path className="path path--axiom" d="M664 132 H538 V246 H406" />
        <path className="path path--gorge" d="M654 485 V392 H404" />
        <path className="path path--authority" d="M380 271 V535" />
      </svg>
      <div className="authority-core"><span>AUTHORITY</span><b>conflict checked</b><i>receipt signed</i></div>
      <div className="receipt-ticket"><span>EVENT / invocation.claim</span><strong>9f2a…e8c1</strong><small>ACTOR GORGE · SCOPE test/auth</small></div>
      <div className="field-status"><span><i /> 3 agents online</span><span>chain verified</span></div>
    </div>
  );
}
