import { createHash } from "node:crypto";
import {
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateRawSync } from "node:zlib";
import { afterEach, describe, expect, it } from "vitest";
import type { Memax, V2 } from "memax-sdk";
import {
  exportSpaces,
  pinnedKey,
  verifyExportFolder,
} from "../../src/commands/export.js";
import {
  crc32,
  exportFolder,
  readExport,
  readZip,
} from "../../src/lib/export-files.js";

// The golden export the server's writer produces (packages/server/
// internal/export, TestGoldenExport), shared with the SDK's tests.
const golden = fileURLToPath(
  new URL("../../../sdk/src/v2/testdata/export-v1/memax-v2", import.meta.url),
);

function load(dir: string): Map<string, Uint8Array> {
  const out = new Map<string, Uint8Array>();
  const walk = (d: string) => {
    for (const name of readdirSync(d).sort()) {
      const p = join(d, name);
      if (statSync(p).isDirectory()) walk(p);
      else
        out.set(
          relative(dir, p).split(sep).join("/"),
          new Uint8Array(readFileSync(p)),
        );
    }
  };
  walk(dir);
  return out;
}

/** A zip archive, deflated with data descriptors as Go writes it, or stored. */
function zip(
  entries: Array<[string, Uint8Array]>,
  how: "deflate" | "store" = "deflate",
): Uint8Array {
  const parts: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const [name, data] of entries) {
    const n = Buffer.from(name);
    const body = how === "deflate" ? deflateRawSync(data) : Buffer.from(data);
    const crc = crc32(data);
    const method = how === "deflate" ? 8 : 0;
    const flags = (how === "deflate" ? 0x08 : 0) | 0x800;
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt16LE(20, 4);
    local.writeUInt16LE(flags, 6);
    local.writeUInt16LE(method, 8);
    local.writeUInt32LE(how === "deflate" ? 0 : crc, 14);
    local.writeUInt32LE(how === "deflate" ? 0 : body.length, 18);
    local.writeUInt32LE(how === "deflate" ? 0 : data.length, 22);
    local.writeUInt16LE(n.length, 26);
    parts.push(local, n, body);
    let size = 30 + n.length + body.length;
    if (how === "deflate") {
      const dd = Buffer.alloc(16);
      dd.writeUInt32LE(0x08074b50, 0);
      dd.writeUInt32LE(crc, 4);
      dd.writeUInt32LE(body.length, 8);
      dd.writeUInt32LE(data.length, 12);
      parts.push(dd);
      size += 16;
    }
    const c = Buffer.alloc(46);
    c.writeUInt32LE(0x02014b50, 0);
    c.writeUInt16LE(20, 4);
    c.writeUInt16LE(20, 6);
    c.writeUInt16LE(flags, 8);
    c.writeUInt16LE(method, 10);
    c.writeUInt32LE(crc, 16);
    c.writeUInt32LE(body.length, 20);
    c.writeUInt32LE(data.length, 24);
    c.writeUInt16LE(n.length, 28);
    c.writeUInt32LE(offset, 42);
    central.push(c, n);
    offset += size;
  }
  const cd = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(entries.length, 8);
  end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(cd.length, 12);
  end.writeUInt32LE(offset, 16);
  return new Uint8Array(Buffer.concat([...parts, cd, end]));
}

const files = load(golden);
const archive = (how: "deflate" | "store" = "deflate") =>
  zip(
    [...files].map(([p, d]) => [`memax-v2/${p}`, d]),
    how,
  );

const dirs: string[] = [];
function tmp(): string {
  const d = mkdtempSync(join(tmpdir(), "memax-export-"));
  dirs.push(d);
  return d;
}
afterEach(() => {
  for (const d of dirs.splice(0)) rmSync(d, { recursive: true, force: true });
});

const key = pinnedKey(
  JSON.parse(new TextDecoder().decode(files.get("checkpoints.json"))).keys[0]
    .public_key,
);

/** A stand-in for the SDK client: two spaces, one of them on V2. */
function fakeMemax(opts: { keys?: V2.SigningKey[] } = {}) {
  const exports: Array<{ space: string; key: string; via?: string }> = [];
  const memax = {
    v2: {
      spaces: {
        list: async () => ({
          items: [
            {
              slug: "memax-v2",
              id: "s1",
              v2_enabled_at: "2026-10-06T08:30:00Z",
            },
            { slug: "old-v1", id: "s2" },
          ],
        }),
        export: async (
          space: string,
          o: { idempotencyKey: string; via?: string },
        ) => {
          exports.push({ space, key: o.idempotencyKey, via: o.via });
          return {
            bytes: archive(),
            filename: "memax-memax-v2-2026-10-06.zip",
            receipt: "0199a1b2-c3d4-7e5f-8a9b-00000000040e",
            replayed: false,
          };
        },
      },
      receipts: {
        checkpoints: async () => ({
          items: [],
          has_more: false,
          seal: { sealed_receipts: 13, checkpoints: 2, unsealed: 0 },
          keys: opts.keys ?? [key],
        }),
      },
    },
  } as unknown as Memax;
  return { memax, exports };
}

function capture() {
  const lines: string[] = [];
  return {
    lines,
    out: (l: string) => lines.push(l),
    text: () => lines.join("\n"),
  };
}

describe("the archive", () => {
  it("reads the archive the server's writer streams", () => {
    const fromGo = readFileSync(
      fileURLToPath(
        new URL("../../../sdk/src/v2/testdata/export-v1.zip", import.meta.url),
      ),
    );
    const { root, files: got } = exportFolder(readZip(fromGo));
    expect(root).toBe("memax-v2");
    expect(got).toEqual(files);
  });

  it("reads deflated entries with data descriptors, and stored ones", () => {
    for (const how of ["deflate", "store"] as const) {
      const { root, files: got } = exportFolder(readZip(archive(how)));
      expect(root).toBe("memax-v2");
      expect(got).toEqual(files);
    }
  });

  it("refuses a damaged archive, and paths outside the folder", () => {
    const damaged = archive("store");
    damaged[200] ^= 0xff;
    expect(() => readZip(damaged)).toThrow(/checksum/);
    expect(() => readZip(archive().subarray(0, 500))).toThrow(/cut short/);
    for (const name of [
      "../evil.md",
      "memax-v2/../../evil.md",
      "/etc/x",
      "top.md",
    ]) {
      expect(() =>
        exportFolder(readZip(zip([[name, new Uint8Array([1])]]))),
      ).toThrow(/Nothing was written/);
    }
    expect(() =>
      exportFolder(
        readZip(
          zip([
            ["a/x.md", new Uint8Array([1])],
            ["b/y.md", new Uint8Array([1])],
          ]),
        ),
      ),
    ).toThrow(/two folders/);
  });
});

describe("memax export", () => {
  it("writes every V2 space's export into its folder", async () => {
    const cwd = tmp();
    const { memax, exports } = fakeMemax();
    const c = capture();
    expect(await exportSpaces({}, { memax, cwd, out: c.out })).toBe(0);
    expect(exports).toEqual([
      { space: "memax-v2", key: expect.any(String), via: "cli" },
    ]);
    expect(load(join(cwd, "memax-export", "memax-v2"))).toEqual(files);
    expect(c.text()).toContain("Exported memax-v2");
    expect(c.text()).toContain(
      "4 memories · 1 tombstone · 18 receipts, sealed through 13",
    );
    expect(c.text()).toContain("memax verify-export memax-export/memax-v2");

    // An earlier export stays unless --force replaces it.
    await expect(
      exportSpaces({ space: "memax-v2" }, { memax, cwd, out: c.out }),
    ).rejects.toThrow(/isn't empty.*--force/);
    expect(
      await exportSpaces(
        { space: "memax-v2", force: true, format: "json" },
        { memax, cwd, out: c.out },
      ),
    ).toBe(0);
    const json = JSON.parse(c.lines[c.lines.length - 1]);
    expect(json.exports[0]).toMatchObject({
      space: "memax-v2",
      memories: 4,
      receipts: 18,
      sealed: 13,
      unsealed: 5,
    });
    // --force never replaces a folder that isn't an export.
    writeFileSync(join(cwd, "memax-export", "memax-v2", "export.json"), "");
    rmSync(join(cwd, "memax-export", "memax-v2", "export.json"));
    await expect(
      exportSpaces(
        { space: "memax-v2", force: true },
        { memax, cwd, out: c.out },
      ),
    ).rejects.toThrow(/doesn't hold a Memax export/);
    await expect(
      exportSpaces({ space: "nope" }, { memax, cwd, out: c.out }),
    ).rejects.toThrow(/isn't one of your spaces/);
  });
});

describe("memax verify-export", () => {
  async function written(): Promise<string> {
    const cwd = tmp();
    await exportSpaces({}, { ...fakeMemax(), cwd, out: () => {} });
    return join(cwd, "memax-export", "memax-v2");
  }

  it("says exactly what it verified", async () => {
    const dir = await written();
    const c = capture();
    const code = await verifyExportFolder(
      dir,
      {},
      {
        memax: fakeMemax().memax,
        apiUrl: "https://api.memax.app",
        out: c.out,
      },
    );
    expect(c.text()).not.toContain("✗");
    expect(code).toBe(0);
    const t = c.text();
    expect(t).toContain("Verified memax-v2");
    expect(t).toContain(`${files.size - 1} files match export.json`);
    expect(t).toContain(
      `Receipts 1–13 are sealed in 2 checkpoints; every signature verifies (${key.key_id}, from https://api.memax.app)`,
    );
    expect(t).toContain(
      "5 receipts came after the last seal (lines 14–18 of receipts.jsonl)",
    );
    expect(t).toContain(
      "4 memories, 1 tombstone and 2 Brief versions match their receipts",
    );
    // Its parent folder, and the archive itself, verify the same way.
    expect(
      await verifyExportFolder(
        join(dir, ".."),
        { key: [key.public_key] },
        { out: () => {} },
      ),
    ).toBe(0);
    const zipPath = join(tmp(), "memax-memax-v2-2026-10-06.zip");
    writeFileSync(zipPath, archive());
    expect(
      await verifyExportFolder(zipPath, { offline: true }, { out: c.out }),
    ).toBe(0);
    expect(c.text()).toContain("checked with the keys the export carries");
  });

  it("fails a changed memory file, receipt or checkpoint, saying where", async () => {
    const dir = await written();
    const run = async (opts = {}) => {
      const c = capture();
      const code = await verifyExportFolder(
        dir,
        { key: [key.public_key], ...opts },
        { out: c.out },
      );
      return { code, text: c.text() };
    };
    const edit = (p: string, f: (s: string) => string) =>
      writeFileSync(join(dir, p), f(readFileSync(join(dir, p), "utf8")));
    const manifest = (p: string) => {
      const m = JSON.parse(readFileSync(join(dir, "export.json"), "utf8"));
      const data = readFileSync(join(dir, p));
      const f = m.files.find((x: { path: string }) => x.path === p);
      f.sha256 = createHash("sha256").update(data).digest("hex");
      f.bytes = data.length;
      writeFileSync(join(dir, "export.json"), JSON.stringify(m));
    };

    edit("memories/M-0001.md", (s) => s.replace("We don't use", "We use"));
    let r = await run();
    expect(r.code).toBe(1);
    expect(r.text).toContain("memax-v2 doesn't verify: 2 problems");
    expect(r.text).toContain("memories/M-0001.md: it changed after the export");
    expect(r.text).toContain(
      "memories/M-0001.md: its body isn't the statement of version 2 in its frontmatter",
    );
    writeFileSync(
      join(dir, "memories/M-0001.md"),
      files.get("memories/M-0001.md")!,
    );

    edit("receipts.jsonl", (s) => s.replace('"via":"cli"', '"via":"web"'));
    manifest("receipts.jsonl");
    r = await run();
    expect(r.code).toBe(1);
    expect(r.text).toContain(
      "receipts.jsonl: checkpoint 2 (receipts 7–13, lines 7–13 of receipts.jsonl): the Merkle root of its receipts differs from the signed one",
    );
    writeFileSync(join(dir, "receipts.jsonl"), files.get("receipts.jsonl")!);

    edit("checkpoints.json", (s) =>
      s.replace(
        /"chain_sha256": "[0-9a-f]+"/,
        `"chain_sha256": "${"1".repeat(64)}"`,
      ),
    );
    manifest("checkpoints.json");
    r = await run();
    expect(r.code).toBe(1);
    expect(r.text).toMatch(
      /checkpoints.json: checkpoint 1 \(receipts 1–6, lines 1–6 of receipts.jsonl\): the signature doesn't verify \(key ed25519:/,
    );
  });

  it("rejects a key that isn't one", () => {
    expect(() => pinnedKey("not-a-key")).toThrow(/isn't an Ed25519 public key/);
    expect(pinnedKey(key.public_key)).toEqual(key);
    expect(
      pinnedKey(Buffer.from(key.public_key, "base64").toString("hex")),
    ).toEqual(key);
  });

  it("says what it can't read", () => {
    expect(() => readExport(join(tmp(), "nothing"))).toThrow(/doesn't exist/);
    expect(() => readExport(tmp())).toThrow(/no export.json/);
  });
});
