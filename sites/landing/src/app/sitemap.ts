import type { MetadataRoute } from "next";
import { site } from "@/lib/site";

export const dynamic = "force-static";

export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: site.marketingSiteUrl, changeFrequency: "weekly", priority: 1 },
    { url: new URL("/download", site.marketingSiteUrl).toString(), changeFrequency: "weekly", priority: 0.9 },
    { url: new URL("/releases", site.marketingSiteUrl).toString(), changeFrequency: "weekly", priority: 0.7 },
    { url: new URL("/security", site.marketingSiteUrl).toString(), changeFrequency: "monthly", priority: 0.5 },
    { url: new URL("/license", site.marketingSiteUrl).toString(), changeFrequency: "monthly", priority: 0.4 },
    { url: new URL("/support", site.marketingSiteUrl).toString(), changeFrequency: "monthly", priority: 0.5 },
    { url: new URL("/privacy", site.marketingSiteUrl).toString(), changeFrequency: "monthly", priority: 0.3 }
  ];
}
