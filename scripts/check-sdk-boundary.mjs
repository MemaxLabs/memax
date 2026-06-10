import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import process from "node:process";

const ROOTS = ["packages/web"];
const SOURCE_EXTENSIONS = new Set([".ts", ".tsx", ".js", ".jsx"]);
const ignoredPathSnippets = [
  "packages/web/src/app/dev/kitchen/",
  "packages/web/src/lib/dev-mocks.ts",
  // Admin endpoints intentionally bypass the public SDK. They are internal
  // operator tools with a different auth model (JWT session + admin_roles).
  "packages/web/src/hooks/use-admin-",
  "packages/web/src/lib/admin-client/",
  "packages/web/src/app/(admin)/",
  // Waitlist signup + invite validation are public endpoints that don't require
  // auth. They predate the user's account, so no API key/session can exist yet.
  "packages/web/src/app/(marketing)/waitlist/",
  "packages/web/src/app/(auth)/register/",
  "packages/web/src/components/landing/hero-waitlist.tsx",
];

function walk(dir) {
  const files = [];
  for (const entry of readdirSync(dir)) {
    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      if (entry === "node_modules" || entry === "dist" || entry === ".next") {
        continue;
      }
      files.push(...walk(fullPath));
      continue;
    }
    if (!SOURCE_EXTENSIONS.has(fullPath.slice(fullPath.lastIndexOf(".")))) {
      continue;
    }
    files.push(fullPath);
  }
  return files;
}

function shouldIgnore(path) {
  return ignoredPathSnippets.some((snippet) => path.includes(snippet));
}

function checkWebBoundary() {
  const violations = [];
  for (const root of ROOTS) {
    for (const file of walk(root)) {
      const relativePath = relative(process.cwd(), file).replaceAll("\\", "/");
      if (shouldIgnore(relativePath)) continue;

      const lines = readFileSync(file, "utf-8").split("\n");
      lines.forEach((line, index) => {
        if (!line.includes("/v1/")) return;
        violations.push(`${relativePath}:${index + 1}:${line.trim()}`);
      });
    }
  }
  return violations;
}

function main() {
  const violations = checkWebBoundary();
  if (violations.length === 0) {
    console.log("SDK boundary check passed.");
    return;
  }

  console.error("SDK boundary violations (web -> /v1):");
  for (const violation of violations) console.error(`- ${violation}`);
  console.error("");
  console.error("Use memax-sdk for product /v1 backend calls in packages/web.");
  console.error(
    "Admin calls go through @/lib/admin-client (web-only), never the SDK.",
  );
  process.exit(1);
}

main();
