// memax export and memax verify-export (plan 25 §7.2, rule 14).
//
// `memax export [--space] [--out dir]` takes a space's whole record (every
// V2 space of yours without --space) as the export format, memax.export.v1:
// Markdown with frontmatter, the receipts and the signed checkpoints, one
// folder per space. Each export is one receipt on the space.
//
// `memax verify-export <dir>` checks one: every file against export.json,
// the receipt chain from the first receipt against every signed
// checkpoint, and the memory files against the receipts (the SDK's
// verifyExport). It trusts the keys you pin with --key, else the server's
// published keys, else (offline) the keys the export carries, and says
// which it used.
import { createHash, randomUUID } from "node:crypto";
import { resolve } from "node:path";
import chalk from "chalk";
import type { Command } from "commander";
import {
  MemaxError,
  verifyExport,
  type ExportReport,
  type Memax,
  type V2,
} from "memax-sdk";
import { getClient } from "../lib/client.js";
import { loadConfig } from "../lib/config.js";
import {
  ExportFilesError,
  exportFolder,
  readExport,
  readZip,
  writeExport,
} from "../lib/export-files.js";
import { findSpace } from "../lib/v2-space.js";
import { apiFailureMessage, MARK } from "./v2-output.js";

export interface ExportOptions {
  space?: string;
  out?: string;
  force?: boolean;
  format?: string;
}

export interface ExportDeps {
  memax: Memax;
  cwd: string;
  out: (line: string) => void;
}

interface ExportedSpace {
  space: string;
  dir: string;
  receipt: string;
  files: number;
  memories: number;
  tombstones: number;
  receipts: number;
  sealed: number;
  unsealed: number;
}

const fmt = (n: number) => n.toLocaleString("en-US");
const plural = (n: number, one: string, many: string) =>
  `${fmt(n)} ${n === 1 ? one : many}`;

/** Which spaces to export: the one named, or every V2 space of yours. */
function chooseSpaces(spaces: V2.Space[], key?: string): V2.Space[] {
  if (key) {
    const sp = findSpace(spaces, key);
    if (!sp) {
      throw new ExportFilesError(
        `${key} isn't one of your spaces. Your spaces: ${spaces.map((s) => s.slug).join(", ") || "none yet"}.`,
      );
    }
    return [sp];
  }
  const v2 = spaces.filter((s) => s.v2_enabled_at);
  if (v2.length === 0) {
    throw new ExportFilesError(
      "None of your spaces is on the V2 record yet, so there is nothing to export. Name one with --space <slug>.",
    );
  }
  return v2;
}

export async function exportSpaces(
  o: ExportOptions,
  d: ExportDeps,
): Promise<number> {
  const json = o.format === "json";
  const spaces = chooseSpaces((await d.memax.v2.spaces.list()).items, o.space);
  const outDir = o.out ?? "memax-export";
  const done: ExportedSpace[] = [];
  for (const sp of spaces) {
    // One key per export, reused if the download is retried.
    const idempotencyKey = randomUUID();
    let got: Awaited<ReturnType<Memax["v2"]["spaces"]["export"]>>;
    try {
      got = await d.memax.v2.spaces.export(sp.slug, {
        idempotencyKey,
        via: "cli",
      });
    } catch (err) {
      if (!(err instanceof MemaxError) || err.code !== "network_error")
        throw err;
      got = await d.memax.v2.spaces.export(sp.slug, {
        idempotencyKey,
        via: "cli",
      });
    }
    const { root, files } = exportFolder(readZip(got.bytes));
    if (root !== sp.slug) {
      throw new ExportFilesError(
        `The archive for ${sp.slug} holds the folder ${root}. Nothing was written.`,
      );
    }
    const dir = `${outDir.replace(/\/+$/, "")}/${sp.slug}`;
    writeExport(resolve(d.cwd, dir), files, Boolean(o.force));
    const manifest = JSON.parse(
      new TextDecoder().decode(files.get("export.json")),
    ) as {
      counts: { memories: number; tombstones: number; receipts: number };
      seal: { sealed_receipts: number; unsealed: number };
    };
    done.push({
      space: sp.slug,
      dir,
      receipt: got.receipt,
      files: files.size,
      memories: manifest.counts.memories,
      tombstones: manifest.counts.tombstones,
      receipts: manifest.counts.receipts,
      sealed: manifest.seal.sealed_receipts,
      unsealed: manifest.seal.unsealed,
    });
    if (!json) {
      d.out(
        chalk.green(`  ${MARK.done} `) +
          chalk.bold(`Exported ${sp.slug}`) +
          chalk.gray(` to ${dir}`),
      );
      const last = done[done.length - 1];
      d.out(
        chalk.gray(
          `    ${plural(last.memories, "memory", "memories")} · ${plural(last.tombstones, "tombstone", "tombstones")} · ${plural(last.receipts, "receipt", "receipts")}, sealed through ${fmt(last.sealed)}`,
        ),
      );
    }
  }
  if (json) {
    d.out(JSON.stringify({ exports: done }, null, 2));
  } else {
    const first = done[0]?.dir;
    d.out(
      chalk.gray(
        `\n  Check ${done.length === 1 ? "it" : "each"} with: memax verify-export ${done.length === 1 ? first : `${outDir}/<space>`}`,
      ),
    );
  }
  return 0;
}

export interface VerifyOptions {
  key?: string[];
  offline?: boolean;
  format?: string;
}

export interface VerifyDeps {
  /** The server's client; none verifies offline. */
  memax?: Memax;
  apiUrl?: string;
  out: (line: string) => void;
}

/** A public key from --key: base64 or hex of 32 bytes, named by its id. */
export function pinnedKey(raw: string): V2.SigningKey {
  const s = raw.trim().replace(/^ed25519:/i, "");
  const bytes = /^[0-9a-f]{64}$/i.test(s)
    ? Buffer.from(s, "hex")
    : Buffer.from(s, "base64");
  if (bytes.length !== 32) {
    throw new ExportFilesError(
      `--key ${raw} isn't an Ed25519 public key (base64 or hex of 32 bytes).`,
    );
  }
  const id =
    "ed25519:" + createHash("sha256").update(bytes).digest("hex").slice(0, 16);
  return {
    key_id: id,
    algorithm: "ed25519",
    public_key: bytes.toString("base64"),
  };
}

export async function verifyExportFolder(
  path: string,
  o: VerifyOptions,
  d: VerifyDeps,
): Promise<number> {
  const { files } = readExport(path);
  let keys: V2.SigningKey[] | undefined;
  let keySource = "the export itself";
  if (o.key?.length) {
    keys = o.key.map(pinnedKey);
    keySource = "--key";
  } else if (!o.offline && d.memax) {
    const manifest = JSON.parse(
      new TextDecoder().decode(files.get("export.json") ?? new Uint8Array()),
    ) as { space?: { id?: string } };
    if (manifest.space?.id) {
      try {
        const page = await d.memax.v2.receipts.checkpoints(manifest.space.id, {
          limit: 1,
        });
        keys = page.keys;
        keySource = d.apiUrl ?? "the server";
      } catch (err) {
        d.out(
          chalk.yellow(
            `  The server's keys aren't available (${apiFailureMessage(err)}); checking signatures with the keys the export carries.`,
          ),
        );
      }
    }
  }
  const report = await verifyExport({ files, keys });
  if (o.format === "json") {
    d.out(JSON.stringify({ ...report, key_source: keySource }, null, 2));
    return report.ok ? 0 : 1;
  }
  for (const line of reportLines(report, keySource, path)) d.out(line);
  return report.ok ? 0 : 1;
}

/** What verify-export says: what was checked, and every problem, exactly. */
export function reportLines(
  r: ExportReport,
  keySource: string,
  path: string,
): string[] {
  const name = r.space?.slug ?? path;
  const asOf = r.asOf
    ? ` as of ${r.asOf.slice(0, 16).replace("T", " ")} UTC`
    : "";
  const lines: string[] = [];
  const ok = (s: string) => lines.push(chalk.green(`  ${MARK.done} `) + s);
  const note = (s: string) => lines.push(chalk.gray(`  ${MARK.waiting} ${s}`));
  const bad = (s: string) => lines.push(chalk.red(`  ✗ ${s}`));
  lines.push(
    r.ok
      ? chalk.bold(`\n  Verified ${name}`) +
          chalk.gray(`, exported${asOf} (${r.format})\n`)
      : chalk.bold.red(
          `\n  ${name} doesn't verify: ${plural(r.problems.length, "problem", "problems")}\n`,
        ),
  );
  if (r.files.listed > 0) {
    if (r.files.matched === r.files.listed)
      ok(
        `${plural(r.files.listed, "file matches", "files match")} export.json`,
      );
    else
      bad(
        `${fmt(r.files.matched)} of ${plural(r.files.listed, "file", "files")} match export.json`,
      );
  }
  const chain = r.chain;
  if (chain) {
    const s = r.sealed;
    const keys = r.keys.ids.join(", ") || "none";
    if (s.receipts === 0) {
      note(
        "No receipt was sealed yet when this was exported, so no signature covers it.",
      );
    } else if (!chain.signaturesChecked) {
      note(
        `Receipts 1–${fmt(s.receipts)} chain into ${plural(s.checkpoints, "checkpoint", "checkpoints")}, but this platform can't check Ed25519 signatures. Use Node 20 or later.`,
      );
    } else {
      const chainOk = !r.problems.some((p) => p.kind === "chain");
      const signed = chain.signed === s.checkpoints;
      const what = `Receipts 1–${fmt(s.receipts)} are sealed in ${plural(s.checkpoints, "checkpoint", "checkpoints")}`;
      if (chainOk && signed)
        ok(`${what}; every signature verifies (${keys}, from ${keySource})`);
      else if (chainOk)
        note(
          `${what}; ${plural(chain.unsigned, "checkpoint is", "checkpoints are")} unsigned, so only the hashes prove it`,
        );
      else bad(`${what}, and the chain doesn't match them (below)`);
    }
    if (r.unsealed.receipts > 0) {
      const first = r.unsealed.firstLine ?? s.receipts + 1;
      note(
        `${plural(r.unsealed.receipts, "receipt", "receipts")} came after the last seal (lines ${fmt(first)}–${fmt(first + r.unsealed.receipts - 1)} of receipts.jsonl): ${r.unsealed.receipts === 1 ? "it chains" : "they chain"} on, but no checkpoint covers ${r.unsealed.receipts === 1 ? "it" : "them"} yet`,
      );
    }
    if (keySource === "the export itself" && s.receipts > 0) {
      note(
        "The signatures were checked with the keys the export carries. Pin Memax's key with --key, or verify while signed in, for an independent check.",
      );
    }
  }
  const rec = r.records;
  if (!r.problems.some((p) => p.kind === "record" || p.kind === "format")) {
    if (chain)
      ok(
        `${plural(rec.memories, "memory", "memories")}, ${plural(rec.tombstones, "tombstone", "tombstones")} and ${plural(rec.briefs, "Brief version", "Brief versions")} match their receipts`,
      );
  }
  if (r.problems.length > 0) lines.push("");
  for (const p of r.problems) {
    const where = p.path ? `${p.path}${p.line ? `:${p.line}` : ""}: ` : "";
    bad(`${where}${p.detail}`);
  }
  if (r.ok) {
    lines.push(
      chalk.gray(
        "\n  Words aren't in receipts (so Forget can remove them): a memory's words are checked against export.json, not a signature.",
      ),
    );
  }
  return lines;
}

export function registerExportCommands(program: Command): void {
  program
    .command("export")
    .description(
      "Export a space's whole record as Markdown, with its receipts and signed checkpoints",
    )
    .option("--space <slug>", "The space (default: every V2 space of yours)")
    .option(
      "--out <dir>",
      "Where to write it, a folder per space",
      "memax-export",
    )
    .option("--force", "Replace an earlier export in the same folder")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (opts: ExportOptions) => {
      try {
        process.exitCode = await exportSpaces(opts, {
          memax: getClient(),
          cwd: process.cwd(),
          out: (l) => console.log(l),
        });
      } catch (err) {
        console.error(
          chalk.red(
            `  ${err instanceof ExportFilesError ? err.message : apiFailureMessage(err)}`,
          ),
        );
        process.exitCode = 1;
      }
    });

  program
    .command("verify-export <path>")
    .description(
      "Check an export: its files, its receipt chain against the signed checkpoints, and its memories",
    )
    .option(
      "--key <key>",
      "A public key to trust (base64 or hex); repeat for more",
      (v: string, all: string[] = []) => [...all, v],
    )
    .option("--offline", "Don't ask the server for its keys")
    .option("--format <format>", "Output format: text, json", "text")
    .action(async (path: string, opts: VerifyOptions) => {
      try {
        process.exitCode = await verifyExportFolder(path, opts, {
          memax: opts.offline ? undefined : getClient(),
          apiUrl: loadConfig().api_url,
          out: (l) => console.log(l),
        });
      } catch (err) {
        console.error(
          chalk.red(
            `  ${err instanceof ExportFilesError ? err.message : apiFailureMessage(err)}`,
          ),
        );
        process.exitCode = 1;
      }
    });
}
