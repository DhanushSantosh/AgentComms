import { site } from "@/lib/site";

const repositoryUrl = "https://github.com/DhanushSantosh/AgentComms";
const repositoryRawUrl = "https://raw.githubusercontent.com/DhanushSantosh/AgentComms/main";
const releaseTag = `v${site.productVersion}`;
const releaseBaseUrl = `${repositoryUrl}/releases/download/${releaseTag}`;

export type InstallerMethod = {
  id: "unix" | "windows";
  name: string;
  environment: string;
  requirements: string;
  command: string;
};

export const installerMethods: readonly InstallerMethod[] = [
  {
    id: "unix",
    name: "Linux + macOS",
    environment: "Terminal",
    requirements: "curl · Python 3 · Cosign",
    command: `curl -fsSL ${repositoryRawUrl}/install.sh | sh`
  },
  {
    id: "windows",
    name: "Windows",
    environment: "PowerShell",
    requirements: "PowerShell 7 · Cosign",
    command: `Invoke-WebRequest ${repositoryRawUrl}/install.ps1 -OutFile install.ps1\n.\\install.ps1`
  }
] as const;

// Before v1, every release is beta-maturity, not "Stable" -- SemVer's own
// 0.x.y convention means anything may still change without notice.
const releaseChannel = site.productVersion.split(".")[0] === "0" ? "Beta" : "Stable";

export const downloadRelease = {
  version: site.productVersion,
  tag: releaseTag,
  channel: releaseChannel,
  releaseUrl: `${repositoryUrl}/releases/tag/${releaseTag}`,
  allReleasesUrl: `${repositoryUrl}/releases`,
  checksumsUrl: `${releaseBaseUrl}/checksums.txt`,
  sourceUrl: `${repositoryUrl}/tree/${releaseTag}`,
  installerStatus: "Verification assets incomplete",
  installerDetail: `The ${releaseTag} CLI Cosign bundles required by both official installers are not published yet. The commands below are the supported install path, but they intentionally fail closed until those verification assets are restored.`
} as const;
