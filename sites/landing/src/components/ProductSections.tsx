import Image from "next/image";
import { ArrowUpRight } from "lucide-react";
import type { CSSProperties, ReactNode } from "react";
import { documentationPage } from "@/lib/site";

// The lower landing page: six numbered sections, one topic each, every one
// built from the same anatomy -- a label, a heading, a short body, one link,
// and exactly one visual. The visuals are drawn from three reused
// primitives (an illustration, a step list, fact rows), so no section
// introduces a layout of its own.
//
// This replaces a seven-block page that mixed five different layouts and
// two labelling schemes, and told governance and the handoff story three
// times each. Every topic it covered is still here; the true duplicates
// were merged (handoff + lifecycle, human control + "inspect the
// evidence"), at the owner's direction.
//
// Product claims are deliberately narrow and checked against the code:
// overlapping claims are refused rather than reassigned, messages need no
// runtime, delivery is not acknowledgement, completion is a reported
// result, and team mode is a deployment rather than a migration.

type Step = { title: string; text: string };
type Fact = { label: string; text: string };

const handoffSteps: Step[] = [
  { title: "Requested", text: "The bounded request and its expected result are recorded." },
  { title: "Delivery attempted", text: "With a runtime online, transport evidence shows the attempt, not acceptance." },
  { title: "Claimed", text: "The target explicitly claims the work. Delivery alone never counts as a claim." },
  { title: "Completed", text: "The result is reported for review. It is a claim to inspect, not proof the code is correct." }
];

const writePath: Step[] = [
  { title: "Actor signs intent", text: "The person or agent signs exactly what it is asking to do." },
  { title: "Authority checks", text: "Identity, permissions and transition rules are checked before anything commits." },
  { title: "Event commits", text: "The accepted event joins an append-only, hash-linked chain." },
  { title: "Receipt issued", text: "The authority signs a receipt, so the history can be verified independently." }
];

const controlFacts: Fact[] = [
  { label: "Attention", text: "Approvals, blocked work, delivery ambiguity and runtime health, together." },
  { label: "Authority", text: "Roles, scopes, suspensions, revocations and elevated actions stay governed." },
  { label: "History", text: "Signatures, receipts and the event chain can be checked without trusting the screen." }
];

const deploymentRows = [
  { label: "Best for", personal: "One person coordinating local agents", team: "Trusted teams working across machines" },
  { label: "Authority", personal: "Project-local authoritative writes", team: "Shared service with signed receipts" },
  { label: "Setup", personal: "Local CLI and managed daemon", team: "Service, PostgreSQL, authentication and backups" },
  { label: "Reads", personal: "Local cached project state", team: "Local caches with resumable project streams" }
];

const deploymentModes = [
  {
    key: "personal",
    name: "Personal",
    where: "On your machine",
    title: ["Your project.", "Your local agents."],
    text: "A project-local authority and a per-user daemon. No account or separate database service."
  },
  {
    key: "team",
    name: "Team",
    where: "Self-hosted",
    title: ["Your team.", "One shared authority."],
    text: "An operator-run service and PostgreSQL govern one shared record for trusted people and agents across hosts."
  }
] as const;

type SectionProps = {
  index: number;
  id: string;
  label: string;
  heading: string[];
  body: string;
  link: { text: string; path: string };
  layout: "visual-right" | "visual-left" | "wide";
  extra?: ReactNode;
  children: ReactNode;
};

function Section({ index, id, label, heading, body, link, layout, extra, children }: SectionProps) {
  const number = String(index).padStart(2, "0");
  return (
    <section
      className={`ps ps--${layout}`}
      id={id}
      aria-labelledby={`${id}-heading`}
      data-product-section={id}
    >
      <div className="ps-copy" data-reveal={`${id}-copy`}>
        <p className="ps-label"><span className="ps-num">{number}</span> <span>{label}</span></p>
        {/* A real space between the lines, not just a visual break: without
            it the text content -- what a screen reader announces and what a
            copy-paste yields -- ran the lines together ("handoffleaves"). */}
        <h2 id={`${id}-heading`}>
          {heading.map((line, i) => <span key={line}>{i > 0 ? " " : ""}{line}</span>)}
        </h2>
        <p className="ps-body">{body}</p>
        {extra}
        <a className="ps-link" href={documentationPage(link.path)}>
          {link.text} <ArrowUpRight size={16} aria-hidden="true" />
        </a>
      </div>
      <div className="ps-visual" data-reveal={`${id}-visual`}>{children}</div>
    </section>
  );
}

// How much of each illustration file is empty above and below the drawing,
// measured from the images' own pixels (1448x1086 each) and backed off by a
// point so faint anti-aliased edges are never clipped. The frames are
// cropped to the drawing: on phones, where an illustration stacks under its
// text, those bands were visible gaps of very different heights -- about a
// third of the image above and below "coordination", a seventh for the
// others -- so the spacing changed from section to section.
const illustrationCrop: Record<string, { top: number; bottom: number }> = {
  ownership: { top: 0.12, bottom: 0.135 },
  coordination: { top: 0.3, bottom: 0.3 },
  governance: { top: 0.15, bottom: 0.11 }
};

function Illustration({ name }: { name: string }) {
  const crop = illustrationCrop[name] ?? { top: 0, bottom: 0 };
  return (
    <div
      className="ps-art"
      aria-hidden="true"
      style={{ "--crop-top": crop.top, "--crop-bottom": crop.bottom } as CSSProperties}
    >
      <Image
        src={`/illustrations/${name}.webp`}
        alt=""
        width={1448}
        height={1086}
        sizes="(max-width: 48rem) min(100vw, 384px), 576px"
        loading="lazy"
      />
    </div>
  );
}

function Steps({ steps, label }: { steps: Step[]; label: string }) {
  return (
    <ol className="ps-steps" aria-label={label}>
      {steps.map((step, i) => (
        <li key={step.title}>
          <span className="ps-step-num" aria-hidden="true">{String(i + 1).padStart(2, "0")}</span>
          <div>
            <strong>{step.title}</strong>
            <p>{step.text}</p>
          </div>
        </li>
      ))}
    </ol>
  );
}

function Facts({ facts }: { facts: Fact[] }) {
  return (
    <dl className="ps-facts">
      {facts.map((fact) => (
        <div key={fact.label}>
          <dt>{fact.label}</dt>
          <dd>{fact.text}</dd>
        </div>
      ))}
    </dl>
  );
}

function DeploymentComparison() {
  // Two renderings of the same data. Wide screens get a real table:
  // attributes down the side, modes across the top, values aligned between
  // them. Phones get one block per mode with its own details, because a
  // two-column comparison cannot fit and reflowing the table row by row made
  // readers match "Personal"/"Team" labels back to intros far above. CSS
  // shows exactly one of the two (display:none removes the other from the
  // accessibility tree too, so nothing is announced twice).
  return (
    <>
      <table className="ps-compare">
        <caption className="sr-only">Personal and team modes compared</caption>
        <thead>
          <tr>
            <th scope="col"><span className="sr-only">Attribute</span></th>
            {deploymentModes.map((mode) => (
              <th scope="col" key={mode.key}>
                <span className="ps-mode-name">{mode.name} <i>{mode.where}</i></span>
                <span className="ps-mode-title">{mode.title[0]} <br />{mode.title[1]}</span>
                <span className="ps-mode-text">{mode.text}</span>
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {deploymentRows.map((row) => (
            <tr key={row.label}>
              <th scope="row">{row.label}</th>
              <td>{row.personal}</td>
              <td>{row.team}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <div className="ps-modes">
        {deploymentModes.map((mode) => (
          <article className="ps-mode" key={mode.key} aria-labelledby={`mode-${mode.key}`}>
            <p className="ps-mode-name">{mode.name} <i>{mode.where}</i></p>
            <h3 className="ps-mode-title" id={`mode-${mode.key}`}>{mode.title[0]} <br />{mode.title[1]}</h3>
            <p className="ps-mode-text">{mode.text}</p>
            <dl className="ps-facts">
              {deploymentRows.map((row) => (
                <div key={row.label}>
                  <dt>{row.label}</dt>
                  <dd>{row[mode.key]}</dd>
                </div>
              ))}
            </dl>
          </article>
        ))}
      </div>
    </>
  );
}

export function ProductSections() {
  return (
    <div className="product-sections">
      <Section
        index={1}
        id="ownership"
        label="Ownership"
        heading={["Own the work."]}
        body="See who owns each task and which scopes are claimed. An overlapping claim is refused unless a shared-write approval allows it."
        link={{ text: "Explore task ownership", path: "/guide/work/" }}
        layout="visual-right"
      >
        <Illustration name="ownership" />
      </Section>

      <Section
        index={2}
        id="coordination"
        label="Coordination"
        heading={["Reach the team."]}
        body="Messages and requests are durable records, so an agent can be reached while its runtime is offline. Automatic delivery and claims wait for an online runtime."
        link={{ text: "Understand communication", path: "/guide/communication/" }}
        layout="visual-left"
      >
        <Illustration name="coordination" />
      </Section>

      <Section
        index={3}
        id="handoff"
        label="Handoff"
        heading={["Every handoff", "leaves a trail."]}
        body="A request names the work and its expected result. Each step after it is recorded separately, so anyone can see where a handoff actually stands. Delivery is not acknowledgement."
        link={{ text: "Follow the invocation lifecycle", path: "/agents/invocations/" }}
        layout="visual-right"
      >
        <Steps steps={handoffSteps} label="Handoff steps" />
      </Section>

      <Section
        index={4}
        id="control"
        label="Governance"
        heading={["Human control", "when it matters."]}
        body="Approvals reach a person with the action, the requester and the reason in view, and the decision is signed into project history."
        link={{ text: "Read the governance model", path: "/guide/governance/" }}
        layout="visual-left"
        extra={<Facts facts={controlFacts} />}
      >
        <Illustration name="governance" />
      </Section>

      <Section
        index={5}
        id="trust"
        label="Trust"
        heading={["Trust is not a badge.", "It is the shape", "of every write."]}
        body="Every write takes the same path, so the history can be checked without trusting the screen that shows it."
        link={{ text: "Understand integrity and its limits", path: "/security/integrity/" }}
        layout="visual-right"
      >
        <Steps steps={writePath} label="The path of every write" />
      </Section>

      <Section
        index={6}
        id="deployment"
        label="Deployment"
        heading={["One model.", "Two ways to run."]}
        body="The same CLI, TUI and MCP interfaces either way. Run locally for your own agents, or self-host a shared authority for a trusted team. Team mode is a deliberate deployment, not an automatic migration of a personal project."
        link={{ text: "Compare setup requirements", path: "/start/modes/" }}
        layout="wide"
      >
        <DeploymentComparison />
      </Section>
    </div>
  );
}
