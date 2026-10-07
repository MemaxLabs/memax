// An export's files on disk: reading the archive the server sends (a zip,
// stored or deflated, as Go's archive/zip writes it), writing it into a
// folder without ever leaving that folder, and reading a folder (or an
// archive downloaded from the web app) back for `memax verify-export`.
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { inflateRawSync } from "node:zlib";

export class ExportFilesError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ExportFilesError";
  }
}

const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

export function crc32(b: Uint8Array): number {
  let c = 0xffffffff;
  for (const x of b) c = CRC_TABLE[(c ^ x) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

/**
 * A zip archive's files, by name. Reads the central directory (so data
 * descriptors are fine), stored and deflated entries, and checks every
 * entry's size and CRC-32. No zip64: an export is far smaller.
 */
export function readZip(buf: Uint8Array): Map<string, Uint8Array> {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const u16 = (at: number) => dv.getUint16(at, true);
  const u32 = (at: number) => dv.getUint32(at, true);
  let eocd = -1;
  for (
    let i = buf.length - 22;
    i >= Math.max(0, buf.length - 22 - 0xffff);
    i--
  ) {
    if (u32(i) === 0x06054b50) {
      eocd = i;
      break;
    }
  }
  if (eocd < 0) {
    throw new ExportFilesError(
      "This isn't a zip archive, or it was cut short. Export again.",
    );
  }
  const count = u16(eocd + 10);
  let p = u32(eocd + 16);
  const names = new TextDecoder("utf-8", { fatal: true });
  const out = new Map<string, Uint8Array>();
  for (let i = 0; i < count; i++) {
    if (p + 46 > buf.length || u32(p) !== 0x02014b50) {
      throw new ExportFilesError(
        "The archive's directory is damaged. Export again.",
      );
    }
    const method = u16(p + 10);
    const crc = u32(p + 16);
    const csize = u32(p + 20);
    const usize = u32(p + 24);
    const nlen = u16(p + 28);
    const local = u32(p + 42);
    const name = names.decode(buf.subarray(p + 46, p + 46 + nlen));
    p += 46 + nlen + u16(p + 30) + u16(p + 32);
    if (name.endsWith("/")) continue;
    if (local + 30 > buf.length || u32(local) !== 0x04034b50) {
      throw new ExportFilesError(`The archive's entry ${name} is damaged.`);
    }
    const start = local + 30 + u16(local + 26) + u16(local + 28);
    const raw = buf.subarray(start, start + csize);
    let data: Uint8Array;
    if (method === 0) data = raw;
    else if (method === 8) data = new Uint8Array(inflateRawSync(raw));
    else
      throw new ExportFilesError(
        `The archive's entry ${name} uses compression method ${method}, which this can't read.`,
      );
    if (data.length !== usize || crc32(data) !== crc) {
      throw new ExportFilesError(
        `The archive's entry ${name} doesn't match its checksum: it was damaged or cut short.`,
      );
    }
    out.set(name, data);
  }
  return out;
}

/** A path inside an export: relative, forward slashes, no `..`. */
export function safePath(path: string): boolean {
  if (
    !path ||
    path.startsWith("/") ||
    path.includes("\\") ||
    path.includes("\0")
  )
    return false;
  return path
    .split("/")
    .every((seg) => seg !== "" && seg !== "." && seg !== "..");
}

/**
 * Splits an archive's entries into its one folder's name and the files
 * below it. Every entry must sit in that folder, at a safe path.
 */
export function exportFolder(entries: Map<string, Uint8Array>): {
  root: string;
  files: Map<string, Uint8Array>;
} {
  let root = "";
  const files = new Map<string, Uint8Array>();
  for (const [name, data] of entries) {
    const slash = name.indexOf("/");
    const top = name.slice(0, slash);
    const rest = name.slice(slash + 1);
    if (slash <= 0 || !safePath(name)) {
      throw new ExportFilesError(
        `The archive holds ${JSON.stringify(name)}, outside an export's folder. Nothing was written.`,
      );
    }
    if (root && top !== root) {
      throw new ExportFilesError(
        `The archive holds two folders, ${root} and ${top}. Nothing was written.`,
      );
    }
    root = top;
    files.set(rest, data);
  }
  if (!root) throw new ExportFilesError("The archive is empty. Export again.");
  return { root, files };
}

/**
 * Writes an export's files into dir, which must not exist yet or be empty
 * (or, with replace, hold an earlier export: one with an export.json).
 */
export function writeExport(
  dir: string,
  files: Map<string, Uint8Array>,
  replace: boolean,
): void {
  if (existsSync(dir) && readdirSync(dir).length > 0) {
    if (!replace) {
      throw new ExportFilesError(
        `${dir} isn't empty. Choose another --out, or pass --force to replace the export there.`,
      );
    }
    if (!existsSync(join(dir, "export.json"))) {
      throw new ExportFilesError(
        `${dir} doesn't hold a Memax export, so --force won't replace it. Choose another --out.`,
      );
    }
    rmSync(dir, { recursive: true, force: true });
  }
  const base = resolve(dir);
  for (const [path, data] of files) {
    const target = resolve(base, ...path.split("/"));
    if (!safePath(path) || !target.startsWith(base + sep)) {
      throw new ExportFilesError(`${path} would be written outside ${dir}.`);
    }
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, data);
  }
}

/**
 * Reads an export back for verifying: a folder (the space's, with its
 * export.json), or a .zip archive as the web app downloads it. Hidden files
 * (.DS_Store and the like) aren't part of an export and are left out.
 */
export function readExport(path: string): {
  root: string;
  files: Map<string, Uint8Array>;
} {
  if (!existsSync(path)) {
    throw new ExportFilesError(`${path} doesn't exist.`);
  }
  if (statSync(path).isFile()) {
    return exportFolder(readZip(readFileSync(path)));
  }
  let dir = path;
  if (!existsSync(join(dir, "export.json"))) {
    // The folder `memax export` wrote into: one space's folder inside.
    const inner = readdirSync(dir).filter((n) =>
      existsSync(join(dir, n, "export.json")),
    );
    if (inner.length !== 1) {
      throw new ExportFilesError(
        inner.length === 0
          ? `${path} holds no export.json. Point at the space's export folder.`
          : `${path} holds ${inner.length} exports (${inner.join(", ")}). Verify them one at a time.`,
      );
    }
    dir = join(dir, inner[0]);
  }
  const files = new Map<string, Uint8Array>();
  const walk = (d: string) => {
    for (const name of readdirSync(d).sort()) {
      if (name.startsWith(".")) continue;
      const p = join(d, name);
      if (statSync(p).isDirectory()) walk(p);
      else files.set(relative(dir, p).split(sep).join("/"), readFileSync(p));
    }
  };
  walk(dir);
  return { root: dir, files };
}
