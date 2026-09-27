import { getDownloadData } from "@/lib/downloads";
import { ogContentType, ogSize, renderOgImage } from "@/lib/og";

export const size = ogSize;
export const contentType = ogContentType;
export const dynamic = "force-static";

export default async function Image() {
  const { downloadRelease } = await getDownloadData();
  return renderOgImage(`Install Agent Comms ${downloadRelease.tag}`, "One command, no account, no cloud dependency");
}
