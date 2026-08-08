import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// import.meta.dirname doesn't survive Next's build-time bundling here (unlike
// next.config.ts, which Node loads directly, unbundled) -- process.cwd() is
// reliable instead, since every landing build/dev script runs from this
// package's own directory (npm --workspace @agent-comms/landing).
const repositoryRoot = resolve(/* turbopackIgnore: true */ process.cwd(), "../..");

export function readRepositoryFile(relativePath: string): string {
  return readFileSync(resolve(repositoryRoot, relativePath), "utf8");
}
