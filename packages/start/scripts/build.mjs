// Builds dist/memax-start.js: src/main.ts and the CLI's cloud-start code
// it imports, bundled into one ES module for Node 18 and up. The build
// fails if anything but this repository's own sources went in, or if the
// bundle imports anything but Node's built-ins: the package must have no
// dependencies (cloud warm-start design §11, supply chain).
//
//   node scripts/build.mjs [--outfile <path>]
import { readFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const here = dirname(fileURLToPath(import.meta.url));
const pkgDir = join(here, "..");
const repo = join(pkgDir, "..", "..");
const pkg = JSON.parse(readFileSync(join(pkgDir, "package.json"), "utf8"));
const at = process.argv.indexOf("--outfile");
const outfile =
  at >= 0 ? process.argv[at + 1] : join(pkgDir, "dist", "memax-start.js");

const result = await build({
  entryPoints: [join(pkgDir, "src", "main.ts")],
  outfile,
  bundle: true,
  platform: "node",
  format: "esm",
  target: "node18",
  banner: { js: "#!/usr/bin/env node" },
  define: { MEMAX_START_VERSION: JSON.stringify(pkg.version) },
  legalComments: "none",
  // Readable, so anyone can read what their hook runs.
  minify: false,
  metafile: true,
  logLevel: "warning",
});

const allowed = [join(pkgDir, "src"), join(repo, "packages", "cli", "src")].map(
  (d) => relative(process.cwd(), d),
);
const problems = [];
for (const input of Object.keys(result.metafile.inputs)) {
  if (!allowed.some((d) => input === d || input.startsWith(`${d}/`)))
    problems.push(`bundled ${input}, which isn't this repository's own source`);
}
for (const out of Object.values(result.metafile.outputs)) {
  for (const imp of out.imports) {
    if (!imp.external || !imp.path.startsWith("node:"))
      problems.push(`imports ${imp.path}, which isn't a Node built-in`);
  }
}
if (problems.length > 0) {
  console.error(`@memaxlabs/start: ${problems.join("\n")}`);
  process.exit(1);
}
const bytes = Object.values(result.metafile.outputs)[0]?.bytes ?? 0;
console.log(
  `@memaxlabs/start ${pkg.version}: ${relative(process.cwd(), outfile)}, ${Math.round(bytes / 1024)} KiB`,
);
