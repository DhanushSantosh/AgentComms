export function CollisionLab() {
  return (
    <div className="collision-lab" data-collision-lab data-mode="ungoverned">
      <div className="lab-toolbar">
        <span>AUTH / SESSION</span>
        <div role="group" aria-label="Collision comparison">
          <button type="button" aria-pressed="true" data-collision-mode="ungoverned">Without authority</button>
          <button type="button" aria-pressed="false" data-collision-mode="governed">With Agent Comms</button>
        </div>
      </div>
      <div className="lab-canvas">
        <div className="code-track code-track--damon"><span>DAMON</span><b>internal/auth/session.go</b><i /></div>
        <div className="code-track code-track--axiom"><span>AXIOM</span><b>internal/auth/session.go</b><i /></div>
        <div className="collision-zone"><span data-collision-state>COLLISION</span><b /></div>
        <div className="governed-result">
          <span>DAMON</span><strong>LEASE ACTIVE</strong><small>AXIOM receives STALE_PRECONDITION before writing.</small>
        </div>
      </div>
      <div className="lab-proof"><span>scope overlap</span><i>→</i><span>transaction check</span><i>→</i><strong data-proof-outcome>two writers</strong></div>
    </div>
  );
}
