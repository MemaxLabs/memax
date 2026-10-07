/* eslint-disable no-console -- a measuring script prints its table */
// What one compile or parse-back costs, against the Workers limits (128 MB
// of memory per isolate, and the CPU time a request may use: wrangler.jsonc's
// limits.cpu_ms). Each case runs in Node (CPU time from process.cpuUsage,
// and the smallest V8 heap it completes in, in a fresh process) and under
// workerd through Wrangler (wall time per request, which bounds the CPU
// time from above).
//
//   pnpm --filter @memaxlabs/compiler build
//   pnpm --filter @memaxlabs/compile-service build
//   node packages/compile-service/scripts/measure.mjs
//
// The cases: every golden fixture; the largest compile input the service
// accepts (just under MAX_BODY_BYTES, 4 MiB: memories of the longest
// statement the compiler takes, half of them path-scoped, every adapter);
// and parse-backs of 1 MiB files (the API's MaxObservedBytes; an accepted
// observation can be the baseline, so both sides can be that big) and of
// 2 MiB files (what the body limit alone lets through): a realistic edit,
// and the slowest shape the diff has, every cited line gone and as many
// new uncited ones.
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
const fixtures = join(root, "../compiler/test/fixtures");
const MAX = 4 << 20;
const TOKEN = "measure";

const KINDS = [
  "agents_md",
  "claude_md",
  "cursor_mdc",
  "chatgpt",
  "gemini_md",
  "copilot",
  "windsurf",
  "claude_rules",
];
const SECTIONS = ["decisions", "conventions", "open"];

function maxCompileInput() {
  const memory = (i) => ({
    ref: `M-${String(i).padStart(5, "0")}`,
    statement:
      `Fact ${i}: ${"keep the record small and cite it ".repeat(60)}`.slice(
        0,
        1999,
      ) + ".",
    section: SECTIONS[i % 3],
    kind: i % 3 === 0 ? "decision" : "fact",
    state: "kept",
    read_score: i % 97,
    ...(i % 2 === 0 ? { scope: { paths: [`packages/p${i % 40}/**`] } } : {}),
  });
  const build = (n) => {
    const memories = Array.from({ length: n }, (_, i) => memory(i + 1));
    return JSON.stringify({
      version: 1,
      compile: { id: "C-9999", at: "2026-10-07T12:00:00Z" },
      space: {
        slug: "measure",
        name: "Measure",
        kind: "project",
        url: "https://memax.app/measure/brief",
      },
      brief: {
        id: "B-9999",
        title: "The largest Brief the service takes",
        sections: SECTIONS.map((key) => ({
          key,
          heading: key[0].toUpperCase() + key.slice(1),
          items: memories
            .filter((m) => m.section === key)
            .map((m) => ({ ref: m.ref })),
        })),
      },
      memories,
      targets: KINDS.map((kind) => ({ kind })),
    });
  };
  // As many memories as fit under the limit.
  const per = build(101).length - build(100).length;
  let n = Math.floor((MAX - build(0).length) / per);
  while (build(n).length > MAX - 1024) n--;
  return build(n);
}

function parseBackInput(mib, current) {
  const n = Math.floor((mib << 20) / 72);
  const lines = (f) => Array.from({ length: n }, (_, i) => f(i)).join("\n");
  const cited = lines(
    (i) => `- Line ${i} keeps a statement of a typical length. [M-${i + 1}]`,
  );
  const next =
    current === "edited"
      ? lines(
          (i) =>
            `- Line ${i} keeps a statement of a typical length, edited. [M-${i + 1}]`,
        )
      : lines((i) => `- ${i} A new line someone wrote by hand without a cite.`);
  return JSON.stringify({ last: cited, current: next });
}

const cases = [
  ...readdirSync(fixtures)
    .sort()
    .map((name) => ({
      name: `golden ${name}`,
      path: "/compile",
      make: () => readFileSync(join(fixtures, name, "input.json"), "utf8"),
    })),
  { name: "max compile input", path: "/compile", make: maxCompileInput },
  ...[1, 2].flatMap((mib) => [
    {
      name: `parse-back ${mib} MiB, every line edited`,
      path: "/parse-back",
      make: () => parseBackInput(mib, "edited"),
    },
    {
      name: `parse-back ${mib} MiB, every cite gone`,
      path: "/parse-back",
      make: () => parseBackInput(mib, "uncited"),
    },
  ]),
];

const fmt = (n) => (n < 10 ? n.toFixed(2) : n.toFixed(0));
const median = (xs) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)];

function request(base, path, body) {
  return new Request(base + path, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${TOKEN}`,
    },
    body,
  });
}

const { createHandler } = await import("../dist/handler.js");

// A child process: run one case once, in a heap of the size it was given.
if (process.argv[2] === "--child") {
  const c = cases[Number(process.argv[3])];
  const res = await createHandler({ token: TOKEN })(
    request("http://localhost", c.path, c.make()),
  );
  await res.arrayBuffer();
  process.exit(res.status === 200 ? 0 : 3);
}

const handler = createHandler({ token: TOKEN });
const bodies = cases.map((c) => c.make());
const rows = [];
for (const [i, c] of cases.entries()) {
  const cpu = [];
  let status = 0;
  for (let run = 0; run < 7; run++) {
    const req = request("http://localhost", c.path, bodies[i]);
    const before = process.cpuUsage();
    const res = await handler(req);
    await res.arrayBuffer();
    const used = process.cpuUsage(before);
    status = res.status;
    if (run >= 2) cpu.push((used.user + used.system) / 1000);
  }
  let heap = null;
  for (const mb of [16, 24, 32, 48, 64, 96, 128]) {
    try {
      // A heap just too small can thrash rather than fail: give up on a
      // size after a minute.
      execFileSync(
        process.execPath,
        [
          `--max-old-space-size=${mb}`,
          fileURLToPath(import.meta.url),
          "--child",
          String(i),
        ],
        { stdio: "ignore", timeout: 60_000 },
      );
      heap = mb;
      break;
    } catch {
      // Out of memory at this size: try the next.
    }
  }
  rows.push({
    case: c.name,
    status,
    body_kib: Math.round(Buffer.byteLength(bodies[i]) / 1024),
    node_cpu_ms: `${fmt(median(cpu))} (max ${fmt(Math.max(...cpu))})`,
    min_heap_mb: heap === null ? "over 128" : `≤ ${heap}`,
  });
}

const { unstable_startWorker } = await import("wrangler");
const worker = await unstable_startWorker({
  config: join(root, "wrangler.jsonc"),
  bindings: { COMPILE_SERVICE_TOKEN: { type: "secret_text", value: TOKEN } },
  dev: {
    server: { hostname: "127.0.0.1", port: 0 },
    inspector: false,
    watch: false,
    persist: false,
    logLevel: "none",
  },
});
await worker.ready;
const base = (await worker.url).origin;
for (const [i, c] of cases.entries()) {
  const wall = [];
  for (let run = 0; run < 7; run++) {
    const started = performance.now();
    const res = await fetch(request(base, c.path, bodies[i]));
    await res.arrayBuffer();
    if (res.status !== 200) throw new Error(`${c.name}: workerd ${res.status}`);
    if (run >= 2) wall.push(performance.now() - started);
  }
  rows[i].workerd_wall_ms =
    `${fmt(median(wall))} (max ${fmt(Math.max(...wall))})`;
}
await worker.dispose();
console.table(rows);
