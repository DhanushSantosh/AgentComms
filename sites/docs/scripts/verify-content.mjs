import { access, readFile, readdir } from "node:fs/promises";
import { dirname, extname, join, relative, resolve } from "node:path";

const siteRoot = resolve(import.meta.dirname, "..");
const repositoryRoot = resolve(siteRoot, "../..");
const contentRoot = join(repositoryRoot, "docs/site");
const distRoot = join(siteRoot, "dist");
const requiredOutputs = [
  "index.html", "404.html", "llms.txt", "llms-full.txt", "sitemap-index.xml",
  "start/quickstart/index.html", "agents/invocations/index.html", "reference/cli/index.html", "reference/mcp/index.html"
];
const forbiddenPatterns = [
  /first-class agent-spawns-agent/i,
  /agent\.spawn/i,
  /legacy migration/i
];

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const paths = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) paths.push(...await walk(path));
    else paths.push(path);
  }
  return paths;
}

for (const output of requiredOutputs) await access(join(distRoot, output));

const contentFiles = (await walk(contentRoot)).filter((path) => /\.mdx?$/.test(path));
if (contentFiles.length < 25) throw new Error(`Expected a complete documentation set; found ${contentFiles.length} pages.`);

for (const path of contentFiles) {
  const body = await readFile(path, "utf8");
  for (const pattern of forbiddenPatterns) {
    if (pattern.test(body)) throw new Error(`${relative(repositoryRoot, path)} exposes deferred or removed behavior: ${pattern}`);
  }
  for (const field of ["title", "description", "section", "audience", "lastVerified"]) {
    if (!new RegExp(`^${field}:`, "m").test(body)) throw new Error(`${relative(repositoryRoot, path)} is missing ${field} metadata.`);
  }
}

const reference = JSON.parse(await readFile(join(siteRoot, "src/generated/reference.json"), "utf8"));
if (reference.commands.length < 50) throw new Error("Generated CLI reference is incomplete.");
if (reference.mcp_tools.length < 20) throw new Error("Generated MCP reference is incomplete.");

const htmlFiles = (await walk(distRoot)).filter((path) => path.endsWith(".html"));
for (const sourcePath of htmlFiles) {
  const body = await readFile(sourcePath, "utf8");
  for (const match of body.matchAll(/\bsrc="([^"]+)"/g)) {
    const source = match[1];
    if (/^(https?:|data:)/.test(source)) continue;
    const sourceTarget = source.startsWith("/") ? join(distRoot, source) : resolve(dirname(sourcePath), source);
    try {
      await access(sourceTarget);
    } catch {
      throw new Error(`${relative(distRoot, sourcePath)} loads missing ${source}`);
    }
  }
  for (const match of body.matchAll(/href="([^"]+)"/g)) {
    const href = match[1];
    if (/^(https?:|mailto:|tel:)/.test(href) || href === "#" || href.includes("${")) continue;
    const [pathPart, anchor] = href.split("#", 2);
    let targetPath = pathPart.startsWith("/") ? join(distRoot, pathPart) : resolve(dirname(sourcePath), pathPart);
    if (pathPart === "" && anchor) targetPath = sourcePath;
    if (targetPath.endsWith("/")) targetPath = join(targetPath, "index.html");
    else if (!extname(targetPath)) targetPath = join(targetPath, "index.html");
    try {
      await access(targetPath);
    } catch {
      throw new Error(`${relative(distRoot, sourcePath)} links to missing ${href}`);
    }
    if (anchor) {
      const targetBody = await readFile(targetPath, "utf8");
      if (!targetBody.includes(`id="${decodeURIComponent(anchor)}"`)) {
        throw new Error(`${relative(distRoot, sourcePath)} links to missing anchor ${href}`);
      }
    }
  }
}

console.log(`Verified ${contentFiles.length} product pages, ${htmlFiles.length} rendered pages, ${reference.commands.length} commands, and ${reference.mcp_tools.length} MCP tools.`);
