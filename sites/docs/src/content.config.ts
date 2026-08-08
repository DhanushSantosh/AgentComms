import { defineCollection } from "astro:content";
import { z } from "astro/zod";
import { glob } from "astro/loaders";

const docs = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "../../docs/site" }),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    section: z.string(),
    order: z.number().int().nonnegative(),
    audience: z.enum(["Everyone", "Human operators", "Agents", "Operators", "Security reviewers"]),
    template: z.enum(["article", "cli-reference", "mcp-reference"]).default("article"),
    lastVerified: z.coerce.date(),
    next: z.string().optional(),
    previous: z.string().optional(),
    related: z.array(z.string()).default([])
  })
});

export const collections = { docs };
