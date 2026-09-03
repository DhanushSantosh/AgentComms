import { site } from "@/lib/site";

const repositoryUrl = "https://github.com/DhanushSantosh/AgentComms";
const releaseTag = `v${site.productVersion}`;
const repositoryRawUrl = `https://raw.githubusercontent.com/DhanushSantosh/AgentComms/${releaseTag}`;
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
    requirements: "curl · Python 3",
    command: `curl -fsSL ${repositoryRawUrl}/install.sh | AGENT_COMMS_VERSION=${releaseTag} sh`
  },
  {
    id: "windows",
    name: "Windows",
    environment: "PowerShell",
    requirements: "PowerShell 7",
    command: `Invoke-WebRequest ${repositoryRawUrl}/install.ps1 -OutFile install.ps1\n.\\install.ps1 -Version ${releaseTag}`
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
  installerStatus: "Verified release",
  installerDetail: `${releaseTag}'s tag pins the installer and verifier digest. Both installers authenticate the verifier before it checks the signed CLI bundle.`
} as const;

// A separate, unstable channel from the release above: builds from dev's
// latest commit daily, for developers -- not Beta, not a numbered version,
// not installed by install.sh/install.ps1.
export const nightlyBuild = {
  command: "oras pull ghcr.io/dhanushsantosh/agentcomms-nightly:latest"
} as const;
