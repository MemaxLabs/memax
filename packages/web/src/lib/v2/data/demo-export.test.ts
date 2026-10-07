import { describe, expect, it } from "vitest";
import { demoExport, storedZip } from "./demo-export";
import type { MemoryListItem } from "./memories";

/** The archive's files, read back from its central directory. */
function entries(zip: Uint8Array): Map<string, string> {
  const dv = new DataView(zip.buffer, zip.byteOffset, zip.byteLength);
  const end = zip.length - 22;
  expect(dv.getUint32(end, true)).toBe(0x06054b50);
  const out = new Map<string, string>();
  let p = dv.getUint32(end + 16, true);
  for (let i = 0; i < dv.getUint16(end + 10, true); i++) {
    expect(dv.getUint32(p, true)).toBe(0x02014b50);
    const size = dv.getUint32(p + 24, true);
    const nlen = dv.getUint16(p + 28, true);
    const local = dv.getUint32(p + 42, true);
    const name = new TextDecoder().decode(zip.subarray(p + 46, p + 46 + nlen));
    const start = local + 30 + dv.getUint16(local + 26, true);
    out.set(name, new TextDecoder().decode(zip.subarray(start, start + size)));
    p += 46 + nlen;
  }
  return out;
}

const item = (ref: string, statement: string, forgotten = false) =>
  ({
    ref,
    statement,
    section: "decisions",
    state: forgotten ? "forgotten" : "kept",
    receipt: null,
    source: null,
    note: null,
    forgotten: forgotten
      ? { at: "2026-10-06T09:00:00Z", by: null, detail: null }
      : null,
  }) as MemoryListItem;

describe("the demo's export", () => {
  it("is a zip archive of its memories, one folder for the space", () => {
    const files = entries(
      demoExport("memax-v2", "memax-v2", [
        item("M-0219", "Background jobs run on River."),
        item("M-0201", "", true),
      ]),
    );
    expect([...files.keys()]).toEqual([
      "memax-v2/README.md",
      "memax-v2/memories/M-0219.md",
    ]);
    expect(files.get("memax-v2/memories/M-0219.md")).toBe(
      "Background jobs run on River.\n",
    );
    expect(files.get("memax-v2/README.md")).toContain(
      "isn't the full export format",
    );
  });

  it("keeps UTF-8 names and words", () => {
    const files = entries(storedZip([["空间/记忆.md", "保留 — café\n"]]));
    expect(files.get("空间/记忆.md")).toBe("保留 — café\n");
  });
});
