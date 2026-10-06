import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The TypeScript side of "every colour is a token": no colour literal in any
// component or preview (inline styles included).

const SRC = join(dirname(fileURLToPath(import.meta.url)), "..");

function walk(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return walk(path);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)
      ? [path]
      : [];
  });
}

describe("component sources", () => {
  const files = walk(SRC);

  it("are all scanned", () => {
    expect(files.length).toBeGreaterThan(30);
  });

  it("contain no colour literals", () => {
    const offenders: string[] = [];
    for (const file of files) {
      const source = readFileSync(file, "utf8");
      const lines = source.split("\n");
      lines.forEach((line, i) => {
        const code = line.replace(/\/\/.*$/, "");
        if (
          /["'`]#[0-9a-fA-F]{3,8}["'`]/.test(code) ||
          /\b(rgba?|hsla?|oklch|oklab)\(/.test(code)
        ) {
          offenders.push(`${relative(SRC, file)}:${i + 1}: ${line.trim()}`);
        }
      });
    }
    expect(offenders).toEqual([]);
  });

  it("set no colour or font in inline styles", () => {
    const offenders: string[] = [];
    for (const file of files) {
      const source = readFileSync(file, "utf8");
      for (const m of source.matchAll(/style=\{\{([^}]*)\}\}/g)) {
        if (
          /\b(color|background|fill|stroke|border|font|fontFamily)\b/.test(
            m[1] ?? "",
          )
        ) {
          offenders.push(`${relative(SRC, file)}: style={{${m[1]}}}`);
        }
      }
    }
    expect(offenders).toEqual([]);
  });
});
