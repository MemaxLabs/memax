// What memax check and memax verify say about conditions and their
// failures: the same sentences as the web's VerifyStale (verify-en.ts).
import type { V2 } from "memax-sdk";
import { describe, expect, it } from "vitest";
import { conditionWords, evidenceWords } from "../../src/lib/stale/words.js";

const c = (x: Record<string, unknown>) => x as unknown as V2.Condition;

describe("a condition in words", () => {
  it("says what each kind checks", () => {
    expect(
      conditionWords(c({ kind: "path_exists", path: "docs/adr.md" })),
    ).toBe("docs/adr.md exists");
    expect(
      conditionWords(c({ kind: "path_exists", glob: "src/**/*.go" })),
    ).toBe("something matches src/**/*.go");
    expect(
      conditionWords(
        c({ kind: "file_contains", path: "go.mod", text: "river", word: true }),
      ),
    ).toBe('go.mod contains "river" as a word');
    expect(
      conditionWords(
        c({
          kind: "dep_present",
          manifest: "package.json",
          name: "react",
          range: "^18",
        }),
      ),
    ).toBe("package.json lists react ^18");
    expect(
      conditionWords(c({ kind: "file_unchanged", path: "a.ts", blob: "x" })),
    ).toBe("a.ts is unchanged");
    expect(
      conditionWords(
        c({ kind: "pr_state", repo: "acme/web", number: 12, state: "merged" }),
      ),
    ).toBe("acme/web#12 is merged");
  });
});

describe("why a condition stopped holding", () => {
  const path = c({ kind: "path_exists", path: "a/config.go" });
  it("says what the web's VerifyStale says", () => {
    expect(evidenceWords(path, { change: "removed" }, "main")).toBe(
      "a/config.go is gone from main.",
    );
    expect(
      evidenceWords(
        path,
        { change: "renamed", path: "a/config.go", renamed_to: "a/tiers.go" },
        "main",
      ),
    ).toBe("a/config.go was renamed to a/tiers.go on main.");
    expect(
      evidenceWords(
        c({ kind: "path_exists", glob: "src/**/*.go" }),
        { change: "no_match" },
        "main",
      ),
    ).toBe("Nothing on main matches src/**/*.go any more.");
    expect(
      evidenceWords(
        c({ kind: "file_contains", path: "go.mod", text: "river" }),
        { change: "text_missing", path: "go.mod" },
        "main",
      ),
    ).toBe('go.mod no longer contains "river" on main.');
    const dep = c({
      kind: "dep_present",
      manifest: "go.mod",
      name: "github.com/x/y",
    });
    expect(
      evidenceWords(
        dep,
        { change: "dependency_removed", path: "svc/go.mod" },
        "main",
      ),
    ).toBe("svc/go.mod no longer lists github.com/x/y on main.");
    expect(
      evidenceWords(
        dep,
        { change: "constraint_changed", path: "go.mod", constraint: "v2.0.0" },
        "main",
      ),
    ).toBe("go.mod now asks for github.com/x/y v2.0.0 on main.");
    expect(
      evidenceWords(
        c({ kind: "file_unchanged", path: "a.ts", blob: "x" }),
        { change: "blob_changed", path: "a.ts" },
        "main",
      ),
    ).toBe("a.ts changed on main.");
  });
});
