const stageDetails = [
  {
    name: "REQUESTED",
    label: "signed obligation",
    description: "A bounded, actor-signed instruction now exists in the project record.",
    proves: "intent and obligation",
    excludes: "delivery or acknowledgement",
    event: "invocation.request"
  },
  {
    name: "DELIVERED",
    label: "transport evidence",
    description: "The selected transport acted and returned bounded delivery evidence.",
    proves: "connector execution",
    excludes: "semantic consumption",
    event: "invocation.notify"
  },
  {
    name: "CLAIMED",
    label: "target acknowledgement",
    description: "An eligible target runtime acknowledged the invocation and accepted ownership.",
    proves: "target acknowledgement",
    excludes: "work completion",
    event: "invocation.claim"
  },
  {
    name: "RUNNING",
    label: "active lease",
    description: "The target is actively executing the work under a renewable project lease.",
    proves: "active execution lease",
    excludes: "successful result",
    event: "invocation.start"
  },
  {
    name: "COMPLETED",
    label: "result committed",
    description: "The target committed its result and closed the governed obligation.",
    proves: "result and closure",
    excludes: "nothing earlier was skipped",
    event: "invocation.complete"
  }
] as const;

export function ProtocolInstrument() {
  const initialStage = stageDetails[0];

  return (
    <div className="protocol-instrument" data-protocol-instrument>
      <ol className="protocol-rail">
        {stageDetails.map((stage, index) => {
          const active = index === 0;
          return (
            <li key={stage.name}>
              <button
                type="button"
                className={active ? "is-active" : undefined}
                aria-pressed={active}
                data-stage={index}
                data-description={stage.description}
                data-proves={stage.proves}
                data-excludes={stage.excludes}
                data-event={stage.event}
              >
                <i /><span>{stage.name}</span><small>{stage.label}</small>
              </button>
            </li>
          );
        })}
      </ol>
      <div className="protocol-readout" aria-live="polite">
        <div className="readout-head"><span data-stage-sequence>01 / 05</span><strong data-stage-name>{initialStage.name}</strong></div>
        <p data-stage-description>{initialStage.description}</p>
        <dl>
          <div><dt>PROVES</dt><dd data-stage-proves>{initialStage.proves}</dd></div>
          <div><dt>DOES NOT PROVE</dt><dd data-stage-excludes>{initialStage.excludes}</dd></div>
        </dl>
        <div className="readout-event"><span>project.event</span><code data-stage-event>{initialStage.event}</code><i>VERIFIED</i></div>
      </div>
    </div>
  );
}
