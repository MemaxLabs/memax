import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The CLI's version, from its package.json. */
export function cliVersion(): string {
  try {
    const here = dirname(fileURLToPath(import.meta.url));
    const pkg = JSON.parse(
      readFileSync(join(here, "..", "..", "package.json"), "utf8"),
    ) as { version: string };
    return pkg.version;
  } catch {
    return "unknown";
  }
}
