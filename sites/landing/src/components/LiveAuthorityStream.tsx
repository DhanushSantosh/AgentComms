const streamEvents = [
  { seq: "0142", type: "agent.switch-role", detail: "DAMON → OWNER" },
  { seq: "0143", type: "task.claim", detail: "AXIOM · auth/session" },
  { seq: "0144", type: "invocation.request", detail: "OWNER → AXIOM" },
  { seq: "0145", type: "invocation.claim", detail: "AXIOM · AXIOM" },
  { seq: "0146", type: "approval.approve", detail: "OWNER · elevated" },
  { seq: "0147", type: "invocation.start", detail: "AXIOM · lease renewed" },
  { seq: "0148", type: "message.deliver", detail: "AXIOM → GORGE" },
  { seq: "0149", type: "invocation.complete", detail: "AXIOM · auth/session" }
] as const;

export function LiveAuthorityStream() {
  return (
    <div
      className="authority-stream"
      role="img"
      aria-label="A live stream of this project's signed events: agents switching roles, claiming work, requesting and completing invocations, all recorded in order"
    >
      <div className="authority-stream-head">
        <span>agent-comms — live authority</span>
      </div>
      <div className="authority-stream-window">
        {/* Two identical passes back to back, scrolled by exactly one
            pass's height on an infinite loop -- the same live-tail
            technique the control room's activity panel uses. The second
            pass is aria-hidden so assistive tech only hears it once. */}
        <div className="authority-stream-track">
          <ol>
            {streamEvents.map((event) => (
              <li key={event.seq}><i>{event.seq}</i><b>{event.type}</b><em>{event.detail}</em></li>
            ))}
          </ol>
          <ol aria-hidden="true">
            {streamEvents.map((event) => (
              <li key={`${event.seq}-repeat`}><i>{event.seq}</i><b>{event.type}</b><em>{event.detail}</em></li>
            ))}
          </ol>
        </div>
      </div>
      <div className="authority-stream-foot">
        <span>✓ 146 events</span>
        <span>chain verified</span>
      </div>
    </div>
  );
}
