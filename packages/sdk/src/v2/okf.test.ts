// The export as an Open Knowledge Format v0.2 bundle, from the golden
// export the Go writer produces: OKF's conformance (every concept has
// frontmatter with a `type`), its fields from the record, the Brief as
// the index, the log from the receipts, tombstones without words, and the
// same bytes every time.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { parseExport, parseFrontmatter, splitDocument } from "./export.js";
import { OKF_VERSION, titleOf, toOkf } from "./okf.js";

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

const bundle = () => toOkf(parseExport(load(golden)), "memax-v2");
const front = (doc: string, path: string) =>
  parseFrontmatter(splitDocument(doc, path).front, path);

describe("an export as an OKF bundle", () => {
  it("is conformant: every concept has frontmatter with a type", () => {
    const b = bundle();
    expect([...b.keys()]).toEqual([
      "index.md",
      "log.md",
      "memories/M-0001.md",
      "memories/M-0002.md",
      "memories/M-0003.md",
      "memories/M-0005.md",
      "tombstones/M-0004.md",
    ]);
    for (const [path, doc] of b) {
      if (path === "index.md" || path === "log.md") continue; // reserved
      const f = front(doc, path);
      expect(typeof f.type === "string" && f.type.length > 0, path).toBe(true);
    }
    expect(front(b.get("index.md")!, "index.md")).toEqual({
      okf_version: OKF_VERSION,
    });
  });

  it("says a kept decision in OKF's own fields, and keeps the rest under memax", () => {
    const doc = bundle().get("memories/M-0001.md")!;
    const f = front(doc, "M-0001");
    expect(f).toMatchObject({
      type: "decision",
      title: "Background jobs run on River, Postgres-backed",
      status: "stable",
      generated: { by: expect.stringMatching(/^(human:|process:|[a-z])/) },
      sources: [
        {
          resource: "https://github.com/MemaxLabs/memax/pull/212",
          title: "PR #212",
        },
        { resource: "go.mod#L14", title: "go.mod:14" },
      ],
      memax: {
        format: "memax.memory.v1",
        ref: "M-0001",
        lifecycle: "kept",
        trust: "person",
      },
    });
    // Kept by a person: OKF reads it as human-reviewed.
    expect(
      (f.verified as Array<{ by: string }>).some((v) =>
        v.by.startsWith("human:"),
      ),
    ).toBe(true);
    expect(splitDocument(doc, "M-0001").body).toBe(
      `Background jobs run on River, Postgres-backed. We don't use Temporal ("one queue" & <one> DB).\n`,
    );
  });

  it("maps the lifecycle to OKF's status: superseded and proposed", () => {
    const b = bundle();
    expect(front(b.get("memories/M-0002.md")!, "M-0002").status).toBe(
      "deprecated",
    );
    const proposal = front(b.get("memories/M-0003.md")!, "M-0003");
    expect(proposal.status).toBe("draft");
    expect(proposal.verified).toBeUndefined(); // nobody kept it
  });

  it("keeps a forgotten memory as a tombstone, with no words", () => {
    const b = bundle();
    const t = b.get("tombstones/M-0004.md")!;
    expect(front(t, "M-0004")).toMatchObject({
      type: "tombstone",
      status: "deprecated",
      memax: { format: "memax.tombstone.v1", ref: "M-0004" },
    });
    expect(b.has("memories/M-0004.md")).toBe(false);
    // The index lists it nowhere, and nothing says what it said.
    expect(b.get("index.md")).not.toContain("M-0004");
  });

  it("makes the Brief the index, and the receipts the log, newest first", () => {
    const b = bundle();
    const index = splitDocument(b.get("index.md")!, "index.md").body;
    expect(index).toContain("# memax-v2");
    expect(index).toContain("## Decisions");
    expect(index).toContain(
      "- [Background jobs run on River, Postgres-backed](memories/M-0001.md)",
    );
    expect(index).toContain("## Waiting for review");
    const log = b.get("log.md")!;
    expect(log.startsWith("# Log\n\n## 2026-10-06\n")).toBe(true);
    expect(log).toMatch(/- kept M-0001 · human:/);
    const kept = log.indexOf("- kept M-0001");
    const edited = log.indexOf("- edited M-0001");
    expect(edited).toBeGreaterThan(-1);
    expect(edited).toBeLessThan(kept); // newest first
  });

  it("is the same bytes for the same export", () => {
    expect([...bundle()]).toEqual([...bundle()]);
  });
});

describe("a concept's title", () => {
  it("is the first clause, cut on a word under 80 characters", () => {
    expect(titleOf("Use pnpm. Never npm.")).toBe("Use pnpm");
    expect(titleOf("a".repeat(30) + " " + "b".repeat(60))).toBe(
      "a".repeat(30) + " " + "b".repeat(49) + "…",
    );
    expect(
      titleOf("x".repeat(30) + " " + "y".repeat(30) + " " + "z".repeat(30)),
    ).toBe("x".repeat(30) + " " + "y".repeat(30) + "…");
  });
});
