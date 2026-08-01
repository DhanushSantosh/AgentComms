import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

export const GET: APIRoute = async () => {
  const entries = await getCollection("docs");
  const ordered = entries.sort((left, right) => left.data.section.localeCompare(right.data.section) || left.data.order - right.data.order);
  const lines = [
    "# Agent Comms",
    "",
    "> Governed coordination for concurrent AI agents and the people directing them.",
    "",
    "The pages below document shipped product behavior. RFCs and backlog items are intentionally excluded.",
    ""
  ];
  for (const entry of ordered) {
    lines.push(`- [${entry.data.title}](/${entry.id}/): ${entry.data.description}`);
  }
  lines.push("", "## Full corpus", "", "- [llms-full.txt](/llms-full.txt)", "");
  return new Response(lines.join("\n"), { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};
