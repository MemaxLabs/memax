/**
 * Golden files: each `fixtures/<case>/input.json` compiles to exactly the
 * files in `fixtures/<case>/expected/`.
 *
 * - File paths are flattened (`/` becomes `__`), because the repository
 *   ignores `.claude/` directories and fixtures must be committed.
 * - Copy-out text is stored as `copy__<label>.txt`.
 * - `result.json` holds the result without file contents.
 * - Prettier skips fixtures (see `.prettierrc.json`), so the bytes stay exact;
 *   `result.json` is written already formatted.
 *
 * Regenerate with `UPDATE_GOLDEN=1 pnpm test`, then review the diff.
 */
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { format } from "prettier";
import { describe, expect, it } from "vitest";
import { compile, type CompileResult } from "../src/index.js";
import { FIXTURES, loadInput } from "./helpers.js";

const UPDATE = process.env.UPDATE_GOLDEN === "1";

function goldenName(path: string): string {
  return path.replaceAll("/", "__");
}

function copyName(label: string): string {
  return `copy__${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}.txt`;
}

/** Everything in the result except the contents, which live in their own files. */
function summary(result: CompileResult): unknown {
  return {
    ...result,
    files: result.files.map(({ content: _content, ...rest }) => rest),
    copies: result.copies.map(({ content: _content, ...rest }) => rest),
  };
}

const cases = readdirSync(FIXTURES, { withFileTypes: true })
  .filter((d) => d.isDirectory())
  .map((d) => d.name)
  .sort();

describe("golden files", () => {
  it("has fixtures", () => {
    expect(cases.length).toBeGreaterThan(0);
  });

  for (const name of cases) {
    it(name, async () => {
      const result = compile(loadInput(name));
      const dir = join(FIXTURES, name, "expected");
      const actual = new Map<string, string>();
      for (const f of result.files) actual.set(goldenName(f.path), f.content);
      for (const c of result.copies) actual.set(copyName(c.label), c.content);
      const resultJson = await format(JSON.stringify(summary(result)), {
        parser: "json",
      });

      if (UPDATE) {
        rmSync(dir, { recursive: true, force: true });
        mkdirSync(dir, { recursive: true });
        for (const [file, content] of actual)
          writeFileSync(join(dir, file), content);
        writeFileSync(join(dir, "result.json"), resultJson);
        return;
      }

      expect(
        existsSync(dir),
        `${name} has no expected/ (run with UPDATE_GOLDEN=1)`,
      ).toBe(true);
      const expectedFiles = readdirSync(dir)
        .filter((f) => f !== "result.json")
        .sort();
      expect(expectedFiles).toEqual([...actual.keys()].sort());
      for (const [file, content] of actual) {
        expect(content, `${name}/${file}`).toBe(
          readFileSync(join(dir, file), "utf8"),
        );
      }
      expect(summary(result)).toEqual(
        JSON.parse(readFileSync(join(dir, "result.json"), "utf8")),
      );
    });
  }
});
