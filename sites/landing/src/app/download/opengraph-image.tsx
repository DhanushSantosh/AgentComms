import { downloadRelease } from "@/lib/downloads";
import { ogContentType, ogSize, renderOgImage } from "@/lib/og";

export const size = ogSize;
export const contentType = ogContentType;
export const dynamic = "force-static";

export default function Image() {
  return renderOgImage(`Install Agent Comms ${downloadRelease.tag}`, "One command, no account, no cloud dependency");
}
