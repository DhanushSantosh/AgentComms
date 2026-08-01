import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, resolve, sep } from "node:path";
import { createGzip } from "node:zlib";

const arguments_ = process.argv.slice(2);
const portIndex = arguments_.indexOf("--port");
const hostIndex = arguments_.indexOf("--host");
const port = Number(portIndex >= 0 ? arguments_[portIndex + 1] : 4323);
const host = hostIndex >= 0 ? arguments_[hostIndex + 1] : "127.0.0.1";
const root = resolve("dist");
const contentTypes = {
  ".css": "text/css; charset=utf-8",
  ".gif": "image/gif",
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".md": "text/markdown; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".txt": "text/plain; charset=utf-8",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".xml": "application/xml; charset=utf-8"
};
const compressibleExtensions = new Set([".css", ".html", ".js", ".json", ".md", ".svg", ".txt", ".xml"]);

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
    response.setHeader("Content-Type", contentTypes[extension] ?? "application/octet-stream");
    if (url.pathname.startsWith("/_astro/") || url.pathname.startsWith("/fonts/")) {
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

server.listen(port, host, () => console.log(`Serving documentation at http://${host}:${port}`));
process.on("SIGTERM", () => server.close());
process.on("SIGINT", () => server.close());
