import { readdir, readFile, rm, writeFile } from "node:fs/promises";
import { extname, join, resolve } from "node:path";

const exportDirectory = resolve("dist");
const nextScriptPreloadPattern = /<link rel="preload" as="script"[^>]*\/>/g;
const nextChunkScriptPattern = /<script\b(?=[^>]*\bsrc="\/_next\/static\/chunks\/)[^>]*><\/script>/g;
const nextFlightScriptPattern = /<script>(?:\(self\.__next_f|self\.__next_f)[\s\S]*?<\/script>/g;

const exportedFiles = await walk(exportDirectory);
const htmlFiles = exportedFiles.filter((file) => extname(file) === ".html");

if (htmlFiles.length === 0) {
  throw new Error("Next.js export produced no HTML files to optimize.");
}

for (const htmlFile of htmlFiles) {
  const original = await readFile(htmlFile, "utf8");
  const optimized = original
    .replace(nextScriptPreloadPattern, "")
    .replace(nextChunkScriptPattern, "")
    .replace(nextFlightScriptPattern, "");

  if (optimized.includes("self.__next_f") || /src="\/_next\/static\/chunks\/[^"]+\.js"/.test(optimized)) {
    throw new Error(`Unsupported Next.js client runtime markup remains in ${htmlFile}.`);
  }
  if (!optimized.includes('href="/_next/static/chunks/') || !optimized.includes('rel="stylesheet"')) {
    throw new Error(`Next.js stylesheet reference was lost while optimizing ${htmlFile}.`);
  }
  await writeFile(htmlFile, optimized);
}

for (const exportedFile of exportedFiles) {
  const extension = extname(exportedFile);
  if (extension === ".js" && exportedFile.includes(`${join("_next", "static")}`)) {
    await rm(exportedFile);
  }
}

console.log(`Removed the unused Next.js client runtime from ${htmlFiles.length} static HTML files.`);

async function walk(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) files.push(...await walk(path));
    else if (entry.isFile()) files.push(path);
  }
  return files;
}
