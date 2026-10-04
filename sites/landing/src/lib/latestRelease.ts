import { get } from "node:https";

const latestReleaseApiUrl = "https://api.github.com/repos/DhanushSantosh/AgentComms/releases/latest";
let latestVersionRequest: Promise<string> | undefined;

type GithubRelease = { tag_name: string };

// This site builds with output: "export" (pure static HTML, no server), so
// this fetch runs once per `next build` -- at whichever commit deploy-sites
// builds from -- not on a live schedule. That still fixes the real prior
// bug: next.config.ts used to derive the version from `git describe --tags`
// against the checked-out ref, which silently fails once a tag lives on a
// commit that isn't an ancestor of that ref (e.g. a release tag on main's
// merge commit isn't reachable from dev, so dev's build kept resolving the
// release before it). Reading GitHub's own "latest release" is correct
// regardless of which ref or branch topology the build runs from.
export function getLatestVersion(): Promise<string> {
  // Share one lookup per build worker, never across separate builds.
  return latestVersionRequest ??= loadLatestVersion();
}

async function loadLatestVersion(): Promise<string> {
  // Server-only build credential: never expose this through NEXT_PUBLIC_*.
  // CI runners share anonymous API limits, so authenticated builds are preferred.
  const token = process.env.AGENT_COMMS_RELEASE_API_TOKEN;
  // Native HTTPS is build-time I/O, not a Next dynamic fetch. This avoids
  // persisted fetch-cache staleness while retaining static image routes.
  const release = await new Promise<GithubRelease>((resolve, reject) => {
    const request = get(latestReleaseApiUrl, {
      headers: {
        Accept: "application/vnd.github+json", "User-Agent": "AgentComms-site-build",
        ...(token ? { Authorization: `Bearer ${token}` } : {})
      }
    }, (response) => {
      if (response.statusCode !== 200) {
        response.resume();
        reject(new Error(`GitHub releases API returned ${response.statusCode} for ${latestReleaseApiUrl}`));
        return;
      }
      let body = "";
      response.setEncoding("utf8");
      response.on("data", (chunk: string) => { body += chunk; });
      response.on("error", reject);
      response.on("end", () => {
        try { resolve(JSON.parse(body) as GithubRelease); } catch (error) { reject(error); }
      });
    });
    request.setTimeout(15_000, () => request.destroy(new Error("GitHub release lookup timed out")));
    request.on("error", reject);
  });
  if (typeof release.tag_name !== "string" || !/^v?\d+\.\d+\.\d+/.test(release.tag_name)) {
    throw new Error("GitHub releases API returned an invalid release tag");
  }
  return release.tag_name.replace(/^v/, "");
}
