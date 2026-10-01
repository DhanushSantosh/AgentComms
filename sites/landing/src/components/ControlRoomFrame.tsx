// A recording of the real current TUI, not a hand-maintained recreation.
// The desktop interactive WASM demo remains separate in LiveControlRoom.
export function ControlRoomFrame() {
  return (
    <div className="tui-recording" data-tui-recording>
      <video
        controls
        playsInline
        preload="none"
        poster="/media/tui-overview.png"
        aria-label="Recorded Agent Comms terminal tour: overview, agents, invocations, and approval inspection"
        aria-describedby="tui-recording-description"
      >
        <source src="/media/tui-demo.mp4" type="video/mp4" />
        <a href="/media/tui-demo.mp4">Download the terminal tour</a>
      </video>
      <p id="tui-recording-description">
        Real TUI · isolated demo project · navigation pauses shortened.
        Messaging readiness and runtime presence are separate.
        No audio; the overview, agent list, invocation timeline, and approval inspector are shown.
      </p>
    </div>
  );
}
