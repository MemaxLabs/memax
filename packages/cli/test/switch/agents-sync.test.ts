// memax agents sync for a person with a space on V2: the server answers
// that space's agent files "unchanged" (reason space_on_v2); the command
// pushes and pulls nothing for them, acknowledges nothing, says so in its
// own words, and keeps the generic warning quiet.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const sync = vi.fn();
const ack = vi.fn();
const push = vi.fn();
const quiet: string[] = [];

vi.mock("../../src/lib/client.js", () => ({
  getClient: () => ({ configs: { sync, ack, push } }),
  quietWarning: (w: string) => quiet.push(w),
}));
vi.mock("../../src/lib/config.js", () => ({
  getOrCreateDeviceID: () => "device-1",
}));
vi.mock("../../src/lib/project-context.js", async (orig) => ({
  ...(await orig<object>()),
  resolveProjectScope: () => ({ scope: "project:https://github.com/acme/web" }),
}));
vi.mock("../../src/commands/agent-configs-discovery.js", async (orig) => ({
  ...(await orig<object>()),
  discoverAgentConfigs: () => [],
}));

const { syncAgentMemoryCommand } =
  await import("../../src/commands/agent-configs.js");

let out: string[];
beforeEach(() => {
  out = [];
  quiet.length = 0;
  sync.mockReset();
  ack.mockReset();
  push.mockReset();
  vi.spyOn(console, "log").mockImplementation((...a: unknown[]) => {
    out.push(a.map(String).join(" "));
  });
});
afterEach(() => vi.restoreAllMocks());

// eslint-disable-next-line no-control-regex -- strips chalk's colours
const plain = () => out.join("\n").replace(/\u001b\[[0-9;]*m/g, "");

describe("memax agents sync, with a space on V2", () => {
  it("leaves its files to Memax and says so once", async () => {
    sync.mockResolvedValue({
      actions: [
        {
          action: "unchanged",
          agent: "claude-code",
          file_path: "CLAUDE.md",
          scope: "project:https://github.com/acme/web",
          reason: "space_on_v2",
        },
        {
          action: "unchanged",
          agent: "cursor",
          file_path: ".cursor/rules/web.mdc",
          scope: "project:https://github.com/acme/web",
          reason: "space_on_v2",
        },
      ],
    });
    await syncAgentMemoryCommand({});
    const text = plain();
    expect(quiet).toEqual(["space_on_v2"]);
    expect(text).toContain("= CLAUDE.md compiled by Memax (space on V2)");
    expect(text).toContain("Done: 2 compiled by Memax");
    expect(text).toContain(
      "Two-way sync is off for 2 agent files of spaces on V2",
    );
    expect(text).not.toContain("No configs in cloud yet");
    expect(push).not.toHaveBeenCalled();
    expect(ack).not.toHaveBeenCalled();
  });

  it("says nothing about V2 when no space is on it", async () => {
    sync.mockResolvedValue({
      actions: [
        {
          action: "unchanged",
          agent: "claude-code",
          file_path: "CLAUDE.md",
          scope: "global",
        },
      ],
    });
    await syncAgentMemoryCommand({});
    expect(plain()).not.toContain("Two-way sync is off");
  });
});
