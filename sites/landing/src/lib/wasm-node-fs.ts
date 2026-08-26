// A minimal in-memory filesystem for the Go WASM runtime (wasm_exec.js),
// installed onto `globalThis.fs` before wasm_exec.js runs.
//
// Why this exists: cmd/agent-comms-tui-wasm/bootstrap.go seeds its demo
// project with real os.MkdirTemp/os.MkdirAll/os.WriteFile/os.ReadFile calls
// (see internal/identity's credential store and internal/store's
// config.json, both plain-Go code paths shared with the real CLI -- not
// something this task is allowed to change). The vanilla wasm_exec.js
// shipped by the Go toolchain only stubs `globalThis.fs` with ENOSYS for
// every real filesystem call (see its own comment: "if (!globalThis.fs)"),
// so those calls fail outright under GOOS=js with errors like "mkdir ...
// not implemented on js" -- confirmed live before this file existed. Since
// wasm_exec.js only installs its stub when `globalThis.fs` doesn't already
// exist, installing a real (if entirely in-memory, per-page-load) one here
// first makes the existing Go code work unmodified.
//
// This mirrors Node.js's callback-based `fs` module shape exactly, because
// that's the shape Go's syscall/fs_js.go (in the Go standard library) calls
// through -- see $GOROOT/src/syscall/fs_js.go for the authoritative
// contract this implements against: every method takes a Node-style
// `(err, result) => void` callback as its last argument, and errors are
// plain Error objects with a `.code` string (e.g. "ENOENT") that Go's
// mapJSError looks up in its own errno table. Only the codes that table
// actually recognizes are used below.
type FsCallback<T = unknown> = (error: NodeStyleError | null, value?: T) => void;

interface NodeStyleError extends Error {
  code: string;
}

interface FileNode {
  kind: "file";
  data: Uint8Array;
  mtimeMs: number;
}

interface DirNode {
  kind: "dir";
  children: Map<string, FsNode>;
  mtimeMs: number;
}

type FsNode = FileNode | DirNode;

interface OpenFile {
  node: FileNode;
  position: number;
}

// Bit flags this shim controls both ends of: Go's fs_js.go only ever reads
// these values back off the `constants` object we hand it and ORs them
// together, so any distinct bit pattern works as long as `open()` below
// decodes the same values consistently.
const O_WRONLY = 1 << 0;
const O_RDWR = 1 << 1;
const O_CREAT = 1 << 2;
const O_TRUNC = 1 << 3;
const O_APPEND = 1 << 4;
const O_EXCL = 1 << 5;

function makeError(code: string, message?: string): NodeStyleError {
  const error = new Error(message ?? code) as NodeStyleError;
  error.code = code;
  return error;
}

function normalizePath(path: string): string {
  const isAbsolute = path.startsWith("/");
  const segments: string[] = [];
  for (const part of path.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") segments.pop();
    else segments.push(part);
  }
  return (isAbsolute ? "/" : "") + segments.join("/");
}

function splitParent(path: string): { parentPath: string; name: string } {
  const normalized = normalizePath(path);
  const lastSlash = normalized.lastIndexOf("/");
  return {
    parentPath: lastSlash <= 0 ? "/" : normalized.slice(0, lastSlash),
    name: normalized.slice(lastSlash + 1)
  };
}

/** Builds a fresh, empty in-memory filesystem and installs it as
 * `globalThis.fs`, unless something (a previous call, most likely -- this
 * is idempotent per page load) has already installed one. Must run before
 * wasm_exec.js is imported. */
export function installVirtualFs(): void {
  const globalWithFs = globalThis as typeof globalThis & { fs?: unknown };
  if (globalWithFs.fs) return;

  const root: DirNode = { kind: "dir", children: new Map(), mtimeMs: Date.now() };
  // os.MkdirTemp("", pattern) resolves "" to os.TempDir(), which is "/tmp"
  // whenever $TMPDIR is unset (it always is here) -- and unlike every real
  // Unix root, this in-memory one starts out completely empty, so without
  // this, Mkdir("/tmp/pattern...") fails ENOENT and MkdirTemp's own
  // fallback Stat("/tmp") fails too, surfacing as "stat /tmp: no such file
  // or directory" (confirmed live before this line existed).
  root.children.set("tmp", { kind: "dir", children: new Map(), mtimeMs: Date.now() });
  const openFiles = new Map<number, OpenFile>();
  let nextFd = 64;
  let stdoutBuffer = "";
  let stderrBuffer = "";
  const decoder = new TextDecoder("utf-8");

  function lookup(path: string): FsNode | undefined {
    const normalized = normalizePath(path);
    if (normalized === "" || normalized === "/") return root;
    let node: FsNode = root;
    for (const part of normalized.split("/").filter(Boolean)) {
      if (node.kind !== "dir") return undefined;
      const next = node.children.get(part);
      if (!next) return undefined;
      node = next;
    }
    return node;
  }

  function flushStd(which: "stdout" | "stderr", text: string): string {
    let buffer = text;
    const newline = buffer.lastIndexOf("\n");
    if (newline !== -1) {
      const line = buffer.slice(0, newline);
      if (which === "stdout") console.log(line);
      else console.error(line);
      buffer = buffer.slice(newline + 1);
    }
    return buffer;
  }

  function writeBytes(fd: number, buffer: Uint8Array): number {
    if (fd === 1 || fd === 2) {
      const text = decoder.decode(buffer);
      if (fd === 1) stdoutBuffer = flushStd("stdout", stdoutBuffer + text);
      else stderrBuffer = flushStd("stderr", stderrBuffer + text);
      return buffer.length;
    }
    const open = openFiles.get(fd);
    if (!open) throw makeError("EBADF");
    const position = open.position;
    const end = position + buffer.length;
    if (end > open.node.data.length) {
      const grown = new Uint8Array(end);
      grown.set(open.node.data);
      open.node.data = grown;
    }
    open.node.data.set(buffer, position);
    open.node.mtimeMs = Date.now();
    open.position = end;
    return buffer.length;
  }

  function statFor(node: FsNode): Record<string, unknown> {
    const isDir = node.kind === "dir";
    return {
      dev: 0,
      ino: 0,
      mode: isDir ? 0o40755 : 0o100644,
      nlink: 1,
      uid: 0,
      gid: 0,
      rdev: 0,
      size: isDir ? 0 : node.data.length,
      blksize: 4096,
      blocks: 0,
      atimeMs: node.mtimeMs,
      mtimeMs: node.mtimeMs,
      ctimeMs: node.mtimeMs,
      isDirectory: () => isDir,
      isFile: () => !isDir
    };
  }

  const virtualFs = {
    constants: {
      O_WRONLY,
      O_RDWR,
      O_CREAT,
      O_TRUNC,
      O_APPEND,
      O_EXCL,
      O_DIRECTORY: -1
    },

    writeSync(fd: number, buffer: Uint8Array): number {
      return writeBytes(fd, buffer);
    },

    write(
      fd: number,
      buffer: Uint8Array,
      offset: number,
      length: number,
      position: number | null,
      callback: FsCallback<number>
    ): void {
      try {
        const slice = buffer.subarray(offset, offset + length);
        if (position != null && fd !== 1 && fd !== 2) {
          const open = openFiles.get(fd);
          if (!open) throw makeError("EBADF");
          const savedPosition = open.position;
          open.position = position;
          const written = writeBytes(fd, slice);
          open.position = savedPosition + written;
          callback(null, written);
          return;
        }
        callback(null, writeBytes(fd, slice));
      } catch (error) {
        callback(error as NodeStyleError);
      }
    },

    open(path: string, flags: number, _mode: number, callback: FsCallback<number>): void {
      try {
        let node = lookup(path);
        if (!node) {
          if (!(flags & O_CREAT)) throw makeError("ENOENT");
          const { parentPath, name } = splitParent(path);
          const parent = lookup(parentPath);
          if (!parent || parent.kind !== "dir") throw makeError("ENOENT");
          const file: FileNode = { kind: "file", data: new Uint8Array(0), mtimeMs: Date.now() };
          parent.children.set(name, file);
          node = file;
        } else if (flags & O_EXCL && flags & O_CREAT) {
          throw makeError("EEXIST");
        }
        if (node.kind === "dir") {
          // Directory fds are only ever fstat'd (fs_js.go's own Open does
          // this to decide whether to readdir), never read/written by this
          // codebase's actual call paths, so an empty-file placeholder
          // stand-in is never actually exercised as file data.
          const fd = nextFd++;
          openFiles.set(fd, { node: { kind: "file", data: new Uint8Array(0), mtimeMs: node.mtimeMs }, position: 0 });
          callback(null, fd);
          return;
        }
        if (flags & O_TRUNC && (flags & O_WRONLY || flags & O_RDWR)) {
          node.data = new Uint8Array(0);
          node.mtimeMs = Date.now();
        }
        const fd = nextFd++;
        const position = flags & O_APPEND ? node.data.length : 0;
        openFiles.set(fd, { node, position });
        callback(null, fd);
      } catch (error) {
        callback(error as NodeStyleError);
      }
    },

    close(fd: number, callback: FsCallback<void>): void {
      openFiles.delete(fd);
      callback(null);
    },

    read(
      fd: number,
      buffer: Uint8Array,
      offset: number,
      length: number,
      position: number | null,
      callback: FsCallback<number>
    ): void {
      const open = openFiles.get(fd);
      if (!open) {
        callback(makeError("EBADF"));
        return;
      }
      const readPosition = position ?? open.position;
      const available = Math.max(0, open.node.data.length - readPosition);
      const n = Math.min(length, available);
      buffer.set(open.node.data.subarray(readPosition, readPosition + n), offset);
      if (position == null) open.position += n;
      callback(null, n);
    },

    fstat(fd: number, callback: FsCallback<Record<string, unknown>>): void {
      const open = openFiles.get(fd);
      if (!open) {
        callback(makeError("EBADF"));
        return;
      }
      callback(null, statFor(open.node));
    },

    stat(path: string, callback: FsCallback<Record<string, unknown>>): void {
      const node = lookup(path);
      if (!node) {
        callback(makeError("ENOENT"));
        return;
      }
      callback(null, statFor(node));
    },

    lstat(path: string, callback: FsCallback<Record<string, unknown>>): void {
      virtualFs.stat(path, callback);
    },

    mkdir(path: string, _perm: number, callback: FsCallback<void>): void {
      const { parentPath, name } = splitParent(path);
      const parent = lookup(parentPath);
      if (!parent || parent.kind !== "dir") {
        callback(makeError("ENOENT"));
        return;
      }
      if (parent.children.has(name)) {
        callback(makeError("EEXIST"));
        return;
      }
      parent.children.set(name, { kind: "dir", children: new Map(), mtimeMs: Date.now() });
      callback(null);
    },

    rmdir(path: string, callback: FsCallback<void>): void {
      const { parentPath, name } = splitParent(path);
      const parent = lookup(parentPath);
      const target = parent?.kind === "dir" ? parent.children.get(name) : undefined;
      if (!parent || parent.kind !== "dir" || !target) {
        callback(makeError("ENOENT"));
        return;
      }
      if (target.kind !== "dir") {
        callback(makeError("ENOTDIR"));
        return;
      }
      if (target.children.size > 0) {
        callback(makeError("ENOTEMPTY"));
        return;
      }
      parent.children.delete(name);
      callback(null);
    },

    unlink(path: string, callback: FsCallback<void>): void {
      const { parentPath, name } = splitParent(path);
      const parent = lookup(parentPath);
      if (!parent || parent.kind !== "dir" || !parent.children.has(name)) {
        callback(makeError("ENOENT"));
        return;
      }
      parent.children.delete(name);
      callback(null);
    },

    rename(from: string, to: string, callback: FsCallback<void>): void {
      const source = splitParent(from);
      const destination = splitParent(to);
      const sourceParent = lookup(source.parentPath);
      const destinationParent = lookup(destination.parentPath);
      const node = sourceParent?.kind === "dir" ? sourceParent.children.get(source.name) : undefined;
      if (!sourceParent || sourceParent.kind !== "dir" || !node || !destinationParent || destinationParent.kind !== "dir") {
        callback(makeError("ENOENT"));
        return;
      }
      sourceParent.children.delete(source.name);
      destinationParent.children.set(destination.name, node);
      callback(null);
    },

    readdir(path: string, callback: FsCallback<string[]>): void {
      const node = lookup(path);
      if (!node || node.kind !== "dir") {
        callback(makeError("ENOTDIR"));
        return;
      }
      callback(null, [...node.children.keys()]);
    },

    truncate(path: string, length: number, callback: FsCallback<void>): void {
      const node = lookup(path);
      if (!node || node.kind !== "file") {
        callback(makeError("ENOENT"));
        return;
      }
      const next = new Uint8Array(length);
      next.set(node.data.subarray(0, Math.min(length, node.data.length)));
      node.data = next;
      callback(null);
    },

    ftruncate(fd: number, length: number, callback: FsCallback<void>): void {
      const open = openFiles.get(fd);
      if (!open) {
        callback(makeError("EBADF"));
        return;
      }
      virtualFs.truncate("", length, (error) => {
        if (error) {
          callback(error);
          return;
        }
        const next = new Uint8Array(length);
        next.set(open.node.data.subarray(0, Math.min(length, open.node.data.length)));
        open.node.data = next;
        callback(null);
      });
    },

    fsync(_fd: number, callback: FsCallback<void>): void {
      callback(null);
    },
    chmod(_path: string, _mode: number, callback: FsCallback<void>): void {
      callback(null);
    },
    fchmod(_fd: number, _mode: number, callback: FsCallback<void>): void {
      callback(null);
    },
    chown(_path: string, _uid: number, _gid: number, callback: FsCallback<void>): void {
      callback(null);
    },
    fchown(_fd: number, _uid: number, _gid: number, callback: FsCallback<void>): void {
      callback(null);
    },
    lchown(_path: string, _uid: number, _gid: number, callback: FsCallback<void>): void {
      callback(null);
    },
    utimes(_path: string, _atime: number, _mtime: number, callback: FsCallback<void>): void {
      callback(null);
    },
    link(_path: string, _link: string, callback: FsCallback<void>): void {
      callback(makeError("ENOSYS"));
    },
    symlink(_path: string, _link: string, callback: FsCallback<void>): void {
      callback(makeError("ENOSYS"));
    },
    readlink(_path: string, callback: FsCallback<string>): void {
      callback(makeError("ENOSYS"));
    }
  };

  globalWithFs.fs = virtualFs;
}
