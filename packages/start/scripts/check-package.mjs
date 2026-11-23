// Check the artifact users install, including the exact tarball a release
// publishes. No runtime packages may enter this zero-dependency command.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import {
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const pkgDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = JSON.parse(readFileSync(join(pkgDir, "package.json"), "utf8"));
const temp = mkdtempSync(join(tmpdir(), "memax-start-package-"));

try {
  let archive = process.argv[2] && resolve(process.argv[2]);
  if (!archive) {
    execFileSync("pnpm", ["pack", "--pack-destination", temp], {
      cwd: pkgDir,
      stdio: "pipe",
    });
    const archives = readdirSync(temp).filter((p) => p.endsWith(".tgz"));
    assert.equal(archives.length, 1, "pack must produce exactly one tarball");
    archive = join(temp, archives[0]);
  }
  const files = execFileSync("tar", ["-tzf", archive], { encoding: "utf8" })
    .trim()
    .split("\n");
  assert.deepEqual(files.sort(), [
    "package/LICENSE",
    "package/NOTICE",
    "package/README.md",
    "package/dist/memax-start.js",
    "package/package.json",
  ]);
  const packed = JSON.parse(
    execFileSync("tar", ["-xOzf", archive, "package/package.json"], {
      encoding: "utf8",
    }),
  );
  assert.equal(packed.name, "@memaxlabs/start");
  assert.equal(packed.version, source.version);
  assert.notEqual(packed.private, true);
  assert.equal(packed.publishConfig?.access, "public");
  assert.equal(packed.license, "Apache-2.0");
  assert.deepEqual(packed.bin, { "memax-start": "./dist/memax-start.js" });
  for (const key of [
    "dependencies",
    "optionalDependencies",
    "peerDependencies",
    "bundledDependencies",
    "bundleDependencies",
  ])
    assert.equal(Object.keys(packed[key] ?? {}).length, 0, `${key} must be empty`);

  // Installation must work without workspace links or any lifecycle script.
  writeFileSync(join(temp, "package.json"), '{"private":true}\n');
  execFileSync(
    "npm",
    ["install", "--ignore-scripts", "--no-audit", "--no-fund", archive],
    { cwd: temp, stdio: "pipe" },
  );
  const modules = join(temp, "node_modules");
  assert.deepEqual(readdirSync(modules).filter((p) => !p.startsWith(".")), [
    "@memaxlabs",
  ]);
  assert.deepEqual(readdirSync(join(modules, "@memaxlabs")), ["start"]);
  const bin = join(modules, ".bin", "memax-start");
  assert.ok(statSync(bin).mode & 0o111, "installed command must be executable");
  const help = execFileSync(bin, ["--help"], {
    cwd: temp,
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.match(help, /memax-start --agent/);
  assert.match(help, /MEMAX_START_KEY/);
  assert.equal(
    execFileSync(bin, ["--agent", "copilot"], {
      cwd: temp,
      input: "{}",
      encoding: "utf8",
      timeout: 10_000,
    }),
    "",
    "a start outside a repository must print nothing",
  );
  console.log(
    `${packed.name}@${packed.version}: packed file list, zero runtime dependencies and installed command passed`,
  );
} finally {
  rmSync(temp, { recursive: true, force: true });
}
