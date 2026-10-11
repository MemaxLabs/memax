// Copied verbatim from packages/compiler/src/conditions/manifests.ts by scripts/sync-compiler.mjs.
// Don't edit it here: change the compiler, then run the script.

// The manifests dep_present reads (the stale-detection design §1.1): what
// each declares, by name, with its declared constraint as written. Declared
// constraints, never lockfiles.
//
//   go.mod            require (single lines and blocks), honouring replace:
//                     a replacement's version is the constraint, and a
//                     replacement by a local path has none to read
//   package.json      dependencies, devDependencies, peerDependencies,
//                     optionalDependencies
//   pyproject.toml    PEP 621's project.dependencies and
//                     optional-dependencies, PEP 735's dependency-groups,
//                     Poetry's dependencies, dev-dependencies and groups
//   Cargo.toml        every dependency table (dev-, build-, workspace.,
//                     target.*.), a renamed one under its key and its crate
//                     (package =)
//   requirements.txt  one requirement a line, with -e's #egg= names
//
// A manifest that takes dependencies from elsewhere (requirements' -r and
// -c, a line it can't name, pyproject's dynamic dependencies) is
// incomplete: a dependency missing from it may be there, so the check says
// unknown. A manifest it can't read at all is null, unknown too.
import {
  isTable,
  parseToml,
  tomlAt,
  type TomlTable,
  type TomlValue,
} from "./toml.js";
import type { Ecosystem } from "./versions.js";

export const MANIFESTS = [
  "go.mod",
  "package.json",
  "pyproject.toml",
  "Cargo.toml",
  "requirements.txt",
] as const;
export type ManifestKind = (typeof MANIFESTS)[number];

/** One dependency a manifest declares. */
export interface Declared {
  /** The name it is declared under. */
  name: string;
  /** Another name it answers to: a renamed Cargo dependency's crate. */
  also?: string;
  /**
   * Its declared constraint as written ("^1.2.0", "v1.4.0", ">=2,<3"; ""
   * for none), or null when it isn't a version (a path, a git source, a
   * URL, workspace = true).
   */
  constraint: string | null;
}

export interface Manifest {
  kind: ManifestKind;
  deps: Declared[];
  /** False when it takes dependencies from elsewhere. */
  complete: boolean;
}

/** A path's manifest kind, by its last segment, or null. */
export function manifestKind(path: string): ManifestKind | null {
  const base = path.slice(path.lastIndexOf("/") + 1);
  return (MANIFESTS as readonly string[]).includes(base)
    ? (base as ManifestKind)
    : null;
}

export function ecosystemOf(kind: ManifestKind): Ecosystem {
  switch (kind) {
    case "go.mod":
      return "go";
    case "package.json":
      return "npm";
    case "Cargo.toml":
      return "cargo";
    default:
      return "python";
  }
}

/** What the manifest declares, or null when it can't be read. */
export function readManifest(
  kind: ManifestKind,
  text: string,
): Manifest | null {
  switch (kind) {
    case "go.mod":
      return goMod(text);
    case "package.json":
      return packageJson(text);
    case "pyproject.toml":
      return pyproject(text);
    case "Cargo.toml":
      return cargo(text);
    case "requirements.txt":
      return requirements(text);
  }
}

/** A name as each ecosystem compares names. */
function normalize(kind: ManifestKind, name: string): string {
  switch (ecosystemOf(kind)) {
    case "python":
      return name.toLowerCase().replace(/[-_.]+/g, "-"); // PEP 503
    case "cargo":
      return name.toLowerCase().replace(/_/g, "-");
    default:
      return name;
  }
}

/** Every declaration of a dependency (one in dependencies and devDependencies, say). */
export function findDeclared(m: Manifest, name: string): Declared[] {
  const want = normalize(m.kind, name);
  return m.deps.filter(
    (d) =>
      normalize(m.kind, d.name) === want ||
      (d.also !== undefined && normalize(m.kind, d.also) === want),
  );
}

// --- go.mod ---

/** A go.mod line's words, quoted module paths unquoted, its comment gone. */
function goWords(line: string): string[] {
  const words: string[] = [];
  const re = /"((?:[^"\\]|\\.)*)"|`([^`]*)`|(\S+)/g;
  let rest = line;
  const comment = rest.indexOf("//");
  if (comment >= 0) rest = rest.slice(0, comment);
  for (let m = re.exec(rest); m; m = re.exec(rest))
    words.push(m[1] ?? m[2] ?? m[3]);
  return words;
}

interface Replace {
  from: string;
  fromVersion?: string;
  toVersion?: string;
}

function goMod(text: string): Manifest | null {
  const deps: Declared[] = [];
  const replaces: Replace[] = [];
  let block: string | null = null;
  const require = (w: string[]) => {
    if (w.length !== 2) return false;
    deps.push({ name: w[0], constraint: w[1] });
    return true;
  };
  const replace = (w: string[]) => {
    const arrow = w.indexOf("=>");
    if (
      arrow < 1 ||
      arrow > 2 ||
      w.length - arrow - 1 < 1 ||
      w.length - arrow - 1 > 2
    )
      return false;
    const r: Replace = { from: w[0] };
    if (arrow === 2) r.fromVersion = w[1];
    // A local directory has no version: "=> ../river".
    if (w.length - arrow - 1 === 2) r.toVersion = w[arrow + 2];
    replaces.push(r);
    return true;
  };
  for (const raw of text.split(/\r?\n/)) {
    const w = goWords(raw);
    if (w.length === 0) continue;
    if (block !== null) {
      if (w.length === 1 && w[0] === ")") block = null;
      else if (block === "require" && !require(w)) return null;
      else if (block === "replace" && !replace(w)) return null;
      continue;
    }
    const [verb, ...rest] = w;
    if (rest.length === 1 && rest[0] === "(") {
      block = verb;
      continue;
    }
    if (verb === "require" && !require(rest)) return null;
    if (verb === "replace" && !replace(rest)) return null;
    // module, go, toolchain, exclude, retract, tool, godebug: not dependencies.
  }
  if (block !== null) return null;
  for (const d of deps) {
    // A replacement of this version wins over one of every version.
    const r =
      replaces.find(
        (x) => x.from === d.name && x.fromVersion === d.constraint,
      ) ??
      replaces.find((x) => x.from === d.name && x.fromVersion === undefined);
    if (r) d.constraint = r.toVersion ?? null;
  }
  return { kind: "go.mod", deps, complete: true };
}

// --- package.json ---

const NPM_FIELDS = [
  "dependencies",
  "devDependencies",
  "peerDependencies",
  "optionalDependencies",
];

function packageJson(text: string): Manifest | null {
  let j: unknown;
  try {
    j = JSON.parse(text);
  } catch {
    return null;
  }
  if (typeof j !== "object" || j === null || Array.isArray(j)) return null;
  const deps: Declared[] = [];
  for (const field of NPM_FIELDS) {
    const o = (j as Record<string, unknown>)[field];
    if (typeof o !== "object" || o === null || Array.isArray(o)) continue;
    for (const [name, v] of Object.entries(o as Record<string, unknown>))
      deps.push({ name, constraint: typeof v === "string" ? v : null });
  }
  return { kind: "package.json", deps, complete: true };
}

// --- Python: PEP 508 requirements ---

const PEP508 =
  /^\s*([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(\[[^\]]*\])?\s*(.*)$/;

/** A requirement's name and specifier ("" for none, null for a URL). */
function pep508(s: string): Declared | null {
  const m = PEP508.exec(s);
  if (!m) return null;
  let rest = m[3];
  const semi = rest.indexOf(";");
  if (semi >= 0) rest = rest.slice(0, semi);
  rest = rest.trim();
  if (rest.startsWith("@")) return { name: m[1], constraint: null };
  const paren = /^\((.*)\)$/.exec(rest);
  if (paren) rest = paren[1].trim();
  if (rest !== "" && !/^[<>=!~]/.test(rest)) return null;
  return { name: m[1], constraint: rest };
}

function pep508List(v: TomlValue | undefined, deps: Declared[]): void {
  if (!Array.isArray(v)) return;
  for (const s of v) {
    if (typeof s !== "string") continue; // {include-group = "…"}: the group is read on its own
    const d = pep508(s);
    if (d) deps.push(d);
  }
}

/** Poetry's table of dependencies: a constraint, a table with a version, or several. */
function poetryTable(v: TomlValue | undefined, deps: Declared[]): void {
  if (!isTable(v)) return;
  for (const [name, spec] of Object.entries(v)) {
    if (name.toLowerCase() === "python") continue; // the interpreter, not a dependency
    let constraint: string | null = null;
    if (typeof spec === "string") constraint = spec;
    else if (isTable(spec) && typeof spec.version === "string")
      constraint = spec.version;
    deps.push({ name, constraint });
  }
}

function pyproject(text: string): Manifest | null {
  const t = parseToml(text);
  if (!t) return null;
  const deps: Declared[] = [];
  let complete = true;
  const project = tomlAt(t, "project");
  if (isTable(project)) {
    const dynamic = project.dynamic;
    if (
      Array.isArray(dynamic) &&
      dynamic.some((d) => d === "dependencies" || d === "optional-dependencies")
    )
      complete = false;
    pep508List(project.dependencies, deps);
    const optional = project["optional-dependencies"];
    if (isTable(optional))
      for (const list of Object.values(optional)) pep508List(list, deps);
  }
  const groups = tomlAt(t, "dependency-groups");
  if (isTable(groups))
    for (const list of Object.values(groups)) pep508List(list, deps);
  const poetry = tomlAt(t, "tool", "poetry");
  if (isTable(poetry)) {
    poetryTable(poetry.dependencies, deps);
    poetryTable(poetry["dev-dependencies"], deps);
    const group = poetry.group;
    if (isTable(group))
      for (const g of Object.values(group))
        if (isTable(g)) poetryTable(g.dependencies, deps);
  }
  return { kind: "pyproject.toml", deps, complete };
}

function requirements(text: string): Manifest | null {
  const deps: Declared[] = [];
  let complete = true;
  // A backslash at a line's end continues it.
  const lines = text.replace(/\\\r?\n/g, " ").split(/\r?\n/);
  for (const raw of lines) {
    const line = raw.replace(/(^|\s)#.*$/, "").trim();
    if (line === "") continue;
    if (/^(-r|--requirement|-c|--constraint)(\s|=|$)/.test(line)) {
      complete = false;
      continue;
    }
    const editable = /^(-e|--editable)(\s+|=)(\S+)/.exec(line);
    if (editable) {
      const egg = /#egg=([A-Za-z0-9._-]+)/.exec(raw);
      if (egg) deps.push({ name: egg[1], constraint: null });
      else complete = false;
      continue;
    }
    if (line.startsWith("-")) continue; // --index-url, --hash, --pre, …
    // Options after a requirement: "requests==2.31 --hash=sha256:…".
    const req = line.split(/\s+--?[A-Za-z]/)[0];
    const d =
      /^[A-Za-z0-9]/.test(req) && !/^[A-Za-z][A-Za-z0-9+.-]*:\/\//.test(req)
        ? pep508(req)
        : null;
    if (d) deps.push(d);
    else complete = false; // a URL or a path: whatever it installs has no name here
  }
  return { kind: "requirements.txt", deps, complete };
}

// --- Cargo.toml ---

const CARGO_TABLES = [
  "dependencies",
  "dev-dependencies",
  "build-dependencies",
  "dev_dependencies",
  "build_dependencies",
];

function cargoTables(scope: TomlTable, deps: Declared[]): void {
  for (const key of CARGO_TABLES) {
    const table = scope[key];
    if (!isTable(table)) continue;
    for (const [name, spec] of Object.entries(table)) {
      if (typeof spec === "string") {
        deps.push({ name, constraint: spec });
        continue;
      }
      if (!isTable(spec)) continue;
      const d: Declared = {
        name,
        constraint: typeof spec.version === "string" ? spec.version : null,
      };
      if (typeof spec.package === "string") d.also = spec.package;
      deps.push(d);
    }
  }
}

function cargo(text: string): Manifest | null {
  const t = parseToml(text);
  if (!t) return null;
  const deps: Declared[] = [];
  cargoTables(t, deps);
  const workspace = t.workspace;
  if (isTable(workspace)) cargoTables(workspace, deps);
  const target = t.target;
  if (isTable(target))
    for (const cfg of Object.values(target))
      if (isTable(cfg)) cargoTables(cfg, deps);
  return { kind: "Cargo.toml", deps, complete: true };
}
