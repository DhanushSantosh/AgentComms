const interfaces = ["CLI", "TUI", "MCP"];

function InterfaceLedger({ label }: { label: string }) {
  return (
    <div className="continuity-ledger">
      <span>{label}</span>
      {interfaces.map((name) => <div key={name}><b>{name}</b><small>{name === "CLI" ? "agentcomms" : name === "TUI" ? "control room" : "model context protocol"}</small></div>)}
    </div>
  );
}

function MiniMap({ shared, agent }: { shared?: boolean; agent?: string }) {
  return (
    <div className={`mode-map ${shared ? "mode-map--shared" : ""}`}>
      <i className="mode-map-contour mode-map-contour--1" /><i className="mode-map-contour mode-map-contour--2" /><i className="mode-map-contour mode-map-contour--3" />
      <span className="mode-map-scope">auth/session</span>
      <span className="mode-map-cursor mode-map-cursor--a">DEVELOPER</span>
      <span className="mode-map-cursor mode-map-cursor--b">{agent ?? "REVIEWER"}</span>
    </div>
  );
}

export function ModeBridge() {
  return (
    <div className="mode-bridge continuity-map" role="img" aria-label="The same project map expands from one local machine to two shared machines while CLI, TUI, and MCP remain unchanged">
      <div className="continuity-heading"><span>LOCAL<small>one machine</small></span><i>→</i><span>SHARED<small>two machines</small></span></div>
      <div className="continuity-topology">
        <div className="continuity-machine continuity-machine--local"><b>MACHINE A</b><MiniMap /></div>
        <div className="continuity-arrow"><span>same project model</span><i /></div>
        <div className="continuity-shared">
          <div className="continuity-machine"><b>MACHINE A · OWNER</b><MiniMap shared /></div>
          <div className="continuity-machine continuity-machine--remote"><b>MACHINE B · PEER</b><MiniMap shared agent="REVIEWER" /></div>
        </div>
      </div>
      <div className="continuity-ledgers">
        <InterfaceLedger label="LOCAL INTERFACES" />
        <i aria-hidden="true" />
        <InterfaceLedger label="SHARED INTERFACES · UNCHANGED" />
      </div>
      <p>Local authority becomes shared authority. The concepts and controls stay familiar.</p>
    </div>
  );
}
