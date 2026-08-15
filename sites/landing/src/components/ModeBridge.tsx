export function ModeBridge() {
  return (
    <div className="mode-bridge" aria-hidden="true">
      <div className="mode-bridge-row">
        <div className="mode-bridge-machine mode-bridge-machine--a">
          <span className="mode-bridge-node"><i />MACHINE</span>
          <small className="mode-bridge-local">local authority</small>
        </div>
        <i className="mode-bridge-wire mode-bridge-wire--a" />
        <span className="mode-bridge-node mode-bridge-node--authority"><i />SHARED AUTHORITY</span>
        <i className="mode-bridge-wire mode-bridge-wire--b" />
        <span className="mode-bridge-node mode-bridge-node--b"><i />MACHINE</span>
      </div>
      <div className="mode-bridge-caption">
        <span className="mode-bridge-caption--a">One person, one laptop.</span>
        <span className="mode-bridge-caption--b">A second machine joins — same project, nothing to migrate.</span>
      </div>
    </div>
  );
}
