import { createHash, randomUUID } from "node:crypto";
import { createRequire } from "node:module";
import { readFileSync, realpathSync, renameSync, statSync, unlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

export const upstreamHash = "ede1cc404a492fa348eb9d97a3007a0d72aa717bd22cd86a56bd0824c19729ca";
export const patchedHash = "9efa07719bad21230327f0c0a9de5a9cfee4fe695e68d80e749d26b15bff0c85";
export const insertion = readFileSync(new URL("../third_party/http-cache-semantics/reuse-guard.txt", import.meta.url), "utf8");
const needle = "        this._assertRequestHasHeaders(req);\n\n        // In all circumstances";
const digest = (source) => createHash("sha256").update(source).digest("hex");

function confinedFile(root, candidate) {
  const resolved = realpathSync(candidate);
  const relative = path.relative(root, resolved);
  if (path.isAbsolute(relative) || relative.startsWith(`..${path.sep}`) ||
      !relative.split(path.sep).includes("node_modules") || !statSync(resolved).isFile()) {
    throw new Error("cache-policy patch target must be a file inside this workspace's node_modules");
  }
  return resolved;
}

export function applyCachePolicyPatch(workspaceRoot) {
  const root = realpathSync(workspaceRoot);
  const require = createRequire(path.join(root, "package.json"));
  let sourcePath;
  try {
    sourcePath = confinedFile(root, require.resolve("http-cache-semantics"));
  } catch (cause) {
    throw new Error("Cannot patch cache policy: install all workspace dependencies with npm ci", { cause });
  }
  const docsRequire = createRequire(path.join(root, "sites", "docs", "package.json"));
  const astroRequire = createRequire(docsRequire.resolve("astro"));
  const consumerPath = confinedFile(root, astroRequire.resolve("http-cache-semantics"));
  if (consumerPath !== sourcePath) throw new Error("Astro resolves another cache-policy copy: review the dependency graph before patching");
  const manifest = confinedFile(root, path.join(path.dirname(sourcePath), "package.json"));
  const { name, version } = JSON.parse(readFileSync(manifest, "utf8"));
  if (name !== "http-cache-semantics" || version !== "4.3.0") {
    throw new Error("Unknown cache-policy version: review RFC 0043 before changing the patch pin");
  }
  const source = readFileSync(sourcePath, "utf8");
  const hash = digest(source);
  if (hash === patchedHash) return "already patched";
  if (hash !== upstreamHash) throw new Error("Unknown cache-policy source hash: refusing to modify dependency");
  const replacement = source.replace(needle, needle.replace("        // In all circumstances", `${insertion}        // In all circumstances`));
  if (digest(replacement) !== patchedHash) throw new Error("cache-policy patch output does not match its reviewed digest");
  const temporary = `${sourcePath}.agc-patch-${randomUUID()}`;
  try {
    writeFileSync(temporary, replacement, { flag: "wx", mode: statSync(sourcePath).mode & 0o777 });
    renameSync(temporary, sourcePath);
  } finally {
    try { unlinkSync(temporary); } catch (error) { if (error.code !== "ENOENT") throw error; }
  }
  return "patched";
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  try {
    console.log(`http-cache-semantics 4.3.0: ${applyCachePolicyPatch(root)} (RFC 0043)`);
  } catch (error) {
    console.error(error.message);
    if (error.cause) console.error(error.cause.message);
    process.exitCode = 1;
  }
}
