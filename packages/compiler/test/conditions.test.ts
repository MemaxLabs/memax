// The condition evaluator (src/conditions/): the TOML reader, versions and
// ranges, the manifest readers, and evaluate over the golden vectors
// (testdata/conditions/vectors.json), which the server's evaluation is held
// to as well.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  checkNeeds,
  compareVersions,
  containsText,
  evaluate,
  findDeclared,
  lowestAllowed,
  parseToml,
  parseVersion,
  readManifest,
  satisfies,
  validPath,
  type CheckCondition,
  type CheckEvaluation,
  type ManifestKind,
  type TreeEntry,
  type TreeView,
} from "../src/index.js";

interface Vector {
  name: string;
  condition: CheckCondition;
  prev?: { resolved?: string };
  tree: {
    entries?: Record<string, TreeEntry | null>;
    texts?: Record<string, string>;
    files?: string[] | null;
  };
  want: CheckEvaluation;
}

const vectors = (
  JSON.parse(
    readFileSync(
      join(import.meta.dirname, "..", "testdata", "conditions", "vectors.json"),
      "utf8",
    ),
  ) as { vectors: Vector[] }
).vectors;

/** A tree view over a vector's tree. */
function view(t: Vector["tree"]): TreeView {
  const files = t.files === null ? null : [...(t.files ?? [])].sort();
  return {
    entry: (p) => (t.entries && p in t.entries ? t.entries[p] : undefined),
    text: (p) => t.texts?.[p],
    under: (prefix) =>
      files === null
        ? undefined
        : files.filter((f) => !prefix || f.startsWith(`${prefix}/`)),
    named: (name) =>
      files === null
        ? undefined
        : files.filter((f) => f === name || f.endsWith(`/${name}`)),
  };
}

describe("evaluate", () => {
  for (const v of vectors)
    it(v.name, () => {
      expect(evaluate(v.condition, view(v.tree), v.prev)).toEqual(v.want);
    });

  it("says what each kind needs from the tree", () => {
    expect(checkNeeds({ kind: "path_exists", path: "a/b.go" })).toEqual({
      entries: ["a/b.go"],
      texts: [],
      prefixes: [],
      names: [],
    });
    expect(
      checkNeeds({ kind: "path_exists", glob: "packages/web/**" }),
    ).toMatchObject({
      prefixes: ["packages/web"],
    });
    expect(
      checkNeeds({ kind: "file_contains", path: "a.go", text: "X" }),
    ).toMatchObject({ entries: ["a.go"], texts: ["a.go"] });
    expect(
      checkNeeds({ kind: "dep_present", manifest: "go.mod", name: "x" }),
    ).toMatchObject({ entries: [], texts: [], names: ["go.mod"] });
    expect(
      checkNeeds({ kind: "dep_present", manifest: "api/go.mod", name: "x" }),
    ).toMatchObject({ entries: ["api/go.mod"], texts: ["api/go.mod"] });
    expect(
      checkNeeds({
        kind: "file_unchanged",
        path: "a.md",
        blob: "ab".repeat(20),
      }),
    ).toMatchObject({ entries: ["a.md"], texts: [] });
    expect(
      checkNeeds({ kind: "pr_state", path: undefined } as CheckCondition),
    ).toBeNull();
  });

  it("matches whole words at the edges of the file and beside punctuation", () => {
    expect(containsText("TierHaiku", "TierHaiku", true)).toBe(true);
    expect(containsText("(TierHaiku)", "TierHaiku", true)).toBe(true);
    expect(containsText("é TierHaiku.x", "TierHaiku", true)).toBe(true);
    expect(containsText("xTierHaiku TierHaiku2", "TierHaiku", true)).toBe(
      false,
    );
    expect(containsText("a TierHaikuX TierHaiku", "TierHaiku", true)).toBe(
      true,
    );
  });

  it("names only paths the server takes as evidence", () => {
    expect(validPath("packages/web/src/app.tsx")).toBe(true);
    expect(validPath("docs/My File.md")).toBe(false);
    expect(validPath("../x")).toBe(false);
    expect(validPath("a//b")).toBe(false);
  });
});

describe("the TOML reader", () => {
  it("reads what pyproject.toml and Cargo.toml use", () => {
    const t = parseToml(`
# a comment
[project]
name = "x"   # trailing
description = """
Line one \\
  continued."""
dependencies = [
  "requests>=2.31",  # comment in an array
  'flask[async]~=3.0',
]
"quoted key" = 1
dotted.key = true
nums = [1, 2.5, -3, 1_000, 0x1f, 1e3]
when = 1979-05-27T07:32:00Z
inline = { a = "b", c = [ 1 ], }

[project.optional-dependencies]
dev = ["pytest"]

[[tool.mypy.overrides]]
module = "a"
[[tool.mypy.overrides]]
module = "b"

[target.'cfg(unix)'.dependencies]
libc = "0.2"

[dependencies.serde]
version = "1"
features = ["derive"]

[workspace]
dependencies.tokio.workspace = true
`);
    expect(t).not.toBeNull();
    const project = t!.project as Record<string, unknown>;
    expect(project.description).toBe("Line one continued.");
    expect(project.dependencies).toEqual([
      "requests>=2.31",
      "flask[async]~=3.0",
    ]);
    expect(project["quoted key"]).toBe(1);
    expect(project.dotted).toEqual({ key: true });
    expect(project.nums).toEqual([1, 2.5, -3, 1000, 31, 1000]);
    expect(project.when).toBe("1979-05-27T07:32:00Z");
    expect(project.inline).toEqual({ a: "b", c: [1] });
    expect(project["optional-dependencies"]).toEqual({ dev: ["pytest"] });
    expect(
      ((t!.tool as Record<string, unknown>).mypy as Record<string, unknown>)
        .overrides,
    ).toEqual([{ module: "a" }, { module: "b" }]);
    expect(t!.target).toEqual({
      "cfg(unix)": { dependencies: { libc: "0.2" } },
    });
    expect(t!.dependencies).toEqual({
      serde: { version: "1", features: ["derive"] },
    });
    expect(t!.workspace).toEqual({
      dependencies: { tokio: { workspace: true } },
    });
  });

  it("is null on what it can't read, and a __proto__ key is only a key", () => {
    for (const bad of [
      'a = "unterminated',
      "a = ",
      "[a",
      "a = [1, 2",
      "a = 1 b = 2",
      'a = "\\q"',
      "a = {x = 1",
      "[a]\nb = 1\n[a.b]\n",
    ])
      expect(parseToml(bad), bad).toBeNull();
    const t = parseToml('__proto__ = "x"\n[constructor]\na = 1\n');
    expect(t).not.toBeNull();
    expect(Object.getPrototypeOf(t)).toBeNull();
    expect(t!["__proto__"]).toBe("x");
    expect(({} as Record<string, unknown>).a).toBeUndefined();
  });
});

describe("versions and ranges", () => {
  it("orders pre-releases, releases and post-releases", () => {
    const order = [
      "1.0.0.dev1",
      "1.0.0-alpha",
      "1.0.0a2",
      "1.0.0-beta.2",
      "1.0.0-beta.11",
      "1.0.0rc1",
      "1.0.0",
      "1.0.0.post1",
      "1.0.1",
      "v1.2",
      "1.10.0",
    ];
    const parsed = order.map((s) => parseVersion(s)!);
    for (let i = 1; i < parsed.length; i++)
      expect(
        compareVersions(parsed[i - 1], parsed[i]),
        `${order[i - 1]} < ${order[i]}`,
      ).toBeLessThan(0);
    expect(
      compareVersions(
        parseVersion("v0.0.0-20230101120000-abcdef123456")!,
        parseVersion("v0.0.0")!,
      ),
    ).toBeLessThan(0);
    expect(compareVersions(parseVersion("1.2")!, parseVersion("1.2.0")!)).toBe(
      0,
    );
    for (const bad of ["", "latest", "1!2.0", "x.y", "1.2.3 4"])
      expect(parseVersion(bad), bad).toBeNull();
  });

  it("reads the lowest version a declared constraint allows", () => {
    const cases: Array<[string, string | null]> = [
      ["^1.2.3", "1.2.3"],
      ["~1.4", "1.4.0"],
      [">=2,<3", "2.0.0"],
      [">=2.1 <3", "2.1.0"],
      [">= 2.1", "2.1.0"],
      ["1.x", "1.0.0"],
      ["1.2.*", "1.2.0"],
      ["==1.2.*", "1.2.0"],
      ["~=1.4.2", "1.4.2"],
      ["^1 || ^2", "1.0.0"],
      ["1.2 - 2.0", "1.2.0"],
      ["v1.4.0", "1.4.0"],
      ["1.2.3", "1.2.3"],
      [">1.0", null],
      ["<2", null],
      ["*", null],
      ["", null],
      ["latest", null],
      ["workspace:*", null],
      ["catalog:", null],
      ["npm:other@^1", null],
      ["file:../x", null],
      ["git+https://github.com/a/b.git", null],
      ["github:a/b", null],
      ["===1.0", null],
    ];
    for (const [c, want] of cases) {
      const low = lowestAllowed(c);
      expect(
        low ? low.release.concat([0, 0, 0]).slice(0, 3).join(".") : null,
        c,
      ).toBe(want);
    }
    expect(lowestAllowed("v0.0.0-20230101120000-abcdef123456")?.tag).toBe(-1);
  });

  it("says whether a version satisfies a range", () => {
    const cases: Array<
      [string, string, boolean | null, ("npm" | "cargo" | "python" | "go")?]
    > = [
      ["1.2.3", ">=1.2", true],
      ["1.2.3", ">= 1.0", true],
      ["1.2.3", "^1", true],
      ["2.0.0", "^1", false],
      ["2.0.0-rc.1", "^1", false],
      ["0.2.5", "^0.2.3", true],
      ["0.3.0", "^0.2.3", false],
      ["0.0.3", "^0.0.3", true],
      ["0.0.4", "^0.0.3", false],
      ["1.4.5", "~1.4", true],
      ["1.5.0", "~1.4", false],
      ["1.4.2", "~=1.4.2", true],
      ["1.5.0", "~=1.4.2", false],
      ["1.5", "~=1.4", true],
      ["1.2.3", "1.2", true, "npm"],
      ["1.3.0", "1.2", false, "npm"],
      ["1.3.0", "1.2", true, "cargo"],
      ["2.0.0", "1.2", false, "cargo"],
      ["1.2.3", ">=1.0, <1.2", false],
      ["1.2.3", ">=1.3 || 1.2.x", true],
      ["1.2.3", "1.2 - 1.3", true],
      ["1.3.9", "1.2 - 1.3", true],
      ["1.4.0", "1.2 - 1.3", false],
      ["1.2.3", "!=1.2.3", false],
      ["1.2.4", "!=1.2.3", true],
      ["1.3.0", ">1.2", true],
      ["1.2.9", ">1.2", false],
      ["1.2.9", "<=1.2", true],
      ["1.3.0", "<=1.2", false],
      ["1.2.3", "*", true],
      ["1.2.3", "nonsense", null],
      ["1.2.3", "~=1", null],
      ["1.2.3", "workspace:*", null],
    ];
    for (const [v, range, want, eco] of cases)
      expect(
        satisfies(parseVersion(v)!, range, eco ?? "npm"),
        `${v} ${range}`,
      ).toBe(want);
  });
});

describe("the manifests", () => {
  const read = (kind: ManifestKind, text: string) => readManifest(kind, text);
  const names = (kind: ManifestKind, text: string) =>
    read(kind, text)?.deps.map((d) => [
      d.name,
      d.constraint,
      ...(d.also ? [d.also] : []),
    ]);

  it("go.mod: require lines and blocks, honouring replace", () => {
    const text = `module github.com/MemaxLabs/memax/packages/server

go 1.26

require github.com/google/uuid v1.6.0

require (
	github.com/jackc/pgx/v5 v5.7.1 // indirect
	"github.com/riverqueue/river" v0.14.2
	github.com/pgvector/pgvector-go v0.2.0
)

exclude (
	github.com/old/thing v1.0.0
)

retract [v1.0.0, v1.1.0]

replace github.com/riverqueue/river => github.com/fork/river v0.15.0
replace github.com/pgvector/pgvector-go v0.2.0 => ../pgvector-go
replace github.com/google/uuid v1.0.0 => github.com/google/uuid v1.1.0
`;
    expect(names("go.mod", text)).toEqual([
      ["github.com/google/uuid", "v1.6.0"],
      ["github.com/jackc/pgx/v5", "v5.7.1"],
      ["github.com/riverqueue/river", "v0.15.0"],
      ["github.com/pgvector/pgvector-go", null],
    ]);
    expect(read("go.mod", "require (\n\tgithub.com/x\n)\n")).toBeNull();
    expect(read("go.mod", "require (\n\tgithub.com/x v1\n")).toBeNull();
  });

  it("package.json: the four dependency fields", () => {
    const text = JSON.stringify({
      name: "x",
      dependencies: { next: "^16.0.3", "memax-sdk": "workspace:*" },
      devDependencies: { vitest: "^3" },
      peerDependencies: { react: ">=19" },
      optionalDependencies: { fsevents: "2.3.3" },
      overrides: { lodash: "4.17.21" },
      scripts: { build: "next build" },
    });
    expect(names("package.json", text)).toEqual([
      ["next", "^16.0.3"],
      ["memax-sdk", "workspace:*"],
      ["vitest", "^3"],
      ["react", ">=19"],
      ["fsevents", "2.3.3"],
    ]);
    expect(read("package.json", "[1]")).toBeNull();
    expect(read("package.json", "{")).toBeNull();
  });

  it("pyproject.toml: PEP 621, PEP 735 and Poetry", () => {
    const text = `
[project]
name = "x"
dependencies = ["requests[security]>=2.31; python_version>='3.9'", "attrs (>=23)", "pkg @ https://example.com/pkg.whl"]
[project.optional-dependencies]
docs = ["mkdocs~=1.6"]
[dependency-groups]
test = ["pytest>=8", { include-group = "docs" }]
[tool.poetry.dependencies]
python = "^3.11"
httpx = "^0.27"
pydantic = { version = "^2.7", extras = ["email"] }
local = { path = "../local" }
[tool.poetry.group.lint.dependencies]
ruff = "0.6.9"
`;
    expect(names("pyproject.toml", text)).toEqual([
      ["requests", ">=2.31"],
      ["attrs", ">=23"],
      ["pkg", null],
      ["mkdocs", "~=1.6"],
      ["pytest", ">=8"],
      ["httpx", "^0.27"],
      ["pydantic", "^2.7"],
      ["local", null],
      ["ruff", "0.6.9"],
    ]);
    expect(read("pyproject.toml", text)?.complete).toBe(true);
    expect(
      read("pyproject.toml", '[project]\ndynamic = ["dependencies"]\n')
        ?.complete,
    ).toBe(false);
    expect(read("pyproject.toml", "[project\n")).toBeNull();
    const m = read("pyproject.toml", text)!;
    expect(findDeclared(m, "Requests").map((d) => d.name)).toEqual([
      "requests",
    ]);
  });

  it("Cargo.toml: every dependency table, a rename by its crate", () => {
    const text = `
[package]
name = "x"
[dependencies]
serde = "1.0"
json = { package = "serde_json", version = "1" }
local = { path = "../local" }
[dev-dependencies.tokio]
version = "1.40"
[build-dependencies]
cc = "1"
[target.'cfg(windows)'.dependencies]
winapi = "0.3"
[workspace.dependencies]
anyhow = "1"
[[bin]]
name = "x"
`;
    expect(names("Cargo.toml", text)).toEqual([
      ["serde", "1.0"],
      ["json", "1", "serde_json"],
      ["local", null],
      ["tokio", "1.40"],
      ["cc", "1"],
      ["anyhow", "1"],
      ["winapi", "0.3"],
    ]);
    const m = read("Cargo.toml", text)!;
    expect(findDeclared(m, "serde-json").map((d) => d.name)).toEqual(["json"]);
  });

  it("requirements.txt: lines, continuations, editables and what it includes", () => {
    const text = `# pinned
requests==2.31.0 \\
    --hash=sha256:abc
Flask>=3.0  # web
-e git+https://github.com/a/b.git#egg=b-lib
--index-url https://pypi.org/simple
uvicorn[standard] ; python_version >= "3.9"
`;
    expect(names("requirements.txt", text)).toEqual([
      ["requests", "==2.31.0"],
      ["Flask", ">=3.0"],
      ["b-lib", null],
      ["uvicorn", ""],
    ]);
    expect(read("requirements.txt", text)?.complete).toBe(true);
    expect(read("requirements.txt", "-r base.txt\nflask\n")?.complete).toBe(
      false,
    );
    expect(
      read("requirements.txt", "https://example.com/x.whl\n")?.complete,
    ).toBe(false);
    expect(read("requirements.txt", "-e .\n")?.complete).toBe(false);
  });
});
