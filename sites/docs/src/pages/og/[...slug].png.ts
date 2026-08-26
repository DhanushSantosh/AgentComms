import type { APIRoute } from "astro";
import { getCollection } from "astro:content";
import { renderOgImage } from "@lib/og";

export const prerender = true;

export async function getStaticPaths() {
  const entries = await getCollection("docs");
  const docPaths = entries.map((entry) => ({
    params: { slug: entry.id },
    props: { title: entry.data.title, kicker: entry.data.section }
  }));
  return [
    { params: { slug: "home" }, props: { title: "Know who owns the work. Prove what happened next.", kicker: "The coordination manual" } },
    ...docPaths
  ];
}

export const GET: APIRoute = async ({ props }) => {
  const { title, kicker } = props as { title: string; kicker: string };
  const png = await renderOgImage(title, kicker);
  return new Response(new Uint8Array(png), {
    headers: { "Content-Type": "image/png", "Cache-Control": "public, max-age=31536000, immutable" }
  });
};
