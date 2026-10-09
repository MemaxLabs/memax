import { describe, expect, it } from "vitest";
import {
  compile,
  extractManagedBlock,
  findManagedBlock,
  isDrifted,
  MANAGED_END,
  MANAGED_START,
  ManagedBlockError,
  removeManagedBlock,
  upsertManagedBlock,
  type CompileInput,
} from "../src/index.js";
import { demo } from "./helpers.js";

const BLOCK = ["<!-- header -->", "@AGENTS.md"];

describe("upsertManagedBlock", () => {
  it("creates the block in an empty file", () => {
    expect(upsertManagedBlock("", BLOCK)).toBe(
      "<!-- memax:start -->\n<!-- header -->\n@AGENTS.md\n<!-- memax:end -->\n",
    );
  });

  it("appends after a blank line and leaves the person's text alone", () => {
    const mine = "# Mine\n\nTabs.  \n";
    const out = upsertManagedBlock(mine, BLOCK);
    expect(out.startsWith(mine)).toBe(true);
    expect(out.slice(mine.length)).toBe(
      "\n<!-- memax:start -->\n<!-- header -->\n@AGENTS.md\n<!-- memax:end -->\n",
    );
  });

  it("appends after a blank line in a CRLF file too", () => {
    // `\r\n` is one line break: the file doesn't end with a blank line yet.
    const mine = "# Mine\r\n\r\nTabs.\r\n";
    const out = upsertManagedBlock(mine, BLOCK);
    expect(out).toBe(
      `${mine}\r\n<!-- memax:start -->\r\n<!-- header -->\r\n@AGENTS.md\r\n<!-- memax:end -->\r\n`,
    );
    expect(upsertManagedBlock(out, BLOCK)).toBe(out);
    // A file that already ends with a blank line gets no second one.
    expect(upsertManagedBlock("top\r\n\r\n", BLOCK)).toBe(
      "top\r\n\r\n<!-- memax:start -->\r\n<!-- header -->\r\n@AGENTS.md\r\n<!-- memax:end -->\r\n",
    );
    expect(upsertManagedBlock("old mac\r\r", BLOCK)).toBe(
      "old mac\r\r<!-- memax:start -->\r<!-- header -->\r@AGENTS.md\r<!-- memax:end -->\r",
    );
  });

  it("adds a line break when the file doesn't end with one", () => {
    expect(upsertManagedBlock("No newline", BLOCK)).toBe(
      "No newline\n\n<!-- memax:start -->\n<!-- header -->\n@AGENTS.md\n<!-- memax:end -->\n",
    );
  });

  it("replaces the block in place, touching nothing outside the markers", () => {
    const before = "top\r\n\r\n";
    const after = "\r\nbottom  \r\nlast line, no newline";
    const file = `${before}<!-- memax:start -->\r\nold\r\n<!-- memax:end -->${after}`;
    const out = upsertManagedBlock(file, BLOCK);
    expect(out).toBe(
      `${before}<!-- memax:start -->\r\n<!-- header -->\r\n@AGENTS.md\r\n<!-- memax:end -->${after}`,
    );
  });

  it("is idempotent", () => {
    for (const start of [
      "",
      "# Mine\n",
      "a\r\nb",
      "<!-- memax:start -->\nx\n<!-- memax:end -->\n",
    ]) {
      const once = upsertManagedBlock(start, BLOCK);
      expect(upsertManagedBlock(once, BLOCK)).toBe(once);
    }
  });

  it("replaces a V1 instruction block, which used the same markers", () => {
    const v1 =
      "# Repo\n\n<!-- memax:start -->\n## Memax — Persistent Memory\nUse memax_recall.\n<!-- memax:end -->\n";
    expect(upsertManagedBlock(v1, BLOCK)).toBe(
      "# Repo\n\n<!-- memax:start -->\n<!-- header -->\n@AGENTS.md\n<!-- memax:end -->\n",
    );
  });

  it.each([
    ["a start with no end", "<!-- memax:start -->\nx\n"],
    ["an end with no start", "x\n<!-- memax:end -->\n"],
    ["an end before the start", "<!-- memax:end -->\n<!-- memax:start -->\n"],
    [
      "two blocks",
      "<!-- memax:start -->\n<!-- memax:end -->\n<!-- memax:start -->\n<!-- memax:end -->\n",
    ],
  ])("refuses %s", (_name, content) => {
    expect(() => upsertManagedBlock(content, BLOCK)).toThrow(ManagedBlockError);
    expect(() => findManagedBlock(content)).toThrow(
      /exactly one <!-- memax:start --> line/,
    );
  });
});

describe("removeManagedBlock and extractManagedBlock", () => {
  it("removes only the block", () => {
    const file = "a\r\n<!-- memax:start -->\r\nx\r\n<!-- memax:end -->\r\nb";
    expect(removeManagedBlock(file)).toBe("a\r\nb");
    expect(removeManagedBlock("no block\n")).toBe("no block\n");
  });

  it.each([
    ["LF", "\n"],
    ["CRLF", "\r\n"],
  ])("takes out the blank line adding the block put there (%s)", (_, eol) => {
    const lines = (...l: string[]) => l.map((x) => x + eol).join("");
    const mine = lines("# Mine", "", "Tabs.");
    // Added, then removed: the file is as it was.
    expect(removeManagedBlock(upsertManagedBlock(mine, BLOCK))).toBe(mine);
    // Text the person wrote below the block stays, byte for byte.
    const below = lines("", "## Below");
    expect(removeManagedBlock(upsertManagedBlock(mine, BLOCK) + below)).toBe(
      mine + below,
    );
    // A file without a final line break gets one: the only change.
    expect(
      removeManagedBlock(upsertManagedBlock(`# Mine${eol}Tabs.`, BLOCK)),
    ).toBe(lines("# Mine", "Tabs."));
    // Adding put no blank line here, so none goes.
    for (const file of ["", lines(""), lines("# Mine", "", "")]) {
      expect(removeManagedBlock(upsertManagedBlock(file, BLOCK))).toBe(file);
    }
    // A line of spaces is the person's.
    const spaced = lines("a", "  ", ...[MANAGED_START, "x", MANAGED_END]);
    expect(removeManagedBlock(spaced)).toBe(lines("a", "  "));
  });

  it("extracts the block with LF endings", () => {
    expect(
      extractManagedBlock(
        "a\r\n<!-- memax:start -->  \r\nx\r\n<!-- memax:end -->\r\nb",
      ),
    ).toBe("<!-- memax:start -->\nx\n<!-- memax:end -->\n");
    expect(extractManagedBlock("nothing")).toBeNull();
  });
});

describe("a user-owned CLAUDE.md through compile", () => {
  const owned = (current: string): CompileInput => {
    const i = demo();
    i.targets = [{ kind: "claude_md", user_owned: true, current }];
    return i;
  };
  const claude = (current: string) => compile(owned(current)).files[0];

  it("is idempotent across compiles", () => {
    const first = claude("# My project\n\nUse tabs.\n");
    const second = claude(first.content);
    expect(second.content).toBe(first.content);
    expect(second.sha256).toBe(first.sha256);
  });

  it("hashes only the block for drift, so edits outside it aren't drift", () => {
    const file = claude("# My project\n");
    expect(file.user_owned).toBe(true);
    expect(file.drift_sha256).not.toBe(file.sha256);
    expect(isDrifted(file.drift_sha256, file.content)).toBe(false);
    expect(
      isDrifted(file.drift_sha256, `${file.content}\nMore of my own notes.\n`),
    ).toBe(false);
    expect(
      isDrifted(
        file.drift_sha256,
        file.content.replace("@AGENTS.md", "@docs/AGENTS.md"),
      ),
    ).toBe(true);
    expect(isDrifted(file.drift_sha256, "# My project\n")).toBe(true);
  });

  it("leaves every byte outside the markers as it was", () => {
    const outside = "# Mine\r\n\r\n  indented  \r\n\u{00e9}t\u{00e9}\r\n";
    const file = claude(outside);
    expect(file.content.startsWith(outside)).toBe(true);
    const tail = "\r\nafter the block\r\n";
    const again = claude(file.content + tail);
    expect(again.content.endsWith(tail)).toBe(true);
    expect(again.content.startsWith(outside)).toBe(true);
  });
});
