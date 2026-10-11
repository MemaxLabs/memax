// memax forget's report for a Personal memory in home files: each file
// named with its machine, an offline machine said with when it was last
// online, a hand edit on a machine held there.
import { describe, expect, it } from "vitest";
import type { V2 } from "memax-sdk";
import { reportLines } from "./forget.js";

// eslint-disable-next-line no-control-regex
const plain = (lines: string[]) =>
  lines.join("\n").replace(/\x1b\[[0-9;]*m/g, "");

const machine = (name: string, last_seen_at?: string) => ({
  id: "m",
  label: "machine-1",
  name,
  linked: true,
  ...(last_seen_at ? { last_seen_at } : {}),
});

const step = (
  status: string,
  reason: string | undefined,
  label: string,
  m: ReturnType<typeof machine>,
) => ({
  kind: "target",
  status,
  ...(reason ? { reason } : {}),
  target: {
    id: "t",
    kind: "claude_user",
    label,
    delivery: "local",
    machine: m,
  },
});

describe("memax forget's report, per machine", () => {
  it("names each home file's machine, and an offline one with its last day online", () => {
    const t = {
      ref: "M-0101",
      steps: [
        step(
          "done",
          undefined,
          "~/.claude/rules/memax-personal.md",
          machine("work-mbp"),
        ),
        step(
          "waiting",
          "offline",
          "~/.claude/rules/memax-personal.md",
          machine("home-linux", "2026-10-09T08:00:00Z"),
        ),
        step("held", "hand_edit", "~/.codex/AGENTS.md", machine("home-linux")),
      ],
    } as unknown as V2.Tombstone;
    const text = plain(reportLines(t, { slug: "personal" } as V2.Space));
    expect(text).toContain("1 file rewritten");
    expect(text).toContain(
      "~/.claude/rules/memax-personal.md on home-linux holds it until home-linux connects (last online 2026-10-09); then it is rewritten.",
    );
    expect(text).toContain(
      "~/.codex/AGENTS.md on home-linux has a hand edit, which Memax never writes over.",
    );
  });
});
