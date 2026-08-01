const channel = import.meta.env.PUBLIC_DOCS_CHANNEL;
const sourceBranch = channel === "stable" ? "main" : "dev";

export const product = {
  name: "Agent Comms",
  description: "Governed coordination for concurrent AI agents and the people directing them.",
  repository: "https://github.com/DhanushSantosh/AgentComms",
  editBase: `https://github.com/DhanushSantosh/AgentComms/edit/${sourceBranch}/docs/site`,
  issueBase: "https://github.com/DhanushSantosh/AgentComms/issues/new",
  version: import.meta.env.PUBLIC_PRODUCT_VERSION,
  channel
} as const;
