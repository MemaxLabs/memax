import type { MemoryListItem } from "./memories";

/**
 * The demo's export: a real zip archive, so the download works end to
 * end, holding what the demo has (its memories' words, by section). The
 * demo has no receipts or seals, so it says so instead of faking the
 * export format; a signed-in space's export comes from the server
 * (memax.export.v1).
 */

const CRC_TABLE = (() => {
  const t = new Uint32Array(256);
  for (let n = 0; n < 256; n++) {
    let c = n;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    t[n] = c >>> 0;
  }
  return t;
})();

function crc32(b: Uint8Array): number {
  let c = 0xffffffff;
  for (const x of b) c = CRC_TABLE[(c ^ x) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
}

/** A zip archive of stored (uncompressed) files. */
export function storedZip(
  files: Array<[string, string]>,
): Uint8Array<ArrayBuffer> {
  const enc = new TextEncoder();
  const chunks: Uint8Array[] = [];
  const central: Uint8Array[] = [];
  let offset = 0;
  for (const [name, text] of files) {
    const n = enc.encode(name);
    const data = enc.encode(text);
    const crc = crc32(data);
    const local = new Uint8Array(30 + n.length);
    const lv = new DataView(local.buffer);
    lv.setUint32(0, 0x04034b50, true);
    lv.setUint16(4, 20, true);
    lv.setUint16(6, 0x800, true); // UTF-8 names
    lv.setUint32(14, crc, true);
    lv.setUint32(18, data.length, true);
    lv.setUint32(22, data.length, true);
    lv.setUint16(26, n.length, true);
    local.set(n, 30);
    chunks.push(local, data);
    const c = new Uint8Array(46 + n.length);
    const cv = new DataView(c.buffer);
    cv.setUint32(0, 0x02014b50, true);
    cv.setUint16(4, 20, true);
    cv.setUint16(6, 20, true);
    cv.setUint16(8, 0x800, true);
    cv.setUint32(16, crc, true);
    cv.setUint32(20, data.length, true);
    cv.setUint32(24, data.length, true);
    cv.setUint16(28, n.length, true);
    cv.setUint32(42, offset, true);
    c.set(n, 46);
    central.push(c);
    offset += local.length + data.length;
  }
  const size = central.reduce((s, c) => s + c.length, 0);
  const end = new Uint8Array(22);
  const ev = new DataView(end.buffer);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(8, files.length, true);
  ev.setUint16(10, files.length, true);
  ev.setUint32(12, size, true);
  ev.setUint32(16, offset, true);
  const out = new Uint8Array(offset + size + 22);
  let at = 0;
  for (const part of [...chunks, ...central, end]) {
    out.set(part, at);
    at += part.length;
  }
  return out;
}

/** The demo space's export: a README and one file per memory. */
export function demoExport(
  slug: string,
  name: string,
  items: MemoryListItem[],
): Uint8Array<ArrayBuffer> {
  const shown = items.filter((m) => !m.forgotten);
  const readme = [
    `# ${name}`,
    "",
    "The demo dataset's memories, one Markdown file each in `memories/`.",
    "The demo has no receipts, so this isn't the full export format: a",
    "signed-in space's export also carries every memory's record in",
    "frontmatter, its receipts and the signed checkpoints, and",
    "`memax verify-export` checks it.",
    "",
  ].join("\n");
  return storedZip([
    [`${slug}/README.md`, readme],
    ...shown.map((m): [string, string] => [
      `${slug}/memories/${m.ref}.md`,
      `${m.statement}\n`,
    ]),
  ]);
}
