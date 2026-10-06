// The CLI carries its own drift hash and a copy of the compiler's managed
// block (memax-cli can't depend on the unpublished compiler yet). These
// checks hold both to the compiler, on its golden fixtures and on random
// files, so the daemon and the server never disagree about a hash.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import * as compiler from "../../../compiler/src/index.js";
import * as copy from "../../src/lib/daemon/compiler/managed-block.js";
import {
  blockState,
  driftHash,
  managedInner,
  manifestSha256,
} from "../../src/lib/daemon/compiler/drift.js";
import { manifest } from "./fake-v2.js";

const fixtures = join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "compiler",
  "test",
  "fixtures",
);

function fixtureFiles(): Array<[string, string]> {
  const out: Array<[string, string]> = [];
  for (const dir of readdirSync(fixtures)) {
    let expected: string[] = [];
    try {
      expected = readdirSync(join(fixtures, dir, "expected"));
    } catch {
      continue;
    }
    for (const f of expected) {
      if (f.endsWith(".json")) continue;
      out.push([
        `${dir}/${f}`,
        readFileSync(join(fixtures, dir, "expected", f), "utf8"),
      ]);
    }
  }
  return out;
}

// Deterministic random text with the characters that matter here.
function* randomFiles(n: number): Generator<string> {
  let seed = 7;
  const rnd = () =>
    (seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff;
  const parts = [
    "# Brief",
    "- a fact [M-0219]",
    "",
    "  ",
    "<!-- memax:start -->",
    "<!-- memax:end -->",
    "@AGENTS.md",
    "\u{FEFF}",
    "é",
    "\u{202E}",
  ];
  const eols = ["\n", "\r\n", "\r", ""];
  for (let i = 0; i < n; i++) {
    let s = "";
    const lines = Math.floor(rnd() * 8);
    for (let j = 0; j < lines; j++)
      s +=
        parts[Math.floor(rnd() * parts.length)] +
        eols[Math.floor(rnd() * eols.length)];
    yield s;
  }
}

describe("the CLI's compiler copy", () => {
  it("hashes every golden fixture as the compiler does", () => {
    const files = fixtureFiles();
    expect(files.length).toBeGreaterThan(5);
    for (const [, content] of files)
      expect(driftHash(content)).toBe(compiler.driftHash(content));
  });

  it("agrees with the compiler on random files", () => {
    const inner = ["<!-- header -->", "@AGENTS.md"];
    for (const content of randomFiles(2_000)) {
      expect(driftHash(content)).toBe(compiler.driftHash(content));
      let theirs: string | Error;
      try {
        theirs = compiler.upsertManagedBlock(content, inner);
      } catch (e) {
        theirs = e as Error;
      }
      if (theirs instanceof Error) {
        expect(() => copy.upsertManagedBlock(content, inner)).toThrow(
          copy.ManagedBlockError,
        );
        expect(blockState(content)).toBe("broken");
        continue;
      }
      expect(copy.upsertManagedBlock(content, inner)).toBe(theirs);
      expect(copy.removeManagedBlock(content)).toBe(
        compiler.removeManagedBlock(content),
      );
      expect(copy.extractManagedBlock(content)).toBe(
        compiler.extractManagedBlock(content),
      );
      expect(blockState(content)).toBe(
        compiler.findManagedBlock(content) ? "present" : "none",
      );
      expect(compiler.isDrifted(driftHash(content), content)).toBe(false);
    }
  });

  it("reads back a block's lines as the compiler wrote them", () => {
    const inner = [
      "<!-- Compiled by Memax -->",
      "@AGENTS.md",
      "",
      "- Claude only. [M-1]",
    ];
    const file = compiler.upsertManagedBlock("# Mine\r\n", inner);
    expect(managedInner(file)).toEqual(inner);
    expect(managedInner("# none\n")).toBeNull();
  });

  it("computes the server's manifest hash", () => {
    const one = { "AGENTS.md": "a".repeat(64) };
    expect(manifestSha256(one)).toBe("a".repeat(64));
    const two = {
      "b/memax-x.mdc": "1".repeat(64),
      "a/memax-y.mdc": "2".repeat(64),
    };
    expect(manifestSha256(two)).toBe(manifest(two));
    expect(manifestSha256({})).toBe(compiler.sha256Hex(""));
  });
});
