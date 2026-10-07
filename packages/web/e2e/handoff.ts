import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import path from "node:path";

/**
 * Where the V2 handoff's component PNGs are: the private sibling
 * checkout `memax-internal` (AGENTS.md, "Design Documents"). They are
 * never copied into this public repo; the comparison reads them in
 * place and skips when the checkout isn't there (CI, contributors).
 *
 * Looks in MEMAX_INTERNAL_DIR, then beside this repo, then beside the
 * main checkout when this is a git worktree.
 */
const PREVIEWS = ["docs", "v2", "handoff", "design-system", "previews"];

function repoRoots(): string[] {
  // Playwright loads the config and specs as CommonJS.
  const webRoot = path.resolve(__dirname, "..");
  const roots = [path.resolve(webRoot, "../..")];
  try {
    const common = execFileSync(
      "git",
      ["rev-parse", "--path-format=absolute", "--git-common-dir"],
      { cwd: webRoot, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] },
    ).trim();
    roots.push(path.dirname(common));
  } catch {
    // Not a git checkout: the env var or the plain sibling path only.
  }
  return roots;
}

export function handoffPreviewsDir(): string | null {
  const candidates = [
    ...(process.env.MEMAX_INTERNAL_DIR ? [process.env.MEMAX_INTERNAL_DIR] : []),
    ...repoRoots().map((root) => path.resolve(root, "..", "memax-internal")),
  ].map((dir) => path.join(dir, ...PREVIEWS));
  return candidates.find((dir) => existsSync(dir)) ?? null;
}

/** Where the handoff's screen boards are (screens/png), beside the previews. */
export function handoffScreensDir(): string | null {
  const previews = handoffPreviewsDir();
  if (!previews) return null;
  const dir = path.resolve(previews, "..", "..", "screens", "png");
  return existsSync(dir) ? dir : null;
}

export const HANDOFF_MISSING =
  "The handoff PNGs aren't here: clone MemaxLabs/memax-internal beside this repo (or set MEMAX_INTERNAL_DIR) to compare the components with the design.";
