const workforce = [
  { signal: "online", agent: "OWNER", role: "OWNER", work: "reviewing approvals" },
  { signal: "online", agent: "AXIOM", role: "ORCHESTRATOR", work: "auth/session" },
  { signal: "online", agent: "DAMON", role: "Frontend-Architect", work: "test/auth" },
  { signal: "offline", agent: "GORGE", role: "Tester", work: "available" }
] as const;

const activity = [
  { seq: "0142", type: "agent.switch-role", actor: "DAMON · OWNER" },
  { seq: "0143", type: "task.claim", actor: "AXIOM · auth/session" },
  { seq: "0144", type: "invocation.request", actor: "OWNER · AXIOM" },
  { seq: "0145", type: "invocation.claim", actor: "AXIOM · AXIOM" },
  { seq: "0146", type: "approval.approve", actor: "OWNER · elevated" },
  { seq: "0147", type: "invocation.start", actor: "AXIOM · lease renewed" },
  { seq: "0148", type: "message.deliver", actor: "AXIOM → GORGE" },
  { seq: "0149", type: "invocation.complete", actor: "AXIOM · auth/session" }
] as const;

const roleClassName: Record<string, string> = {
  OWNER: "role-owner",
  ORCHESTRATOR: "role-orchestrator"
};

export function ControlRoomFrame() {
  return (
    <div className="tui-frame" data-tui-frame role="img" aria-label="Agent Comms control room: agent workforce, attention items, and signed live activity">
      <div className="tui-sidebar">
        <div className="tui-brand"><i /><span>AGENT COMMS</span></div>
        <small>ac-c940e6ee-1234…</small>
        <p>OPERATIONS</p>
        <ul>
          <li className="is-active">Command<i>└ Overview</i></li>
          <li>Work</li>
          <li>Team</li>
          <li>Relay</li>
          <li>Project</li>
        </ul>
      </div>
      <div className="tui-main">
        <div className="tui-tabs">
          <span><b data-tui-live /> Command / Overview · LOCAL · seq 146</span>
          <span className="tui-authority">authority owner</span>
        </div>
        <nav className="tui-tab-row">
          <span className="is-active">Overview</span>
          <span>My work</span>
          <span>Blockers</span>
          <span>Approvals</span>
        </nav>
        <h3>PROJECT CONTROL</h3>
        <p className="tui-stats">4 agents · 2 active tasks · 1 active invocation · 146 signed events</p>
        <div className="tui-panel-row">
          <div className="tui-panel tui-panel--workforce">
            <span>AGENT WORKFORCE</span>
            <small>signal / identity / role / current obligation</small>
            <table>
              <thead>
                <tr><th>SIGNAL</th><th>AGENT</th><th>ROLE</th><th>CURRENT WORK</th></tr>
              </thead>
              <tbody>
                {workforce.map((row) => (
                  <tr key={row.agent}>
                    <td><i className={row.signal === "online" ? "is-online" : undefined} />{row.signal === "online" ? "ONLINE" : "OFFLINE"}</td>
                    <td>{row.agent}</td>
                    <td className={roleClassName[row.role] ?? "role-custom"}>{row.role}</td>
                    <td>{row.work}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="tui-panel tui-panel--attention">
            <span>ATTENTION</span>
            <small>items requiring intervention</small>
            <p>→ approval-orchestrator-axiom <br />pending HUMAN-tier review</p>
          </div>
        </div>
        <div className="tui-panel tui-panel--activity">
          <span>LIVE ACTIVITY</span>
          <small>append-only, signed project history</small>
          <div className="tui-activity-window">
            {/* Two identical passes back to back inside one track, scrolled
                by exactly one pass's height on an infinite linear loop -- a
                real live-tail effect with no JS timer, matching this
                project's pure-CSS motion convention. The second pass is
                aria-hidden so assistive tech only ever hears the feed once. */}
            <div className="tui-activity-track">
              <ul data-tui-activity>
                {activity.map((row) => (
                  <li key={row.seq}><i>{row.seq}</i><b>{row.type}</b><em>{row.actor}</em></li>
                ))}
              </ul>
              <ul aria-hidden="true">
                {activity.map((row) => (
                  <li key={`${row.seq}-repeat`}><i>{row.seq}</i><b>{row.type}</b><em>{row.actor}</em></li>
                ))}
              </ul>
            </div>
          </div>
        </div>
        <div className="tui-footer">
          <span>[g] agents</span><span>[i] invocations</span><span>[n] create</span><span>[r] refresh</span><span>[/] commands</span>
        </div>
      </div>
    </div>
  );
}
