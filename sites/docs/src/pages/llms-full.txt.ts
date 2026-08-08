import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

export const GET: APIRoute = async () => {
  const entries = await getCollection("docs");
  const ordered = entries.sort((left, right) => left.data.section.localeCompare(right.data.section) || left.data.order - right.data.order);
  const sections = ordered.map((entry) => `# ${entry.data.title}\n\n${entry.data.description}\n\n${(entry.body ?? "").trim()}`);
  return new Response(`# Agent Comms documentation\n\n${sections.join("\n\n---\n\n")}\n`, {
    headers: { "Content-Type": "text/plain; charset=utf-8" }
  });
};
