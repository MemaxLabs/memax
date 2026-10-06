/**
 * Input validation. The compiler takes JSON from outside (the CLI's local
 * record, the compile service's request), so it checks everything, reports
 * every problem at once, and refuses anything that must never compile:
 * proposals, other non-kept states and quarantined content.
 */
import { getAdapter, isAdapterKind } from "./adapters/index.js";
import {
  DEFAULT_HEADINGS,
  type Fact,
  type Model,
  type ModelItem,
  type ModelSection,
  type Target,
} from "./model.js";
import { findManagedBlock, ManagedBlockError } from "./managed-block.js";
import { cleanLine } from "./sanitize.js";
import { formatStamp, isRepoPath, parseTime } from "./text.js";
import { CONTRACT_VERSION, type CompileWarning } from "./types.js";

export interface Issue {
  /** Where in the input, for example `memories[3].state`. */
  path: string;
  message: string;
}

/** Thrown when a {@link CompileInput} is malformed or carries what may not compile. */
export class CompileInputError extends Error {
  readonly issues: Issue[];

  constructor(issues: Issue[]) {
    const more = issues.length > 1 ? ` (and ${issues.length - 1} more)` : "";
    super(
      `Invalid compile input: ${issues[0].path}: ${issues[0].message}${more}`,
    );
    this.name = "CompileInputError";
    this.issues = issues;
  }
}

export const MEMORY_REF = /^M-\d+$/;
const BRIEF_ID = /^B-\d+$/;
const COMPILE_ID = /^C-\d+$/;
const SLUG = /^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/;
const KEY = /^[a-z][a-z0-9_-]{0,39}$/;
const AGENT = /^[a-z][a-z0-9-]{0,39}$/;
const URL = /^https?:\/\/[A-Za-z0-9\-._~:/?#[\]@!$&()*+;=%]+$/;
/** Globs: no spaces, quotes, commas, braces, colons or `#`, so every format can carry them unquoted or quoted. */
const GLOB = /^[A-Za-z0-9._\-/*?[\]!@+~]+$/;

const SPACE_KINDS = ["personal", "project", "team"];
const MEMORY_KINDS = ["fact", "decision"];
const TRUST = ["person", "agent_own_work", "repository"];
const FLAGS = ["stale", "conflict"];
const MIN_BUDGET = 1024;

type Json = Record<string, unknown>;

const isObject = (v: unknown): v is Json =>
  typeof v === "object" && v !== null && !Array.isArray(v);

class Checker {
  readonly issues: Issue[] = [];
  readonly warnings: CompileWarning[] = [];

  fail(path: string, message: string): void {
    this.issues.push({ path, message });
  }

  object(v: unknown, path: string): Json {
    if (isObject(v)) return v;
    this.fail(path, "must be an object");
    return {};
  }

  array(v: unknown, path: string): unknown[] {
    if (Array.isArray(v)) return v;
    this.fail(path, "must be an array");
    return [];
  }

  pattern(v: unknown, path: string, re: RegExp, hint: string): string {
    if (typeof v === "string" && re.test(v)) return v;
    this.fail(path, `must be ${hint}`);
    return "";
  }

  oneOf<T extends string>(
    v: unknown,
    path: string,
    options: readonly string[],
    fallback?: T,
  ): T {
    if (v === undefined && fallback !== undefined) return fallback;
    if (typeof v === "string" && options.includes(v)) return v as T;
    this.fail(path, `must be one of ${options.join(", ")}`);
    return (fallback ?? options[0]) as T;
  }

  /** Clean one line of text, warn about hidden characters, enforce a length. */
  text(
    v: unknown,
    path: string,
    max: number,
    what: string,
    ref?: string,
  ): string {
    if (typeof v !== "string") {
      this.fail(path, "must be a string");
      return "";
    }
    const { text, removed } = cleanLine(v);
    if (removed > 0) {
      const chars = removed === 1 ? "character" : "characters";
      this.warnings.push({
        code: "hidden_characters",
        message: `Removed ${removed} hidden ${chars} from ${what}.`,
        ...(ref ? { ref } : {}),
      });
    }
    if (text === "")
      this.fail(path, "is empty once hidden characters are removed");
    if (text.length > max) this.fail(path, `is longer than ${max} characters`);
    return text;
  }

  sortedSet(v: unknown, path: string, re: RegExp, hint: string): string[] {
    if (v === undefined) return [];
    const values = this.array(v, path).map((x, i) =>
      this.pattern(x, `${path}[${i}]`, re, hint),
    );
    return [...new Set(values)].sort();
  }
}

/** Validates and normalises a compile input, or throws {@link CompileInputError}. */
export function validate(input: unknown): Model {
  const c = new Checker();
  const root = c.object(input, "input");

  if (root.version !== CONTRACT_VERSION) {
    c.fail(
      "version",
      `must be ${CONTRACT_VERSION}; this compiler reads contract version ${CONTRACT_VERSION}`,
    );
  }
  if (
    Array.isArray(root.proposals)
      ? root.proposals.length > 0
      : root.proposals !== undefined
  ) {
    c.fail("proposals", "proposals never compile; leave them out of the input");
  }

  const run = c.object(root.compile, "compile");
  const compileId = c.pattern(
    run.id,
    "compile.id",
    COMPILE_ID,
    "a compile ID like C-0881",
  );
  const time = parseTime(run.at);
  if (time === null)
    c.fail(
      "compile.at",
      "must be an ISO 8601 time with a zone, like 2026-10-05T14:31:00Z",
    );

  const s = c.object(root.space, "space");
  const space = {
    slug: c.pattern(
      s.slug,
      "space.slug",
      SLUG,
      "a lowercase slug like memax-v2",
    ),
    name: c.text(s.name, "space.name", 120, "the space name"),
    kind: c.oneOf<Model["space"]["kind"]>(s.kind, "space.kind", SPACE_KINDS),
    url:
      typeof s.url === "string" && s.url.length <= 300 && URL.test(s.url)
        ? s.url
        : "",
  };
  if (space.url === "")
    c.fail("space.url", "must be an http(s) URL of at most 300 characters");

  const brief = checkBrief(c, root.brief);
  const sectionKeys = new Set(brief.sections.map((s) => s.key));

  const facts = new Map<string, Fact>();
  c.array(root.memories, "memories").forEach((m, i) => {
    const fact = checkMemory(c, m, `memories[${i}]`, time ?? 0);
    if (!fact) return;
    if (facts.has(fact.ref))
      c.fail(`memories[${i}].ref`, `${fact.ref} appears more than once`);
    if (
      fact.section &&
      !sectionKeys.has(fact.section) &&
      !(fact.section in DEFAULT_HEADINGS)
    ) {
      c.fail(
        `memories[${i}].section`,
        `${fact.section} isn't a Brief section or a well-known one`,
      );
    }
    facts.set(fact.ref, fact);
  });

  const targets = c
    .array(root.targets, "targets")
    .map((t, i) => checkTarget(c, t, `targets[${i}]`));
  if (targets.length === 0) c.fail("targets", "must list at least one target");
  const paths = new Map<string, number>();
  targets.forEach((t, i) => {
    if (t.kind === "chatgpt") return;
    if (paths.has(t.path))
      c.fail(
        `targets[${i}].path`,
        `${t.path} is already a target (targets[${paths.get(t.path)}])`,
      );
    paths.set(t.path, i);
  });

  if (c.issues.length > 0) throw new CompileInputError(c.issues);

  return {
    compileId,
    compiledAt: run.at as string,
    stamp: formatStamp(time as number),
    time: time as number,
    space,
    brief,
    facts,
    targets,
    warnings: c.warnings,
  };
}

function checkMemory(
  c: Checker,
  value: unknown,
  path: string,
  now: number,
): Fact | null {
  const m = c.object(value, path);
  const ref = c.pattern(
    m.ref,
    `${path}.ref`,
    MEMORY_REF,
    "a memory ref like M-0219",
  );
  const who = ref || path;

  // Defensive: whatever the server's row looked like, only kept memories
  // and open questions may reach a compiled file.
  if (
    m.state === "proposed" ||
    (m.lifecycle !== undefined && m.lifecycle !== "kept")
  ) {
    c.fail(
      `${path}.state`,
      `${who} isn't kept; proposals and other non-kept memories never compile`,
    );
    return null;
  }
  if (m.trust === "external" || m.quarantined === true || m.external === true) {
    c.fail(
      `${path}.trust`,
      `${who} is quarantined (an external source); quarantined content never compiles`,
    );
    return null;
  }
  if (Array.isArray(m.flags) && m.flags.includes("quarantined")) {
    c.fail(
      `${path}.flags`,
      `${who} is quarantined; quarantined content never compiles`,
    );
    return null;
  }

  const state = c.oneOf<Fact["state"]>(m.state, `${path}.state`, [
    "kept",
    "open",
  ]);
  c.oneOf(m.kind, `${path}.kind`, MEMORY_KINDS);
  if (m.trust !== undefined) c.oneOf(m.trust, `${path}.trust`, TRUST);
  const flags = c.sortedSet(
    m.flags,
    `${path}.flags`,
    /^(stale|conflict)$/,
    `one of ${FLAGS.join(", ")}`,
  );

  let score = 0;
  if (m.read_score !== undefined) {
    if (
      typeof m.read_score === "number" &&
      Number.isFinite(m.read_score) &&
      m.read_score >= 0
    ) {
      score = m.read_score;
    } else {
      c.fail(`${path}.read_score`, "must be a finite number, zero or more");
    }
  }

  let pastStaleAfter = false;
  if (m.stale_after !== undefined) {
    const t = parseTime(m.stale_after);
    if (t === null)
      c.fail(`${path}.stale_after`, "must be an ISO 8601 time with a zone");
    else pastStaleAfter = t <= now;
  }

  const scope = m.scope === undefined ? {} : c.object(m.scope, `${path}.scope`);
  const paths = c.sortedSet(
    scope.paths,
    `${path}.scope.paths`,
    GLOB,
    "a repository glob with no spaces, commas, braces, quotes, colons or #",
  );
  paths.forEach((g, i) => {
    if (g.startsWith("!") || !isRepoPath(g.replace(/[*?[\]!]+/g, "x"))) {
      c.fail(
        `${path}.scope.paths[${i}]`,
        `${g} must be a positive glob relative to the repository root, with no "..", leading or trailing "/"`,
      );
    }
  });

  return {
    ref,
    text: c.text(m.statement, `${path}.statement`, 2000, ref, ref),
    section: c.pattern(
      m.section,
      `${path}.section`,
      KEY,
      "a section key like decisions",
    ),
    state,
    stale: flags.includes("stale") || pastStaleAfter,
    conflict: flags.includes("conflict"),
    paths,
    agents: c.sortedSet(
      scope.agents,
      `${path}.scope.agents`,
      AGENT,
      "an agent ID like claude-code",
    ),
    score,
  };
}

function checkBrief(c: Checker, value: unknown): Model["brief"] {
  const b = c.object(value, "brief");
  const keys = new Set<string>();
  const placed = new Set<string>();
  const sections = c
    .array(b.sections, "brief.sections")
    .map((v, i): ModelSection => {
      const path = `brief.sections[${i}]`;
      const s = c.object(v, path);
      const key = c.pattern(
        s.key,
        `${path}.key`,
        KEY,
        "a section key like decisions",
      );
      if (keys.has(key)) c.fail(`${path}.key`, `${key} appears more than once`);
      keys.add(key);
      const items = c.array(s.items, `${path}.items`).map((value, j) => {
        const item = checkItem(c, value, `${path}.items[${j}]`);
        if (item.kind === "memory" && item.ref) {
          if (placed.has(item.ref))
            c.fail(
              `${path}.items[${j}].ref`,
              `${item.ref} is placed more than once`,
            );
          placed.add(item.ref);
        }
        return item;
      });
      return {
        key,
        heading: c.text(s.heading, `${path}.heading`, 80, `the ${key} heading`),
        items,
      };
    });
  return {
    id: c.pattern(b.id, "brief.id", BRIEF_ID, "a Brief ID like B-0043"),
    title: c.text(b.title, "brief.title", 200, "the Brief title"),
    summary:
      b.summary === undefined
        ? null
        : c.text(b.summary, "brief.summary", 500, "the Brief summary"),
    sections,
  };
}

function checkItem(c: Checker, value: unknown, path: string): ModelItem {
  const item = c.object(value, path);
  if (item.quarantined === true || item.state === "proposed") {
    c.fail(path, "proposals and quarantined content never compile");
  }
  if (typeof item.ref === "string" || item.text === undefined) {
    return {
      kind: "memory",
      ref: c.pattern(
        item.ref,
        `${path}.ref`,
        MEMORY_REF,
        "a memory ref like M-0219",
      ),
    };
  }
  const cites = c
    .array(item.cites, `${path}.cites`)
    .map((r, k) =>
      c.pattern(
        r,
        `${path}.cites[${k}]`,
        MEMORY_REF,
        "a memory ref like M-0219",
      ),
    );
  if (cites.length === 0)
    c.fail(`${path}.cites`, "connective prose must cite at least one memory");
  return {
    kind: "prose",
    text: c.text(
      item.text,
      `${path}.text`,
      500,
      "connective prose in the Brief",
    ),
    cites: [...new Set(cites)],
  };
}

function checkTarget(c: Checker, value: unknown, path: string): Target {
  const t = c.object(value, path);
  if (!isAdapterKind(t.kind)) {
    c.fail(`${path}.kind`, "must be a known adapter kind");
    return defaults("agents_md");
  }
  const adapter = getAdapter(t.kind);
  const target = defaults(t.kind);
  const only = (field: string, ok: boolean, who: string) => {
    if (t[field] !== undefined && !ok)
      c.fail(`${path}.${field}`, `only applies to ${who}`);
  };

  if (t.path !== undefined) {
    if (
      typeof t.path === "string" &&
      isRepoPath(t.path) &&
      adapter.acceptsPath(t.path)
    )
      target.path = t.path;
    else
      c.fail(
        `${path}.path`,
        `must be a repository-relative path ${adapter.pathHint}`,
      );
  }
  target.include = c.oneOf(
    t.include,
    `${path}.include`,
    ["kept_only", "kept_and_open"],
    target.include,
  );
  target.stale = c.oneOf(
    t.stale,
    `${path}.stale`,
    ["mark", "omit"],
    target.stale,
  );
  if (t.size_budget !== undefined) {
    if (
      Number.isInteger(t.size_budget) &&
      (t.size_budget as number) >= MIN_BUDGET
    )
      target.budget = t.size_budget as number;
    else
      c.fail(
        `${path}.size_budget`,
        `must be a whole number of bytes, at least ${MIN_BUDGET}`,
      );
  }
  if (t.sections !== undefined)
    target.sections = c.sortedSet(
      t.sections,
      `${path}.sections`,
      KEY,
      "a section key",
    );

  const shim = adapter.role === "shim";
  const canonical = adapter.role === "canonical" || adapter.role === "copy";
  only("scoped", canonical, "agents_md and chatgpt");
  only("user_owned", shim, "shims (claude_md, gemini_md)");
  only("current", shim, "shims (claude_md, gemini_md)");
  only("canonical_path", !canonical, "shims and scoped adapters");
  if (t.scoped !== undefined)
    target.scoped = c.oneOf(t.scoped, `${path}.scoped`, ["inline", "omit"]);
  if (t.canonical_path !== undefined) {
    if (
      typeof t.canonical_path === "string" &&
      isRepoPath(t.canonical_path) &&
      t.canonical_path.endsWith("AGENTS.md")
    ) {
      target.canonical = t.canonical_path;
    } else
      c.fail(
        `${path}.canonical_path`,
        "must be a repository-relative path to an AGENTS.md",
      );
  }
  if (t.user_owned !== undefined) {
    if (typeof t.user_owned === "boolean") target.userOwned = t.user_owned;
    else c.fail(`${path}.user_owned`, "must be true or false");
  }
  if (t.current !== undefined) {
    if (typeof t.current === "string") target.current = t.current;
    else c.fail(`${path}.current`, "must be the file's content as a string");
  }
  if (t.current !== undefined && !target.userOwned)
    c.fail(`${path}.current`, "only applies when user_owned is true");
  if (target.userOwned) {
    try {
      findManagedBlock(target.current);
    } catch (err) {
      if (!(err instanceof ManagedBlockError)) throw err;
      c.fail(`${path}.current`, `${target.path}: ${err.message}`);
    }
  }
  return target;
}

function defaults(kind: Target["kind"]): Target {
  return {
    kind,
    path: getAdapter(kind).defaultPath ?? "",
    include: "kept_and_open",
    stale: "mark",
    budget: null,
    sections: null,
    scoped: "inline",
    canonical: "AGENTS.md",
    userOwned: false,
    current: "",
  };
}
