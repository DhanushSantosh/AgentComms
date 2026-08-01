import type { MetadataRoute } from "next";
import { site } from "@/lib/site";

export const dynamic = "force-static";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: site.marketingSiteUrl, changeFrequency: "weekly", priority: 1 },
    { url: new URL("/download", site.marketingSiteUrl).toString(), changeFrequency: "weekly", priority: 0.9 }
  ];
}
