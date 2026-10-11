// Copied verbatim from packages/compiler/src/conditions/versions.ts by scripts/sync-compiler.mjs.
// Don't edit it here: change the compiler, then run the script.

// Versions and ranges, as far as dep_present reads them (the
// stale-detection design §1.1): a condition's `range` is compared with the
// lowest version the manifest's declared constraint allows. Both are read
// in a subset each ecosystem shares:
//
//   versions     1, 1.2, 1.2.3.4, with a leading v (Go), a pre-release
//                (-rc.1, a1, b2, rc1, .dev0, a Go pseudo-version's
//                -2023…-abcdef) or a post-release (.post1, -1); build
//                metadata and local versions (+…) are ignored
//   comparators  = == != < <= > >= ^ ~ ~= and a bare version, x and *
//                wildcards (1.2.x, 1.*), joined by commas or spaces (all
//                of them hold), alternatives by || (one of them does), and
//                npm's hyphen ranges (1.2 - 2.0)
//
// A bare version is caret in Cargo (1.2 is ^1.2) and exact elsewhere.
// Anything outside the subset (a tag like latest, workspace: or catalog:,
// a path, a URL, a git source, an epoch, ===) can't be read: the check
// says unknown, never that the constraint changed.

/** A version: its release numbers, then pre (-1), release (0) or post (1). */
export interface Version {
  release: number[];
  tag: -1 | 0 | 1;
  /** What follows the release, lower case without its separator ("rc.1", "a1", "post2"). */
  extra: string;
}

export type Ecosystem = "go" | "npm" | "python" | "cargo";

const PRE = /^[-_.]?(dev|a|alpha|b|beta|c|rc|pre|preview)[-_.]?\d*([-_.+].*)?$/;
// PEP 440's post-releases; 1.0-1 is one too (a semver pre-release 1.0.0-1
// reads as a post-release: the subset's one ambiguity).
const POST = /^([-_.]?(post|rev|r)[-_.]?\d*|-\d+)$/;

/** A version, or null outside the subset. */
export function parseVersion(text: string): Version | null {
  const m = /^[vV]?(\d+(?:\.\d+)*)(.*)$/.exec(text.trim());
  if (!m) return null;
  const release = m[1].split(".").map(Number);
  if (release.some((n) => !Number.isSafeInteger(n))) return null;
  const rest = m[2].split("+")[0].toLowerCase();
  if (rest === "") return { release, tag: 0, extra: "" };
  if (POST.test(rest))
    return { release, tag: 1, extra: rest.replace(/^[-_.]/, "") };
  if (PRE.test(rest) || /^-[0-9a-z][0-9a-z.-]*$/.test(rest))
    return { release, tag: -1, extra: preExtra(rest.replace(/^[-_.]/, "")) };
  return null;
}

/** PEP 440's spellings of a pre-release as one: alpha is a, beta b, c and pre rc. */
function preExtra(extra: string): string {
  const m = /^(alpha|beta|preview|pre|c)(?![a-z])(.*)$/.exec(extra);
  if (!m) return extra;
  return (
    (
      { alpha: "a", beta: "b", preview: "rc", pre: "rc", c: "rc" } as Record<
        string,
        string
      >
    )[m[1]] + m[2]
  );
}

/** Negative, zero or positive, as a is before, the same as or after b. */
export function compareVersions(a: Version, b: Version): number {
  const n = Math.max(a.release.length, b.release.length);
  for (let i = 0; i < n; i++) {
    const d = (a.release[i] ?? 0) - (b.release[i] ?? 0);
    if (d !== 0) return d;
  }
  if (a.tag !== b.tag) return a.tag - b.tag;
  return compareExtra(a.extra, b.extra);
}

/** Pre-releases among themselves: dev first, then by identifiers. */
function compareExtra(a: string, b: string): number {
  if (a === b) return 0;
  // A top's bound (caretTop, tildeTop) comes before every pre-release.
  if (a === "") return -1;
  if (b === "") return 1;
  const dev = (s: string) => (s.startsWith("dev") ? 0 : 1);
  if (dev(a) !== dev(b)) return dev(a) - dev(b);
  const pa = a.split(/[-_.]|(?<=\D)(?=\d)|(?<=\d)(?=\D)/);
  const pb = b.split(/[-_.]|(?<=\D)(?=\d)|(?<=\d)(?=\D)/);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i];
    const y = pb[i];
    if (x === undefined) return -1;
    if (y === undefined) return 1;
    if (x === y) continue;
    const nx = /^\d+$/.test(x);
    const ny = /^\d+$/.test(y);
    if (nx && ny) return Number(x) - Number(y);
    if (nx !== ny) return nx ? -1 : 1;
    return x < y ? -1 : 1;
  }
  return 0;
}

/** A comparator's version: the numbers given, and whether a wildcard ended it. */
interface Partial {
  parts: number[];
  version: Version;
  /** Fewer numbers than a full version, or an x or * in place of one. */
  partial: boolean;
}

function parsePartial(text: string): Partial | null {
  const t = text.trim();
  const m = /^[vV]?(\d+)((?:\.(?:\d+|[xX*]))*)(.*)$/.exec(t);
  if (!m) return null;
  const rest = m[2].split(".").slice(1);
  const parts = [Number(m[1])];
  let wild = false;
  for (const r of rest) {
    if (/^[xX*]$/.test(r)) {
      wild = true;
      break;
    }
    if (wild) return null;
    parts.push(Number(r));
  }
  if (wild && m[3] !== "") return null;
  if (parts.some((n) => !Number.isSafeInteger(n))) return null;
  const version = wild
    ? { release: parts, tag: 0 as const, extra: "" }
    : parseVersion(t);
  if (!version) return null;
  return { parts, version, partial: wild || parts.length < 3 };
}

interface Comparator {
  op: string;
  v: Partial;
}

/** One alternative's comparators, or null outside the subset. */
function comparators(alt: string): Comparator[] | null {
  const tokens = alt.replace(/,/g, " ").trim().split(/\s+/).filter(Boolean);
  const out: Comparator[] = [];
  for (let i = 0; i < tokens.length; i++) {
    let t = tokens[i];
    if (/^(==|!=|~=|<=|>=|<|>|=|\^|~)$/.test(t)) {
      // An operator apart from its version: ">= 1.2".
      if (i + 1 >= tokens.length) return null;
      t += tokens[++i];
    }
    const m = /^(===|==|!=|~=|<=|>=|<|>|=|\^|~)?(.*)$/.exec(t)!;
    const op = m[1] ?? "";
    if (op === "===") return null;
    if (/^[xX*]$/.test(m[2]) && (op === "" || op === "=" || op === "==")) {
      out.push({
        op: "*",
        v: {
          parts: [],
          version: { release: [0], tag: 0, extra: "" },
          partial: true,
        },
      });
      continue;
    }
    const v = parsePartial(m[2]);
    if (!v) return null;
    out.push({ op, v });
  }
  return out;
}

/** The alternatives of a range, each its comparators, or null. */
function alternatives(
  text: string,
): Array<Comparator[] | { from: Partial; to: Partial }> | null {
  const t = text.trim();
  if (t === "" || /[:/@]|^(latest|next)$/i.test(t)) return null;
  const out: Array<Comparator[] | { from: Partial; to: Partial }> = [];
  for (const alt of t.split("||")) {
    const hy = /^\s*(\S+)\s+-\s+(\S+)\s*$/.exec(alt);
    if (hy) {
      const from = parsePartial(hy[1]);
      const to = parsePartial(hy[2]);
      if (!from || !to) return null;
      out.push({ from, to });
      continue;
    }
    const cs = comparators(alt);
    if (!cs || cs.length === 0) return null;
    out.push(cs);
  }
  return out;
}

const floor = (p: Partial): Version =>
  p.partial
    ? {
        release: [
          ...p.parts,
          ...Array(Math.max(0, 3 - p.parts.length)).fill(0),
        ],
        tag: 0,
        extra: "",
      }
    : p.version;

/**
 * The lowest version a declared constraint allows ("^1.2.0" → 1.2.0,
 * ">=2,<3" → 2.0.0, "v1.4.0" → 1.4.0), or null when it can't say: outside
 * the subset, no lower bound (<2), or an exclusive one (>1.0).
 */
export function lowestAllowed(constraint: string): Version | null {
  // A bare version is its own lowest in every ecosystem (exact, caret in
  // Cargo, the minimum in Go), so the ecosystem doesn't matter here.
  const alts = alternatives(constraint);
  if (!alts) return null;
  let lowest: Version | null = null;
  for (const alt of alts) {
    let low: Version | null = null;
    if (!Array.isArray(alt)) low = floor(alt.from);
    else
      for (const c of alt) {
        if (c.op === ">") return null;
        if (c.op === "<" || c.op === "<=" || c.op === "!=" || c.op === "*")
          continue;
        const v = floor(c.v);
        if (!low || compareVersions(v, low) > 0) low = v;
      }
    if (!low) return null;
    if (!lowest || compareVersions(low, lowest) < 0) lowest = low;
  }
  return lowest;
}

/** v's release starts with p's numbers. */
function prefixOf(p: Partial, v: Version): boolean {
  return p.parts.every((n, i) => (v.release[i] ?? 0) === n);
}

/** The first version above what a caret allows: ^1.2.3 → 2.0.0, ^0.2 → 0.3.0, ^0.0.3 → 0.0.4. */
function caretTop(p: Partial): Version {
  const parts = p.parts;
  let i = parts.findIndex((n) => n !== 0);
  if (i < 0) i = parts.length - 1;
  const top = parts.slice(0, i + 1);
  top[i]++;
  return { release: top, tag: -1, extra: "" };
}

/** The first version above what a tilde allows: ~1.2.3 → 1.3.0, ~1 → 2.0.0. */
function tildeTop(p: Partial): Version {
  const top =
    p.parts.length >= 2 ? [p.parts[0], p.parts[1] + 1] : [p.parts[0] + 1];
  return { release: top, tag: -1, extra: "" };
}

function holds(c: Comparator, v: Version, eco: Ecosystem): boolean | null {
  const cmp = compareVersions(v, floor(c.v));
  const exact = () =>
    c.v.partial ? prefixOf(c.v, v) : compareVersions(v, c.v.version) === 0;
  // A version just below a top's release, its pre-releases included.
  const below = (top: Version) => compareVersions(v, top) < 0;
  switch (c.op) {
    case "*":
      return true;
    case ">=":
      return cmp >= 0;
    case ">":
      return c.v.partial
        ? !prefixOf(c.v, v) && cmp > 0
        : compareVersions(v, c.v.version) > 0;
    case "<":
      return compareVersions(v, c.v.partial ? floor(c.v) : c.v.version) < 0;
    case "<=":
      return exact() || compareVersions(v, c.v.version) < 0;
    case "=":
    case "==":
      return exact();
    case "!=":
      return !exact();
    case "^":
      return cmp >= 0 && below(caretTop(c.v));
    case "~":
      return cmp >= 0 && below(tildeTop(c.v));
    case "~=":
      if (c.v.parts.length < 2) return null;
      return cmp >= 0 && prefixOf({ ...c.v, parts: c.v.parts.slice(0, -1) }, v);
    case "":
      return eco === "cargo" ? cmp >= 0 && below(caretTop(c.v)) : exact();
    default:
      return null;
  }
}

/**
 * Whether v satisfies a condition's range, or null when the range can't be
 * read.
 */
export function satisfies(
  v: Version,
  range: string,
  eco: Ecosystem,
): boolean | null {
  const alts = alternatives(range);
  if (!alts) return null;
  let unknown = false;
  for (const alt of alts) {
    if (!Array.isArray(alt)) {
      const ok =
        compareVersions(v, floor(alt.from)) >= 0 &&
        (prefixOf(alt.to, v) || compareVersions(v, alt.to.version) < 0);
      if (ok) return true;
      continue;
    }
    let all = true;
    for (const c of alt) {
      const h = holds(c, v, eco);
      if (h === null) {
        unknown = true;
        all = false;
        break;
      }
      if (!h) {
        all = false;
        break;
      }
    }
    if (all) return true;
  }
  return unknown ? null : false;
}
