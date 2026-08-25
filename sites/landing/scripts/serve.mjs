import { createReadStream } from "node:fs";
import { open, stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, resolve, sep } from "node:path";
import { createGzip } from "node:zlib";

const arguments_ = process.argv.slice(2);
const portIndex = arguments_.indexOf("--port");
const hostIndex = arguments_.indexOf("--host");
const defaultPort = 4333;
const port = Number(portIndex >= 0 ? arguments_[portIndex + 1] : defaultPort);
const host = hostIndex >= 0 ? arguments_[hostIndex + 1] : "127.0.0.1";
const root = resolve("dist");
const contentTypes = {
  ".css": "text/css; charset=utf-8",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".wasm": "application/wasm",
  ".woff2": "font/woff2",
  ".xml": "application/xml; charset=utf-8"
};
const compressibleExtensions = new Set([".css", ".html", ".js", ".svg", ".xml"]);

// Next's app-router file conventions (opengraph-image.tsx, robots.ts,
// sitemap.ts) emit their static export output with no file extension at all
// (dist/opengraph-image, dist/download/opengraph-image, ...) -- extname()
// alone can't classify those, so they'd otherwise fall through to
// application/octet-stream. Sniffing the real magic bytes handles this
// generically rather than hardcoding every route's own path.
async function sniffContentType(path) {
  const handle = await open(path, "r");
  try {
    const buffer = Buffer.alloc(8);
    await handle.read(buffer, 0, 8, 0);
    if (buffer.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return "image/png";
    if (buffer.subarray(0, 2).equals(Buffer.from([0xff, 0xd8]))) return "image/jpeg";
    return undefined;
  } finally {
    await handle.close();
  }
}

const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url ?? "/", `http://${request.headers.host ?? host}`);
    const decodedPath = decodeURIComponent(url.pathname);
    let path = resolve(root, `.${decodedPath}`);
    if (path !== root && !path.startsWith(`${root}${sep}`)) {
      response.writeHead(400).end("Invalid path");
      return;
    }
    let metadata;
    try {
      metadata = await stat(path);
    } catch {
      metadata = undefined;
    }
    if (metadata?.isDirectory()) path = resolve(path, "index.html");
    try {
      metadata = await stat(path);
    } catch {
      path = resolve(root, "404.html");
      metadata = await stat(path);
      response.statusCode = 404;
    }
    if (!metadata.isFile()) {
      response.writeHead(404).end("Not found");
      return;
    }
    const extension = extname(path);
    const contentType = contentTypes[extension] ?? (extension === "" ? await sniffContentType(path) : undefined);
    response.setHeader("Content-Type", contentType ?? "application/octet-stream");
    if (url.pathname.startsWith("/_next/") || url.pathname.startsWith("/images/")) {
      response.setHeader("Cache-Control", "public, max-age=31536000, immutable");
    } else {
      response.setHeader("Cache-Control", "no-cache");
    }
    const acceptsGzip = request.headers["accept-encoding"]?.includes("gzip");
    if (acceptsGzip && compressibleExtensions.has(extension)) {
      response.setHeader("Content-Encoding", "gzip");
      response.setHeader("Vary", "Accept-Encoding");
      createReadStream(path).pipe(createGzip()).pipe(response);
    } else {
      createReadStream(path).pipe(response);
    }
  } catch (error) {
    response.writeHead(500).end(error instanceof Error ? error.message : "Internal server error");
  }
});

server.listen(port, host, () => console.log(`Serving landing site at http://${host}:${port}`));
process.on("SIGTERM", () => server.close());
process.on("SIGINT", () => server.close());
