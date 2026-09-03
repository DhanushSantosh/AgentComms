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

// Not a fourth installer -- a distinct path for contributors who want dev's
// current tip rather than a signed release, shown alongside the installers
// with its own commands inline rather than only a link out to CONTRIBUTING.md.
export const buildFromSourceMethod = {
  id: "source" as const,
  name: "Build from source",
  environment: "Terminal · dev's tip",
  requirements: "git · Go (see go.mod)",
  command: `git clone ${repositoryUrl}.git\ncd AgentComms\ngo build -o ./bin/agent-comms ./cmd/agent-comms\n./bin/agent-comms version`,
  detailUrl: `${repositoryUrl}/blob/main/CONTRIBUTING.md#build-from-source`
} as const;

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
