// readObjects on a git without `cat-file --batch-command` (before 2.36): a
// git on PATH that refuses the option, in front of the real one. The
// checks' git environment is built once a process, so this has a file of
// its own.
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import { readObjects, type GitCost } from "../../src/lib/daemon/git-tree.js";

const base = realpathSync(mkdtempSync(join(tmpdir(), "memax-old-git-")));
const real = execFileSync("sh", ["-c", "command -v git"]).toString().trim();
const bin = join(base, "bin");
mkdirSync(bin);
writeFileSync(
  join(bin, "git"),
  `#!/bin/sh
for a in "$@"; do
  if [ "$a" = "--batch-command" ]; then
    echo "error: unknown option batch-command" >&2
    exit 129
  fi
done
exec "${real}" "$@"
`,
);
chmodSync(join(bin, "git"), 0o755);
process.env.PATH = `${bin}:${process.env.PATH}`;

afterAll(() => rmSync(base, { recursive: true, force: true }));

describe("readObjects on an older git", () => {
  it("reads the infos, then the contents, in two processes", async () => {
    const root = join(base, "repo");
    const env = {
      ...process.env,
      GIT_AUTHOR_NAME: "Test",
      GIT_AUTHOR_EMAIL: "test@example.com",
      GIT_COMMITTER_NAME: "Test",
      GIT_COMMITTER_EMAIL: "test@example.com",
      HOME: base,
    };
    const git = (...args: string[]) =>
      execFileSync(real, args, { cwd: root, env }).toString().trim();
    mkdirSync(join(root, "docs"), { recursive: true });
    writeFileSync(join(root, "go.mod"), "module x\n");
    writeFileSync(join(root, "docs", "a.md"), "# A\n");
    execFileSync(real, ["init", "-q", "-b", "main", root], { env });
    git("add", "-A");
    git("commit", "-q", "-m", "first");
    const commit = git("rev-parse", "HEAD");
    const cost: GitCost = { processes: 0, bytes: 0 };
    const blobs = new Map<string, string>();
    const entries = await readObjects(
      root,
      commit,
      ["go.mod", "docs", "nope.go"],
      (es) => {
        const e = es.get("go.mod");
        return e?.type === "blob" ? [e.id] : [];
      },
      (id, contents) => blobs.set(id, contents.toString()),
      { fileBytes: 1 << 20, totalBytes: 1 << 20 },
      cost,
    );
    expect(entries.get("go.mod")).toMatchObject({ type: "blob", size: 9 });
    expect(entries.get("docs")?.type).toBe("tree");
    expect(entries.get("nope.go")).toBeNull();
    expect([...blobs.values()]).toEqual(["module x\n"]);
    // The refused --batch-command, then --batch-check and --batch.
    expect(cost.processes).toBe(3);
  });
});
