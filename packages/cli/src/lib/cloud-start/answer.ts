// The server's answer to a cloud start, checked and turned into the
// block's section (cloud warm-start design §3.5, §11). The answer is the
// record's, and still nothing in it is printed unchecked: every line must
// end in its cite list, cite only the memories it says, cite none a person
// forgot, and is cleaned of hidden characters again; paths, refs and
// agent names must look like what they are; gate questions are cleaned
// and cut. Nothing the request sent (branch, paths) is echoed. What this
// session was already told (a resume, a compact) is left out, by hash.
import type { V2 } from "memax-sdk";
import { cleanLine } from "../daemon/compiler/sanitize.js";
import type { CitedLine, CompiledFile } from "../hook/compiled.js";
import { shortHash } from "../hook/hash.js";
import type {
  BlockFile,
  BlockForget,
  BlockGate,
  BlockSection,
} from "../hook/render.js";
import { emptySeen, type Seen } from "../hook/seen.js";

const PATH = /^[A-Za-z0-9._/-]{1,300}$/;
const COMPILE = /^C-\d{4,18}$/;
const MEMORY = /^M-\d{4,18}$/;
const GATE = /^G-\d{4,18}$/;
const AGENT = /^[a-z0-9][a-z0-9-]{0,39}$/;
/** Trailing cites, as the compiler's parse-back reads them. */
const CITES = /\s*\[(M-\d+(?:\s*,\s*M-\d+)*)\]$/;
const MAX_QUESTION = 200;
/** Seen keys kept a file, most recent first. */
const KEEP_LINES = 500;

/** Thrown for an answer that isn't a session start's. */
export class AnswerError extends Error {}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function list(v: unknown, what: string): unknown[] {
  if (!Array.isArray(v)) throw new AnswerError(`${what} isn't a list`);
  return v;
}

function refsOf(v: unknown, pattern: RegExp): string[] {
  return Array.isArray(v)
    ? v.filter((r): r is string => typeof r === "string" && pattern.test(r))
    : [];
}

function iso(v: unknown): string | null {
  return typeof v === "string" && !Number.isNaN(Date.parse(v)) ? v : null;
}

/** A line the block may print, or null. */
function printable(
  raw: unknown,
  forgotten: Set<string>,
): { text: string; refs: string[] } | null {
  if (!isObject(raw) || typeof raw.text !== "string") return null;
  const refs = refsOf(raw.refs, MEMORY);
  if (refs.length === 0 || refs.length !== (raw.refs as unknown[]).length)
    return null;
  const text = cleanLine(raw.text).text;
  const m = CITES.exec(text);
  if (!m) return null;
  const cited = m[1].split(",").map((r) => r.trim());
  if (cited.join(",") !== refs.join(",")) return null;
  if (refs.some((r) => forgotten.has(r))) return null;
  return { text, refs };
}

export interface Section {
  block: BlockSection;
  /** What to remember as told, for the next start in this sandbox. */
  seen: Seen;
}

/**
 * The block's section from the answer, without what `before` says this
 * session was already told. `local` are the files the session loaded, to
 * say which still hold a forgotten memory.
 */
export function toSection(
  answer: unknown,
  local: CompiledFile[],
  before: Seen | null,
  now: Date,
): Section {
  if (!isObject(answer)) throw new AnswerError("the answer isn't an object");
  const a = answer as Partial<V2.SessionStart> & Record<string, unknown>;
  const space =
    isObject(a.space) && typeof a.space.slug === "string" ? a.space.slug : "";
  const prev = before ?? emptySeen();
  const told = {
    forgotten: new Set(prev.forgotten),
    gates: new Set(prev.gates),
  };
  const next: Seen = {
    version: 1,
    at: now.toISOString(),
    files: { ...prev.files },
    forgotten: [...prev.forgotten],
    gates: [...prev.gates],
    behind: [],
  };

  // Every memory forgotten, told or not: no line citing one is printed.
  const forgottenRefs = new Set<string>();
  const forgets = list(a.forgotten, "forgotten").filter(isObject);
  const notices = list(a.notices, "notices").filter(isObject);
  for (const f of forgets) {
    for (const r of [f.ref, ...(Array.isArray(f.with) ? f.with : [])])
      if (typeof r === "string") forgottenRefs.add(r);
  }
  for (const n of notices)
    if (n.kind === "forgotten")
      for (const r of refsOf(n.refs, MEMORY)) forgottenRefs.add(r);

  const files: BlockFile[] = [];
  for (const raw of list(a.files, "files")) {
    if (!isObject(raw)) continue;
    const path = typeof raw.path === "string" ? raw.path : "";
    const here = typeof raw.here === "string" ? raw.here : "";
    const latest = typeof raw.latest === "string" ? raw.latest : "";
    if (!PATH.test(path) || path.split("/").includes("..")) continue;
    if (!COMPILE.test(here) || !COMPILE.test(latest)) continue;
    const was = prev.files[path]?.lines ?? {};
    const tell: Record<string, string> = {};
    const counts = new Map<string, number>();
    const keep = (ls: unknown): CitedLine[] => {
      const out: CitedLine[] = [];
      for (const l of Array.isArray(ls) ? ls : []) {
        const p = printable(l, forgottenRefs);
        if (!p) continue;
        const base = p.refs.join(",");
        const n = (counts.get(base) ?? 0) + 1;
        counts.set(base, n);
        const key = n === 1 ? base : `${base}#${n}`;
        const hash = shortHash(p.text);
        if (was[key] === hash) continue;
        tell[key] = hash;
        out.push({ key, refs: p.refs, text: p.text, hash });
      }
      return out;
    };
    const added = keep(raw.added);
    const changed = keep(raw.changed);
    const gone = refsOf(raw.gone, MEMORY).filter(
      (r) => !forgottenRefs.has(r) && was[`-${r}`] !== latest,
    );
    for (const r of gone) tell[`-${r}`] = latest;
    const editKey = `!edited:${here}`;
    const handEdit = raw.hand_edited === true && was[editKey] === undefined;
    if (handEdit) tell[editKey] = "1";
    const more =
      typeof raw.more === "number" && raw.more > 0 ? Math.floor(raw.more) : 0;
    files.push({
      path,
      compile: latest,
      was: here,
      added,
      changed,
      gone,
      handEdit,
      ...(more > 0 ? { more } : {}),
    });
    const lines = { ...was, ...tell };
    const keys = Object.keys(lines);
    next.files[path] = {
      compile: latest,
      lines: Object.fromEntries(
        keys
          .slice(Math.max(0, keys.length - KEEP_LINES))
          .map((k) => [k, lines[k]]),
      ),
    };
  }

  // Forgets: the server's, then the notices' it didn't list.
  const forgotten: BlockForget[] = [];
  const held = (ref: string) =>
    local
      .filter((f) => f.lines.some((l) => l.refs.includes(ref)))
      .map((f) => f.path);
  const add = (
    ref: string,
    withRefs: string[],
    at: string | null,
    inFiles: boolean,
  ) => {
    if (!MEMORY.test(ref) || !at || told.forgotten.has(ref)) return;
    if (forgotten.some((f) => f.ref === ref)) return;
    told.forgotten.add(ref);
    next.forgotten.unshift(ref);
    const paths = inFiles ? held(ref) : [];
    forgotten.push({
      ref,
      with: withRefs,
      at,
      ...(paths.length > 0 ? { held: paths } : {}),
    });
  };
  for (const f of forgets)
    add(String(f.ref), refsOf(f.with, MEMORY), iso(f.at), f.in_files === true);
  for (const n of notices)
    if (n.kind === "forgotten")
      for (const r of refsOf(n.refs, MEMORY))
        add(r, [], iso(n.at), held(r).length > 0);

  // Gates still waiting that this session hasn't heard of.
  const gates: BlockGate[] = [];
  for (const g of list(a.gates, "gates")) {
    if (!isObject(g) || typeof g.ref !== "string" || !GATE.test(g.ref))
      continue;
    const expires = iso(g.expires_at);
    if (!expires || told.gates.has(g.ref)) continue;
    let question = cleanLine(
      typeof g.question === "string" ? g.question : "",
    ).text;
    if (question === "") continue;
    if (question.length > MAX_QUESTION)
      question = `${question.slice(0, MAX_QUESTION - 1)}…`;
    told.gates.add(g.ref);
    next.gates.unshift(g.ref);
    gates.push({
      ref: g.ref,
      question,
      ...(typeof g.agent === "string" && AGENT.test(g.agent)
        ? { agent: g.agent }
        : {}),
      expires_at: expires,
    });
  }

  return {
    block: { space, files, forgotten, gates, behind: [] },
    seen: next,
  };
}
