import type { APIRoute } from "astro";
import { getCollection } from "astro:content";

export async function getStaticPaths() {
  const entries = await getCollection("docs");
  return entries.map((entry) => ({ params: { slug: entry.id }, props: { entry } }));
}

export const GET: APIRoute = ({ props }) => new Response(props.entry.body, {
  headers: { "Content-Type": "text/markdown; charset=utf-8" }
});
