const latestReleaseApiUrl = "https://api.github.com/repos/DhanushSantosh/AgentComms/releases/latest";

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
export async function getLatestVersion(): Promise<string> {
  const response = await fetch(latestReleaseApiUrl, {
    headers: { Accept: "application/vnd.github+json" }
  });

  if (!response.ok) {
    throw new Error(`GitHub releases API returned ${response.status} for ${latestReleaseApiUrl}`);
  }

  const release = (await response.json()) as GithubRelease;
  return release.tag_name.replace(/^v/, "");
}
