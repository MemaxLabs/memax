import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it, vi } from "vitest";
import {
  ExportFormatError,
  Memax,
  parseExport,
  parseFrontmatter,
  verifyExport,
} from "../index.js";
import type { ExportManifest, V2 } from "../index.js";

// The golden export: the server writes it from its fixture
// (packages/server/internal/export, TestGoldenExport), so these tests read
// and verify exactly what the Go writer produces.
const golden = fileURLToPath(
  new URL("./testdata/export-v1/memax-v2", import.meta.url),
);

function load(dir: string): Map<string, Uint8Array> {
  const out = new Map<string, Uint8Array>();
  const walk = (d: string) => {
    for (const name of readdirSync(d)) {
      const p = join(d, name);
      if (statSync(p).isDirectory()) walk(p);
      else out.set(relative(dir, p).split(sep).join("/"), readFileSync(p));
    }
  };
  walk(dir);
  return out;
}

const enc = new TextEncoder();
const dec = new TextDecoder();
const text = (files: Map<string, Uint8Array>, p: string) =>
  dec.decode(files.get(p));

/** Changes a file, optionally rewriting export.json to match. */
function tamper(
  files: Map<string, Uint8Array>,
  path: string,
  edit: (s: string) => string,
  rewriteManifest = false,
): Map<string, Uint8Array> {
  const out = new Map(files);
  const before = text(files, path);
  const after = edit(before);
  expect(after).not.toBe(before);
  out.set(path, enc.encode(after));
  if (rewriteManifest) reManifest(out, path);
  return out;
}

function reManifest(files: Map<string, Uint8Array>, path: string) {
  const m = JSON.parse(text(files, "export.json")) as ExportManifest;
  const data = files.get(path);
  m.files = m.files.filter((f) => f.path !== path);
  if (data) {
    m.files.push({
      path,
      sha256: createHash("sha256").update(data).digest("hex"),
      bytes: data.length,
    });
  }
  files.set("export.json", enc.encode(JSON.stringify(m)));
}

function keyOf(files: Map<string, Uint8Array>): V2.SigningKey[] {
  return JSON.parse(text(files, "checkpoints.json")).keys;
}

describe("the export format, memax.export.v1", () => {
  const files = load(golden);

  it("reads the golden export back", () => {
    const p = parseExport(files);
    expect(p.manifest.space.slug).toBe("memax-v2");
    expect(p.memories.map((m) => m.ref)).toEqual([
      "M-0001",
      "M-0002",
      "M-0003",
      "M-0005",
    ]);
    const m1 = p.memories[0];
    expect(m1.version).toBe(2);
    expect(m1.body).toBe(
      `Background jobs run on River, Postgres-backed. We don't use Temporal ("one queue" & <one> DB).\n`,
    );
    expect(m1.decision?.options.map((o) => o.label)).toEqual([
      "River",
      "Temporal",
    ]);
    expect(m1.sources[1]).toMatchObject({
      kind: "file",
      ref: "go.mod:14",
      locator: { line: 14, path: "go.mod" },
    });
    expect(m1.versions.map((v) => v.version)).toEqual([1, 2]);
    expect(m1.receipts.map((r) => r.action)).toEqual([
      "proposed",
      "kept",
      "edited",
    ]);
    // A statement with a --- line and characters JSON escapes.
    expect(p.memories[3].body).toContain("Release steps:\n---\nTag first");
    expect(p.memories[3].body).toContain("\u2028");
    expect(p.tombstones).toHaveLength(1);
    expect(p.tombstones[0]).toMatchObject({
      ref: "M-0004",
      kind: "memory",
      receipts: [{ action: "kept" }, { action: "forgot" }],
    });
    expect(p.briefs.filter((b) => b.current).map((b) => b.ref)).toEqual([
      "B-0002",
    ]);
    expect(p.receipts).toHaveLength(18);
    expect(p.checkpoints.checkpoints).toHaveLength(2);
  });

  it("reads only the frontmatter subset, and says where it stops", () => {
    expect(
      parseFrontmatter(
        'a: "x"\nb:\n  - {"c": 1}\n  - 2\nd:\n  e: null\n  f: [1, 2]\ng: []\n',
      ),
    ).toEqual({ a: "x", b: [{ c: 1 }, 2], d: { e: null, f: [1, 2] }, g: [] });
    expect(() => parseFrontmatter("a: x\n", "memories/M-1.md")).toThrow(
      ExportFormatError,
    );
    expect(() => parseFrontmatter('a: "x"\na: "y"\n')).toThrow(/twice/);
    expect(() => parseFrontmatter("a:\nb: 1\n")).toThrow(/no value/);
  });

  it("verifies: every file, the chain against its signed checkpoints, and the records", async () => {
    const pinned = keyOf(files);
    for (const keys of [pinned, undefined]) {
      const r = await verifyExport({ files, keys });
      expect(r.problems).toEqual([]);
      expect(r).toMatchObject({
        ok: true,
        format: "memax.export.v1",
        space: { slug: "memax-v2" },
        files: { listed: files.size - 1, matched: files.size - 1 },
        sealed: { receipts: 13, throughSeq: 13, checkpoints: 2 },
        unsealed: { receipts: 5, firstLine: 14 },
        keys: { source: keys ? "given" : "export", ids: [pinned[0].key_id] },
        records: { memories: 4, tombstones: 1, briefs: 2 },
      });
      expect(r.chain).toMatchObject({ ok: true, signed: 2, receipts: 18 });
    }
  });

  it("catches a changed receipt, even with export.json rewritten", async () => {
    const edit = (s: string) => s.replace('"via":"cli"', '"via":"web"');
    let r = await verifyExport({
      files: tamper(files, "receipts.jsonl", edit),
      keys: keyOf(files),
    });
    expect(r.ok).toBe(false);
    expect(r.problems[0]).toMatchObject({
      kind: "file",
      path: "receipts.jsonl",
    });
    expect(r.problems[0].detail).toMatch(/changed after the export/);

    r = await verifyExport({
      files: tamper(files, "receipts.jsonl", edit, true),
      keys: keyOf(files),
    });
    expect(r.problems.map((p) => p.kind)).toEqual(["chain", "chain"]);
    expect(r.problems[0]).toMatchObject({ checkpoint: 2 });
    expect(r.problems[0].detail).toBe(
      "checkpoint 2 (receipts 7–13, lines 7–13 of receipts.jsonl): the Merkle root of its receipts differs from the signed one; a receipt in that range was changed, added, removed or reordered",
    );
  });

  it("catches a changed memory file, and a removed receipt", async () => {
    const path = "memories/M-0001.md";
    const edit = (s: string) =>
      s.replace("Postgres-backed. We don't", "Postgres-backed. We do");
    let r = await verifyExport({ files: tamper(files, path, edit) });
    expect(r.problems.map((p) => p.path)).toEqual([path, path]);
    expect(r.problems[0].detail).toMatch(/changed after the export/);
    expect(r.problems[1].detail).toBe(
      "its body isn't the statement of version 2 in its frontmatter",
    );

    // A memory's receipt changed in its own file.
    r = await verifyExport({
      files: tamper(
        files,
        path,
        (s) => s.replace('"action": "kept"', '"action": "edited"'),
        true,
      ),
    });
    expect(r.problems).toHaveLength(1);
    expect(r.problems[0].detail).toMatch(
      /its receipt 0199a1b2-c3d4-7e5f-8a9b-000000000406 says edited \(seq 6, .*\); receipts.jsonl line 6 says kept/,
    );

    // A receipt line removed: the checkpoint covering it fails, and the
    // memory's file lists a receipt that isn't there.
    const lines = text(files, "receipts.jsonl").split("\n");
    const without = new Map(files);
    without.set(
      "receipts.jsonl",
      enc.encode([...lines.slice(0, 3), ...lines.slice(4)].join("\n")),
    );
    reManifest(without, "receipts.jsonl");
    r = await verifyExport({ files: without, keys: keyOf(files) });
    expect(r.ok).toBe(false);
    expect(r.problems.some((p) => p.kind === "chain")).toBe(true);
    expect(r.problems).toContainEqual({
      kind: "record",
      path: path,
      detail:
        "it lists receipt 0199a1b2-c3d4-7e5f-8a9b-000000000404, which isn't in receipts.jsonl",
    });
  });

  it("catches a forged checkpoint, and a key it wasn't signed with", async () => {
    const forged = tamper(
      files,
      "checkpoints.json",
      (s) =>
        s.replace(
          /"merkle_root": "[0-9a-f]+"/,
          `"merkle_root": "${"0".repeat(64)}"`,
        ),
      true,
    );
    let r = await verifyExport({ files: forged, keys: keyOf(files) });
    expect(r.problems.map((p) => p.kind)).toEqual(["chain", "chain"]);
    expect(r.problems[0]).toMatchObject({
      path: "checkpoints.json",
      checkpoint: 1,
    });
    expect(r.problems[0].detail).toMatch(
      /^checkpoint 1 \(receipts 1–6, lines 1–6 of receipts.jsonl\): the signature doesn't verify \(key ed25519:/,
    );

    const other: V2.SigningKey = {
      key_id: "ed25519:0000000000000000",
      algorithm: "ed25519",
      public_key: Buffer.alloc(32, 1).toString("base64"),
    };
    r = await verifyExport({ files, keys: [other] });
    expect(r.ok).toBe(false);
    expect(r.problems.every((p) => p.path === "checkpoints.json")).toBe(true);
    expect(r.problems[0].detail).toMatch(/signed by an unknown key/);
  });

  it("holds a forgotten memory as a tombstone, and notices one missing", async () => {
    const tomb = text(files, "tombstones/M-0004.md");
    expect(tomb).toContain("M-0004 was forgotten on 2026-10-06 09:36 UTC.");
    // The redacted reason stays redacted: only its commitment is there.
    expect(text(files, "receipts.jsonl")).not.toContain("release notes");
    const missing = new Map(files);
    missing.delete("tombstones/M-0004.md");
    reManifest(missing, "tombstones/M-0004.md");
    const r = await verifyExport({ files: missing });
    expect(r.problems.map((p) => p.detail)).toEqual(
      expect.arrayContaining([
        "M-0004 was forgotten, but the export has no tombstone for it",
        "it counts 1 tombstones; the export holds 0",
      ]),
    );
  });

  it("says which files aren't part of the export", async () => {
    const extra = new Map(files);
    extra.set("notes.md", enc.encode("mine\n"));
    const r = await verifyExport({ files: extra });
    expect(r.problems).toEqual([
      {
        kind: "file",
        path: "notes.md",
        detail: "it isn't in export.json, so it wasn't part of the export",
      },
    ]);
  });

  it("downloads an export as a command, with its receipt and name", async () => {
    const zip = new Uint8Array([
      0x50,
      0x4b,
      0x05,
      0x06,
      ...new Array(18).fill(0),
    ]);
    const fetchMock = vi.fn(
      async () =>
        new Response(zip, {
          status: 200,
          headers: {
            "Content-Type": "application/zip",
            "Content-Disposition":
              'attachment; filename="memax-memax-v2-2026-10-07.zip"',
            "X-Memax-Export-Receipt": "0199a1b2-c3d4-7e5f-8a9b-00000000040e",
            "Idempotent-Replayed": "true",
          },
        }),
    );
    const memax = new Memax({
      apiUrl: "https://api.memax.app",
      apiKey: "mxk_test",
      fetch: fetchMock,
      maxRetries: 0,
    });
    const got = await memax.v2.spaces.export("memax-v2", {
      idempotencyKey: "k-1",
      via: "cli",
    });
    expect(got).toEqual({
      bytes: zip,
      filename: "memax-memax-v2-2026-10-07.zip",
      receipt: "0199a1b2-c3d4-7e5f-8a9b-00000000040e",
      replayed: true,
    });
    const [url, init] = fetchMock.mock.calls[0] as unknown as [
      string,
      RequestInit & { headers: Record<string, string> },
    ];
    expect(url).toBe("https://api.memax.app/v2/spaces/memax-v2:export");
    expect(init.method).toBe("POST");
    expect(init.headers).toMatchObject({
      "Idempotency-Key": "k-1",
      "X-Memax-Via": "cli",
      Accept: "application/zip",
    });

    // An archive cut off partway is a network_error, to retry with the key.
    const cut = new Memax({
      apiUrl: "https://api.memax.app",
      apiKey: "mxk_test",
      maxRetries: 0,
      fetch: async () =>
        new Response(
          new ReadableStream({
            start(c) {
              c.enqueue(new Uint8Array([0x50, 0x4b]));
              c.error(new Error("connection reset"));
            },
          }),
          { status: 200, headers: { "Content-Type": "application/zip" } },
        ),
    });
    await expect(
      cut.v2.spaces.export("memax-v2", { idempotencyKey: "k-3" }),
    ).rejects.toMatchObject({ code: "network_error" });

    // A refusal is the usual MemaxError.
    const refused = new Memax({
      apiUrl: "https://api.memax.app",
      apiKey: "mxk_test",
      maxRetries: 0,
      fetch: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: "refused",
              message: "Only a signed-in person exports memax-v2.",
              details: {
                policy: { effect: "refuse", code: "export_by_person" },
              },
            },
          }),
          { status: 403, headers: { "Content-Type": "application/json" } },
        ),
    });
    await expect(
      refused.v2.spaces.export("memax-v2", { idempotencyKey: "k-2" }),
    ).rejects.toMatchObject({ code: "refused", status: 403 });
  });

  it("can't prove words rewritten consistently with export.json", async () => {
    // Words aren't in receipts (so Forget can remove them): a statement
    // rewritten in its body, its version and export.json still verifies.
    // The receipts prove which memories existed, their versions, when and
    // by whom; the words, only as far as export.json is trusted.
    const swapped = tamper(
      files,
      "memories/M-0002.md",
      (s) => s.replaceAll("on Temporal.", "on Sidekiq."),
      true,
    );
    expect((await verifyExport({ files: swapped })).ok).toBe(true);
  });
});
