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
      <svg className="field-path" viewBox="0 0 760 620" preserveAspectRatio="none" aria-hidden="true">
        <path className="path path--damon" d="M92 138 C210 138, 218 246, 354 246" />
        <path className="path path--axiom" d="M664 132 C554 132, 526 246, 406 246" />
        <path className="path path--gorge" d="M654 485 C532 485, 518 392, 404 392" />
        <path className="path path--authority" d="M380 271 L380 535" />
      </svg>
      <div className="authority-core"><span>AUTHORITY</span><b>conflict checked</b><i>receipt signed</i></div>
      <div className="receipt-ticket"><span>EVENT / invocation.claim</span><strong>9f2a…e8c1</strong><small>ACTOR GORGE · SCOPE test/auth</small></div>
      <div className="field-status"><span><i /> 3 agents online</span><span>chain verified</span></div>
    </div>
  );
}
