import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { describe, expect, it } from "vitest";

// The web app keeps no token where page script can read it (the BFF:
// lib/bff, and packages/server/internal/websurface/THREAT_MODEL.md). This
// holds it so in the source of both trees, V1 and the Ledger, and in the
// built client bundle when there is one (after `next build` or
// `build:cf`).

const WEB = join(import.meta.dirname, "..", "..");
const SRC = join(WEB, "src");

const LEGACY_KEYS = [
  "memax_access_token",
  "memax_refresh_token",
  "memax_original_access_token",
  "memax_original_refresh_token",
];
// The one module that names them, to delete them unread.
const LEGACY_MODULE = join("src", "lib", "legacy-tokens.ts");
// The server side of the BFF: route handlers and lib/bff run only on the
// web app's server, where the tokens live.
const SERVER_ONLY = [
  join("src", "app", "api") + sep,
  join("src", "lib", "bff") + sep,
];

function files(dir: string, ext: RegExp): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...files(path, ext));
    else if (ext.test(name)) out.push(path);
  }
  return out;
}

const source = files(SRC, /\.(ts|tsx)$/)
  .filter((f) => !/\.test\.tsx?$/.test(f))
  .map((f) => ({ path: relative(WEB, f), text: readFileSync(f, "utf8") }));

describe("no token where page script can read it", () => {
  it("names the old localStorage token keys in one module only, which never reads or writes them", () => {
    for (const f of source) {
      if (f.path === LEGACY_MODULE) continue;
      for (const key of LEGACY_KEYS) {
        expect(f.text.includes(key), `${f.path} names ${key}`).toBe(false);
      }
    }
    const legacy = source.find((f) => f.path === LEGACY_MODULE)!.text;
    expect(legacy).toMatch(/removeItem/);
    expect(legacy).not.toMatch(
      /getItem|setItem|\[\s*key\s*\]\s*=|localStorage\.\w+\s*=/,
    );
  });

  it("keeps nothing token-like in localStorage or sessionStorage", () => {
    const access =
      /(localStorage|sessionStorage)\.(getItem|setItem)\(\s*([^)]*)\)/g;
    for (const f of source) {
      for (const m of f.text.matchAll(access)) {
        expect(m[3], `${f.path}: ${m[0]}`).not.toMatch(
          /token|refresh|bearer|jwt|credential|secret/i,
        );
      }
    }
  });

  it("builds no Authorization header in page code", () => {
    for (const f of source) {
      if (SERVER_ONLY.some((dir) => f.path.startsWith(dir))) continue;
      // The admin client and the SDK client send none: the proxy does.
      expect(f.text, f.path).not.toMatch(/Bearer \$\{/);
      expect(f.text, f.path).not.toMatch(/getAccessToken|apiAuthHeaders/);
    }
  });

  const built = [
    join(WEB, ".next", "static"),
    join(WEB, ".open-next", "assets", "_next", "static"),
  ].filter(existsSync);

  it.skipIf(built.length === 0)(
    "ships no token reads or writes in the built client bundle",
    () => {
      for (const dir of built) {
        for (const file of files(dir, /\.js$/)) {
          const js = readFileSync(file, "utf8");
          for (const key of LEGACY_KEYS) {
            let at = js.indexOf(key);
            while (at >= 0) {
              // The only use is the array forgetLegacyTokens deletes.
              const around = js.slice(Math.max(0, at - 200), at + 300);
              expect(around, `${relative(WEB, file)} near ${key}`).not.toMatch(
                /getItem|setItem/,
              );
              at = js.indexOf(key, at + key.length);
            }
          }
        }
      }
    },
  );
});
