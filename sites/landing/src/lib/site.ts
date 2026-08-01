const fallbackMarketingSiteUrl = "https://agentcomms.dev";
const fallbackDocumentationUrl = "https://docs.agentcomms.dev";
const productVersion = process.env.NEXT_PUBLIC_PRODUCT_VERSION;

if (!productVersion) {
  throw new Error("Landing runtime metadata requires NEXT_PUBLIC_PRODUCT_VERSION.");
}

export const site = {
  documentationUrl: process.env.NEXT_PUBLIC_DOCS_URL ?? fallbackDocumentationUrl,
  marketingSiteUrl: process.env.NEXT_PUBLIC_SITE_URL ?? fallbackMarketingSiteUrl,
  productVersion
} as const;

export function documentationPage(path: string): string {
  return new URL(path, `${site.documentationUrl}/`).toString();
}
