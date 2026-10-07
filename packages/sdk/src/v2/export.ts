// The Memax export format, memax.export.v1 (rule 14): read it back and
// verify it, with no dependency beyond the platform's Web Crypto. The
// server writes it (packages/server/internal/export); `memax export`
// fetches it and `memax verify-export` checks it with verifyExport.
//
// One folder per space:
//
//   README.md, decisions.md   for people (checked by hash only)
//   export.json               the manifest: format, space, as_of, counts, seal, every file's SHA-256
//   memories/M-0219.md        YAML frontmatter + the statement in force as the body
//   tombstones/M-0201.md      a forgotten memory: IDs, times and counts, never words
//   brief/B-0007.md           every Brief version
//   gates.json, targets.json, agents.json, reads.json
//   receipts.jsonl            every receipt in chain order, one per line (ChainReceipt)
//   checkpoints.json          every signed checkpoint, the seal status and the public keys
//
// What verification proves. The receipt chain is recomputed from the
// first receipt and checked against every checkpoint (range, chain hash,
// Merkle root, Ed25519 signature): receipts 1 to N are exactly the ones
// Memax sealed and signed. Receipts after the last checkpoint were not
// sealed when the export was made: they chain on, unproven. Every file is
// checked against export.json, and every memory and tombstone file against
// the receipts (its receipts exist, are about it and are all listed; its
// versions and its body agree). A memory's words are not in any receipt,
// by design (so Forget can remove them), so they are proven only as far
// as export.json is: against changes made without rewriting the manifest.
import type { Checkpoint, SigningKey } from "./types.js";
import {
  micros,
  verifyReceiptChain,
  type ChainCrypto,
  type ChainReceipt,
  type ChainReport,
} from "./verify.js";

/** The format this reads and verifies. */
export const EXPORT_FORMAT = "memax.export.v1";

/** An export's files, by path below the space's folder ("memories/M-0219.md"). */
export type ExportFiles = ReadonlyMap<string, Uint8Array>;

export interface ExportManifestFile {
  path: string;
  sha256: string;
  bytes: number;
}

/** export.json. */
export interface ExportManifest {
  format: string;
  space: {
    id: string;
    tenant_id: string;
    slug: string;
    name: string;
    kind: string;
    repository: string | null;
    v2_enabled_at: string | null;
  };
  /** When the newest receipt in the export was recorded. */
  as_of: string | null;
  /** The export's own `exported` receipt. */
  receipt: string | null;
  counts: {
    memories: number;
    tombstones: number;
    brief_versions: number;
    gates: number;
    targets: number;
    agents: number;
    receipts: number;
    checkpoints: number;
  };
  seal: {
    sealed_receipts: number;
    sealed_through_seq: number | null;
    sealed_through_receipt_id: string | null;
    unsealed: number;
  };
  files: ExportManifestFile[];
  formats: Array<{ path: string; format: string }>;
}

/** checkpoints.json. */
export interface ExportCheckpoints {
  format: string;
  space_id: string;
  seal: {
    sealed_receipts: number;
    sealed_through_seq?: number;
    sealed_through_receipt_id?: string;
    head_sha256?: string;
    checkpoints: number;
    sealed_at?: string;
    unsealed: number;
  };
  keys: SigningKey[];
  checkpoints: Checkpoint[];
}

/** A receipt as a memory's or tombstone's frontmatter lists it. */
export interface ExportReceiptStamp {
  seq: number;
  id: string;
  action: string;
  occurred_at: string;
}

/** A memory file read back: its frontmatter, and its body. */
export interface ExportedMemory {
  path: string;
  format: string;
  ref: string;
  id: string;
  state: string;
  lifecycle: string;
  flags: string[];
  section: string;
  kind: string;
  trust: string;
  version: number;
  stale_after: string | null;
  valid_from: string | null;
  valid_to: string | null;
  created_at: string;
  updated_at: string;
  created_receipt: string;
  last_receipt: string;
  decision: {
    status: string | null;
    why: string | null;
    options: Array<{ label: string; detail: string | null }>;
    consequences: string | null;
    area: string | null;
  } | null;
  conditions: unknown;
  scope: unknown;
  sources: Array<{
    id: string;
    kind: string;
    ref: string;
    uri: string | null;
    locator: unknown;
    trust: string;
    external: boolean;
    quote: string | null;
    content_hash: string | null;
    created_at: string;
  }>;
  links: Array<{
    id: string;
    kind: string;
    direction: string;
    ref: string;
    memory_id: string;
    receipt: string;
    created_at: string;
  }>;
  versions: Array<{
    version: number;
    receipt: string;
    created_at: string;
    statement: string;
  }>;
  receipts: ExportReceiptStamp[];
  /** The body: the statement in force, and a newline. */
  body: string;
}

/** A tombstone file read back. No words. */
export interface ExportedTombstone {
  path: string;
  format: string;
  ref: string;
  id: string;
  kind: string;
  tombstone: string | null;
  op: string | null;
  forgotten_at: string | null;
  by: { kind: string; id: string | null } | null;
  requested_by: { connection_id: string } | null;
  via: string | null;
  receipt: string | null;
  carried: string | null;
  primary: string | null;
  with: string[];
  kept_at: string | null;
  reads_before: number;
  gone: Record<string, number>;
  agents_told: number;
  status: string | null;
  completed_at: string | null;
  reapplied_at: string | null;
  receipts: ExportReceiptStamp[];
}

/** A Brief version read back. */
export interface ExportedBrief {
  path: string;
  format: string;
  ref: string;
  version: number;
  current: boolean;
  brief_id: string;
  version_id: string;
  parent_version: number | null;
  title: string;
  summary: string | null;
  facts: number;
  receipt: string;
  created_at: string;
  sections: Array<{
    key: string;
    heading: string;
    items: Array<{
      ref?: string;
      text?: string;
      cites?: string[];
      forgotten?: boolean;
    }>;
  }>;
  body: string;
}

/** An export read back. */
export interface ParsedExport {
  manifest: ExportManifest;
  memories: ExportedMemory[];
  tombstones: ExportedTombstone[];
  briefs: ExportedBrief[];
  /** In chain order; `lines[i]` is receipts[i]'s line in receipts.jsonl. */
  receipts: ChainReceipt[];
  checkpoints: ExportCheckpoints;
  gates: unknown[];
  targets: unknown[];
  agents: unknown[];
  reads: unknown;
}

export type ExportProblemKind = "format" | "file" | "chain" | "record";

/** One thing that doesn't match, said precisely: where, and what. */
export interface ExportProblem {
  kind: ExportProblemKind;
  /** The file, below the space's folder. */
  path?: string;
  /** The line, for receipts.jsonl and frontmatter. */
  line?: number;
  receipt?: string;
  checkpoint?: number;
  detail: string;
}

/** What verifyExport checked, and what it found. */
export interface ExportReport {
  /** Nothing that was checked failed. */
  ok: boolean;
  format: string | null;
  space: { id: string; slug: string; name: string } | null;
  asOf: string | null;
  /** The export's own receipt. */
  receipt: string | null;
  files: { listed: number; matched: number };
  /** The receipt chain, as verifyReceiptChain reports it. */
  chain: ChainReport | null;
  /** Receipts 1 to `receipts` (chain positions) are sealed in `checkpoints`. */
  sealed: {
    receipts: number;
    throughSeq: number | null;
    throughReceipt: string | null;
    checkpoints: number;
    sealedAt: string | null;
  };
  /** Receipts after the last checkpoint: unsealed when exported. */
  unsealed: { receipts: number; firstLine: number | null };
  /** The keys signatures were checked with: given (pinned or the server's), or the export's own. */
  keys: { source: "given" | "export"; ids: string[] };
  records: { memories: number; tombstones: number; briefs: number };
  problems: ExportProblem[];
}

export interface VerifyExportInput {
  files: ExportFiles;
  /**
   * The public keys to trust: pinned, or the server's (`receipts.checkpoints`).
   * Without them the keys checkpoints.json embeds are used, which proves
   * only that the export is consistent with itself; the report says so.
   */
  keys?: readonly SigningKey[];
  /** Defaults to globalThis.crypto.subtle. */
  crypto?: ChainCrypto;
}

/** A problem with the export's form: a file that can't be read. */
export class ExportFormatError extends Error {
  constructor(
    readonly path: string,
    readonly detail: string,
    readonly line?: number,
  ) {
    super(`${path}${line ? `:${line}` : ""}: ${detail}`);
    this.name = "ExportFormatError";
  }
}

const decoder = new TextDecoder("utf-8", { fatal: true });

function text(files: ExportFiles, path: string): string {
  const raw = files.get(path);
  if (!raw) throw new ExportFormatError(path, "it is missing");
  try {
    return decoder.decode(raw);
  } catch {
    throw new ExportFormatError(path, "it isn't UTF-8 text");
  }
}

function json<T>(files: ExportFiles, path: string): T {
  const s = text(files, path);
  try {
    return JSON.parse(s) as T;
  } catch (err) {
    throw new ExportFormatError(
      path,
      `it isn't JSON (${err instanceof Error ? err.message : String(err)})`,
    );
  }
}

const KEY = /^([a-z][a-z0-9_]*):(?: (.*))?$/;
const FIELD = /^ {2}([a-z][a-z0-9_]*): (.*)$/;

/**
 * Reads frontmatter in the export's YAML subset: `key: <json>`, or `key:`
 * followed by a block list (`  - <json>`) or a block object one level deep
 * (`  name: <json>`). Any YAML parser reads the same data; this one reads
 * only the subset, so a file reformatted by hand is caught. `line` is the
 * frontmatter's first line in its file, for messages.
 */
export function parseFrontmatter(
  front: string,
  path = "frontmatter",
  line = 2,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  const lines = front.endsWith("\n")
    ? front.slice(0, -1).split("\n")
    : front.split("\n");
  const value = (raw: string, at: number): unknown => {
    try {
      return JSON.parse(raw);
    } catch {
      throw new ExportFormatError(path, `not a JSON value: ${raw}`, line + at);
    }
  };
  for (let i = 0; i < lines.length; ) {
    const m = KEY.exec(lines[i]);
    if (!m) {
      throw new ExportFormatError(
        path,
        `not a frontmatter field: ${lines[i]}`,
        line + i,
      );
    }
    const key = m[1];
    if (key in out) {
      throw new ExportFormatError(path, `${key} appears twice`, line + i);
    }
    if (m[2] !== undefined) {
      out[key] = value(m[2], i);
      i++;
      continue;
    }
    i++;
    if (i < lines.length && lines[i].startsWith("  - ")) {
      const list: unknown[] = [];
      while (i < lines.length && lines[i].startsWith("  - ")) {
        list.push(value(lines[i].slice(4), i));
        i++;
      }
      out[key] = list;
      continue;
    }
    const obj: Record<string, unknown> = {};
    let fields = 0;
    for (
      let f = FIELD.exec(lines[i] ?? "");
      f;
      f = FIELD.exec(lines[i] ?? "")
    ) {
      obj[f[1]] = value(f[2], i);
      fields++;
      i++;
    }
    if (fields === 0) {
      throw new ExportFormatError(path, `${key} has no value`, line + i - 1);
    }
    out[key] = obj;
  }
  return out;
}

/** Splits a Markdown file into its frontmatter (without fences) and body. */
export function splitDocument(
  doc: string,
  path = "file",
): { front: string; body: string } {
  if (!doc.startsWith("---\n")) {
    throw new ExportFormatError(path, "it doesn't start with a --- fence", 1);
  }
  const rest = doc.slice(4);
  const end = rest.indexOf("\n---\n");
  if (end < 0) {
    throw new ExportFormatError(path, "its frontmatter has no closing ---");
  }
  const body = rest.slice(end + 5);
  if (!body.startsWith("\n")) {
    throw new ExportFormatError(
      path,
      "a blank line must follow the frontmatter",
    );
  }
  return { front: rest.slice(0, end + 1), body: body.slice(1) };
}

function document<T>(
  files: ExportFiles,
  path: string,
  format: string,
): T & { path: string; body: string } {
  const { front, body } = splitDocument(text(files, path), path);
  const data = parseFrontmatter(front, path);
  if (data.format !== format) {
    throw new ExportFormatError(
      path,
      `its format is ${JSON.stringify(data.format)}, not ${format}`,
      2,
    );
  }
  return { ...(data as T), path, body };
}

/** Reads receipts.jsonl: one receipt per line, in chain order. */
export function parseReceipts(raw: string): ChainReceipt[] {
  if (raw === "") return [];
  const lines = raw.endsWith("\n") ? raw.slice(0, -1).split("\n") : [raw];
  return lines.map((l, i) => {
    let r: unknown;
    try {
      r = JSON.parse(l);
    } catch {
      throw new ExportFormatError("receipts.jsonl", "not JSON", i + 1);
    }
    if (
      !r ||
      typeof r !== "object" ||
      typeof (r as ChainReceipt).id !== "string" ||
      typeof (r as ChainReceipt).seq !== "number"
    ) {
      throw new ExportFormatError("receipts.jsonl", "not a receipt", i + 1);
    }
    return r as ChainReceipt;
  });
}

/**
 * Reads an export back: the manifest, every memory, tombstone and Brief
 * file, the receipts and checkpoints. Throws ExportFormatError at the
 * first file that isn't in the format.
 */
export function parseExport(files: ExportFiles): ParsedExport {
  const manifest = json<ExportManifest>(files, "export.json");
  if (manifest.format !== EXPORT_FORMAT) {
    throw new ExportFormatError(
      "export.json",
      `the format is ${JSON.stringify(manifest.format)}; this reads ${EXPORT_FORMAT}`,
    );
  }
  const paths = [...files.keys()].sort();
  const memories: ExportedMemory[] = [];
  const tombstones: ExportedTombstone[] = [];
  const briefs: ExportedBrief[] = [];
  for (const p of paths) {
    if (p.startsWith("memories/") && p.endsWith(".md")) {
      memories.push(document<ExportedMemory>(files, p, "memax.memory.v1"));
    } else if (p.startsWith("tombstones/") && p.endsWith(".md")) {
      tombstones.push(
        document<ExportedTombstone>(files, p, "memax.tombstone.v1"),
      );
    } else if (p.startsWith("brief/") && p.endsWith(".md")) {
      briefs.push(document<ExportedBrief>(files, p, "memax.brief.v1"));
    }
  }
  return {
    manifest,
    memories,
    tombstones,
    briefs: briefs.sort((a, b) => a.version - b.version),
    receipts: parseReceipts(text(files, "receipts.jsonl")),
    checkpoints: json<ExportCheckpoints>(files, "checkpoints.json"),
    gates: json<{ gates: unknown[] }>(files, "gates.json").gates,
    targets: json<{ targets: unknown[] }>(files, "targets.json").targets,
    agents: json<{ agents: unknown[] }>(files, "agents.json").agents,
    reads: json<unknown>(files, "reads.json"),
  };
}

function subtleOf(crypto?: ChainCrypto): ChainCrypto {
  const c =
    crypto ??
    (globalThis as { crypto?: { subtle?: ChainCrypto } }).crypto?.subtle;
  if (!c) throw new Error("verifyExport needs Web Crypto (crypto.subtle)");
  return c;
}

function toHex(b: ArrayBuffer): string {
  let s = "";
  for (const x of new Uint8Array(b)) s += x.toString(16).padStart(2, "0");
  return s;
}

function lines(from: number, to: number): string {
  return from === to ? `line ${from}` : `lines ${from}–${to}`;
}

/**
 * Verifies an export: every file against export.json, the receipt chain
 * against every signed checkpoint (with `keys`), and every memory,
 * tombstone and Brief file against the receipts. Each problem says
 * exactly where it is. See the top of this file for what that proves.
 */
export async function verifyExport(
  input: VerifyExportInput,
): Promise<ExportReport> {
  const c = subtleOf(input.crypto);
  const problems: ExportProblem[] = [];
  const report: ExportReport = {
    ok: false,
    format: null,
    space: null,
    asOf: null,
    receipt: null,
    files: { listed: 0, matched: 0 },
    chain: null,
    sealed: {
      receipts: 0,
      throughSeq: null,
      throughReceipt: null,
      checkpoints: 0,
      sealedAt: null,
    },
    unsealed: { receipts: 0, firstLine: null },
    keys: { source: input.keys ? "given" : "export", ids: [] },
    records: { memories: 0, tombstones: 0, briefs: 0 },
    problems,
  };
  const done = () => {
    report.ok = problems.length === 0;
    return report;
  };

  // The manifest, and every file against it.
  let manifest: ExportManifest;
  try {
    manifest = json<ExportManifest>(input.files, "export.json");
  } catch (err) {
    problems.push(formatProblem(err));
    return done();
  }
  report.format = manifest.format ?? null;
  if (manifest.format !== EXPORT_FORMAT) {
    problems.push({
      kind: "format",
      path: "export.json",
      detail: `the format is ${JSON.stringify(manifest.format)}; this verifies ${EXPORT_FORMAT}`,
    });
    return done();
  }
  report.space = {
    id: manifest.space.id,
    slug: manifest.space.slug,
    name: manifest.space.name,
  };
  report.asOf = manifest.as_of;
  report.receipt = manifest.receipt;
  report.files.listed = manifest.files.length;
  const listed = new Set<string>();
  for (const f of manifest.files) {
    listed.add(f.path);
    const data = input.files.get(f.path);
    if (!data) {
      problems.push({
        kind: "file",
        path: f.path,
        detail: "export.json lists it, but it isn't in the export",
      });
      continue;
    }
    const sum = toHex(await c.digest("SHA-256", data));
    if (sum !== f.sha256 || data.length !== f.bytes) {
      problems.push({
        kind: "file",
        path: f.path,
        detail: `it changed after the export: its SHA-256 is ${sum.slice(0, 12)}…, export.json says ${f.sha256.slice(0, 12)}…`,
      });
      continue;
    }
    report.files.matched++;
  }
  for (const p of [...input.files.keys()].sort()) {
    if (p !== "export.json" && !listed.has(p)) {
      problems.push({
        kind: "file",
        path: p,
        detail: "it isn't in export.json, so it wasn't part of the export",
      });
    }
  }

  // Read the rest; a file that can't be read stops here.
  let parsed: ParsedExport;
  try {
    parsed = parseExport(input.files);
  } catch (err) {
    problems.push(formatProblem(err));
    return done();
  }

  // The chain.
  const cps = [...parsed.checkpoints.checkpoints].sort(
    (a, b) => a.number - b.number,
  );
  const keys = input.keys ?? parsed.checkpoints.keys;
  report.keys.ids = keys.map((k) => k.key_id).sort();
  const chain = await verifyReceiptChain({
    spaceId: manifest.space.id,
    receipts: parsed.receipts,
    checkpoints: cps,
    keys,
    crypto: input.crypto,
  });
  report.chain = chain;
  const byNumber = new Map(cps.map((cp) => [cp.number, cp]));
  for (const p of chain.problems) {
    const cp = p.checkpoint ? byNumber.get(p.checkpoint) : undefined;
    const where = cp
      ? `checkpoint ${cp.number} (receipts ${cp.position_from}–${cp.position_to}, ${lines(cp.position_from, cp.position_to)} of receipts.jsonl)`
      : p.receipt
        ? `receipt ${p.receipt}`
        : "the chain";
    const what =
      p.kind === "signature"
        ? `${where}: ${p.detail}${cp?.key_id ? ` (key ${cp.key_id})` : ""}`
        : p.kind === "merkle" || p.kind === "chain"
          ? `${where}: ${p.detail}; a receipt in that range was changed, added, removed or reordered`
          : `${where}: ${p.detail}`;
    problems.push({
      kind: "chain",
      path: p.kind === "signature" ? "checkpoints.json" : "receipts.jsonl",
      checkpoint: p.checkpoint,
      receipt: p.receipt,
      line: p.receipt
        ? parsed.receipts.findIndex((r) => r.id === p.receipt) + 1 || undefined
        : undefined,
      detail: what,
    });
  }
  const last = cps[cps.length - 1];
  report.sealed = {
    receipts: last ? last.position_to : 0,
    throughSeq: last ? last.last_seq : null,
    throughReceipt: last ? last.last_receipt_id : null,
    checkpoints: cps.length,
    sealedAt: last ? last.sealed_at : null,
  };
  const unsealed = Math.max(0, parsed.receipts.length - report.sealed.receipts);
  report.unsealed = {
    receipts: unsealed,
    firstLine: unsealed > 0 ? report.sealed.receipts + 1 : null,
  };
  if (
    manifest.seal.sealed_receipts !== report.sealed.receipts ||
    manifest.seal.unsealed !== unsealed
  ) {
    problems.push({
      kind: "record",
      path: "export.json",
      detail: `it says ${manifest.seal.sealed_receipts} receipts are sealed and ${manifest.seal.unsealed} aren't; the checkpoints seal ${report.sealed.receipts} of ${parsed.receipts.length}`,
    });
  }

  checkRecords(parsed, problems);
  report.records = {
    memories: parsed.memories.length,
    tombstones: parsed.tombstones.length,
    briefs: parsed.briefs.length,
  };
  return done();
}

function formatProblem(err: unknown): ExportProblem {
  if (err instanceof ExportFormatError) {
    return {
      kind: "format",
      path: err.path,
      line: err.line,
      detail: err.detail,
    };
  }
  return {
    kind: "format",
    detail: err instanceof Error ? err.message : String(err),
  };
}

/** Each memory, tombstone and Brief file against the receipts. */
function checkRecords(parsed: ParsedExport, problems: ExportProblem[]): void {
  const receipts = new Map<string, { r: ChainReceipt; line: number }>();
  parsed.receipts.forEach((r, i) => receipts.set(r.id, { r, line: i + 1 }));
  const about = new Map<string, ChainReceipt[]>();
  for (const r of parsed.receipts) {
    if (r.object_kind !== "memory") continue;
    about.set(r.object_id, [...(about.get(r.object_id) ?? []), r]);
  }
  const add = (path: string, detail: string, receipt?: string) => {
    const line = receipt ? receipts.get(receipt)?.line : undefined;
    problems.push({ kind: "record", path, detail, receipt, line });
  };

  // A file's receipts: each is in receipts.jsonl, about this object, and
  // as listed; and none about it is left out.
  const checkStamps = (
    path: string,
    id: string,
    stamps: ExportReceiptStamp[] | undefined,
  ) => {
    const listed = new Set<string>();
    for (const s of stamps ?? []) {
      listed.add(s.id);
      const found = receipts.get(s.id);
      if (!found) {
        add(path, `it lists receipt ${s.id}, which isn't in receipts.jsonl`);
        continue;
      }
      const r = found.r;
      if (r.object_id !== id) {
        add(path, `its receipt ${s.id} is about ${r.object_ref}`, s.id);
      } else if (
        r.seq !== s.seq ||
        r.action !== s.action ||
        micros(r.occurred_at) !== micros(s.occurred_at)
      ) {
        add(
          path,
          `its receipt ${s.id} says ${s.action} (seq ${s.seq}, ${s.occurred_at}); receipts.jsonl line ${found.line} says ${r.action} (seq ${r.seq}, ${r.occurred_at})`,
          s.id,
        );
      }
    }
    for (const r of about.get(id) ?? []) {
      if (!listed.has(r.id)) {
        add(
          path,
          `receipts.jsonl has receipt ${r.id} (${r.action}) about it, which the file doesn't list`,
          r.id,
        );
      }
    }
    return listed;
  };

  const files = new Set<string>();
  for (const m of parsed.memories) {
    files.add(m.id);
    if (m.path !== `memories/${m.ref}.md`) {
      add(m.path, `it holds ${m.ref}, whose file is memories/${m.ref}.md`);
    }
    if (m.lifecycle === "forgotten") {
      add(
        m.path,
        "a forgotten memory has a memory file; it should be a tombstone",
      );
    }
    const current = (m.versions ?? []).find((v) => v.version === m.version);
    if (!current) {
      add(m.path, `it has no version ${m.version}, the one in force`);
    } else if (m.body !== `${current.statement}\n`) {
      add(
        m.path,
        `its body isn't the statement of version ${m.version} in its frontmatter`,
      );
    }
    const listed = checkStamps(m.path, m.id, m.receipts);
    const own = [
      ["created_receipt", m.created_receipt],
      ["last_receipt", m.last_receipt],
      ...(m.versions ?? []).map((v) => [
        `version ${v.version}'s receipt`,
        v.receipt,
      ]),
    ];
    for (const [what, id] of own) {
      if (id && !listed.has(id)) {
        add(m.path, `its ${what}, ${id}, isn't among its receipts`, id);
      }
    }
  }
  for (const t of parsed.tombstones) {
    if (t.kind !== "memory") continue;
    files.add(t.id);
    if (parsed.memories.some((m) => m.id === t.id)) {
      add(t.path, `${t.ref} has a tombstone and a memory file`);
    }
    if (t.path !== `tombstones/${t.ref}.md`) {
      add(t.path, `it holds ${t.ref}, whose file is tombstones/${t.ref}.md`);
    }
    const forgot = t.receipt ? receipts.get(t.receipt)?.r : undefined;
    if (!forgot || forgot.action !== "forgot" || forgot.object_id !== t.id) {
      add(
        t.path,
        `its receipt ${t.receipt ?? "(none)"} isn't the forgot receipt of ${t.ref}`,
        t.receipt ?? undefined,
      );
    }
    checkStamps(t.path, t.id, t.receipts);
  }
  // Every memory the receipts name has a file: a memory, or a tombstone.
  for (const [id, rs] of about) {
    if (!files.has(id)) {
      add(
        "receipts.jsonl",
        `${rs.length} receipts are about ${rs[0].object_ref}, which has no file in memories/ or tombstones/`,
        rs[0].id,
      );
    }
  }
  // Every forgotten memory has its tombstone.
  for (const r of parsed.receipts) {
    if (
      r.action === "forgot" &&
      r.object_kind === "memory" &&
      !parsed.tombstones.some((t) => t.id === r.object_id)
    ) {
      add(
        "receipts.jsonl",
        `${r.object_ref} was forgotten, but the export has no tombstone for it`,
        r.id,
      );
    }
  }

  const current = parsed.briefs.filter((b) => b.current);
  if (parsed.briefs.length > 0 && current.length !== 1) {
    add("brief/", `${current.length} Brief versions say they are current`);
  }
  for (const b of parsed.briefs) {
    const r = receipts.get(b.receipt)?.r;
    if (!r || r.object_kind !== "brief" || r.object_id !== b.brief_id) {
      add(
        b.path,
        `its receipt ${b.receipt} isn't one of the Brief's`,
        b.receipt,
      );
    }
  }

  const m = parsed.manifest;
  if (m.receipt) {
    const r = receipts.get(m.receipt)?.r;
    if (!r || r.action !== "exported" || r.object_id !== m.space.id) {
      add(
        "export.json",
        `its receipt ${m.receipt} isn't this export's exported receipt`,
        m.receipt,
      );
    }
  }
  const counts: Array<[string, number, number]> = [
    ["memories", m.counts.memories, parsed.memories.length],
    ["tombstones", m.counts.tombstones, parsed.tombstones.length],
    ["Brief versions", m.counts.brief_versions, parsed.briefs.length],
    ["receipts", m.counts.receipts, parsed.receipts.length],
    [
      "checkpoints",
      m.counts.checkpoints,
      parsed.checkpoints.checkpoints.length,
    ],
    ["gates", m.counts.gates, parsed.gates.length],
    ["targets", m.counts.targets, parsed.targets.length],
    ["agents", m.counts.agents, parsed.agents.length],
  ];
  for (const [what, said, found] of counts) {
    if (said !== found) {
      add(
        "export.json",
        `it counts ${said} ${what}; the export holds ${found}`,
      );
    }
  }
}
