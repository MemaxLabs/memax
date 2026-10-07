import type { V2 } from "memax-sdk";
import { describe, expect, it, vi } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { switchRows } from "../switch-copy";
import { createDemoSource } from "./demo-source";
import { DEMO_SWITCH_PREVIEW, DEMO_V1_SPACE } from "./demo-switch-data";
import { createSdkSwitch } from "./switch-sdk";

const space = DEMO_V1_SPACE;

function spaceSwitch(over: Partial<V2.SpaceSwitch> = {}): V2.SpaceSwitch {
  return {
    space: {
      id: space.id,
      tenant_id: "t1",
      slug: space.slug,
      name: space.name,
      kind: "team",
      role: "owner",
    },
    state: "v1",
    step: "space",
    attempts: 0,
    background: false,
    preview: {
      kind: "team",
      kinds: ["team", "project"],
      suggested_repository: "acme/web",
      members: [
        {
          person_id: "p1",
          name: "Ziyang Zeng",
          v1_role: "admin",
          role: "member",
          can_forget: true,
        },
      ],
      notes: {
        total: 3,
        person: 2,
        agent: 1,
        candidates: 2,
        fold: 1,
        kept: 0,
        long: 0,
        secret: 0,
        archived: 0,
        format: 0,
        external: 0,
        seeds: 1,
      },
      personas: 0,
      configs: [
        {
          path: "CLAUDE.md",
          agent: "claude-code",
          scope: "project:https://github.com/acme/web",
          targets: ["claude_md"],
        },
      ],
      targets: ["agents_md", "claude_md"],
      agents: [
        {
          credential: "oauth_grant",
          name: "Gemini CLI",
          agent: "gemini-cli",
          person_id: "p1",
          autonomy: "read",
          connected: false,
        },
      ],
      gates: 0,
      dream_runs: 0,
      empty: false,
    },
    progress: {
      notes: 3,
      personas: 0,
      configs: 1,
      targets: ["agents_md"],
      proposed: 2,
      folded: 0,
      existing: 0,
      refused: 0,
      imports: ["i1"],
      connected: 1,
      already_connected: 0,
      notified: 1,
      gates_moved: 0,
      gates_left: 0,
    },
    ...over,
  } as V2.SpaceSwitch;
}

describe("the SDK's switch", () => {
  it("reads and switches through memax.v2.spaces, by slug", async () => {
    const switchStatus = vi.fn(async () => spaceSwitch());
    const switchToV2 = vi.fn(async () =>
      spaceSwitch({ state: "running", step: "candidates", import_id: "i1" }),
    );
    const switchToV1 = vi.fn(async () =>
      spaceSwitch({ state: "off", step: "done" }),
    );
    const client = {
      v2: { spaces: { switchStatus, switchToV2, switchToV1 } },
    } as unknown as Parameters<typeof createSdkSwitch>[0];
    const sw = createSdkSwitch(client);

    const v = await sw.status({ space });
    expect(switchStatus).toHaveBeenCalledWith("acme-web", {
      signal: undefined,
    });
    expect(v.state).toBe("v1");
    expect(v.preview.members).toEqual([
      { name: "Ziyang Zeng", v1Role: "admin", role: "member", canForget: true },
    ]);
    // Agents by registry key; the files counted; notes, personas and files together.
    expect(v.preview.agents[0]).toMatchObject({
      agent: "gemini",
      autonomy: "read",
    });
    expect(v.preview.configs).toBe(1);
    expect(v.progress.notes).toBe(4);
    expect(v.importId).toBeNull();

    const started = await sw.toV2({
      space,
      kind: "project",
      idempotencyKey: "k1",
    });
    expect(switchToV2).toHaveBeenCalledWith("acme-web", {
      idempotencyKey: "k1",
      kind: "project",
    });
    expect(started).toMatchObject({ state: "running", importId: "i1" });

    expect((await sw.toV1({ space, idempotencyKey: "k2" })).state).toBe("off");
    expect(switchToV1).toHaveBeenCalledWith("acme-web", {
      idempotencyKey: "k2",
    });
  });
});

describe("the demo's switch", () => {
  it("switches acme-web in the background, then lands its V1 import in Review", async () => {
    const demo = createDemoSource({ streamDelayMs: 0 });
    const v1 = (await demo.spaces()).find((s) => s.slug === "acme-web")!;
    expect(v1.onV2).toBe(false);
    expect(await demo.imports.list({ space: v1 })).toEqual([]);

    expect((await demo.switch.status({ space: v1 })).state).toBe("v1");
    const started = await demo.switch.toV2({ space: v1, idempotencyKey: "k" });
    expect(started.state).toBe("running");
    const landed = await demo.switch.status({ space: v1 });
    expect(landed).toMatchObject({ state: "switched", step: "done" });
    expect(landed.progress.proposed).toBe(7);

    expect((await demo.spaces()).find((s) => s.slug === "acme-web")?.onV2).toBe(
      true,
    );
    const [imp] = await demo.imports.list({ space: v1 });
    expect(imp).toMatchObject({ id: landed.importId, origin: "v1" });
    const view = await demo.imports.get({ space: v1, id: imp!.id });
    expect(view?.memories.map((m) => m.refs[0])).toContain("N-0001");

    // Back to V1: the spaces list says so again.
    expect(
      (await demo.switch.toV1({ space: v1, idempotencyKey: "k2" })).state,
    ).toBe("off");
    expect((await demo.spaces()).find((s) => s.slug === "acme-web")?.onV2).toBe(
      false,
    );
  });

  it("knows no other space on V1", async () => {
    const demo = createDemoSource({ streamDelayMs: 0 });
    const v2 = (await demo.spaces()).find((s) => s.slug === "memax-v2")!;
    await expect(demo.switch.status({ space: v2 })).rejects.toThrow();
  });
});

describe("what moves, in words", () => {
  it("says each thing that moves, and leaves out what isn't there", () => {
    const rows = switchRows(en.ledger.today.switch, DEMO_SWITCH_PREVIEW, "en");
    expect(Object.fromEntries(rows.map((r) => [r.label, r.text]))).toEqual({
      Notes:
        "112 V1 memories become notes (N-): searchable, never compiled. Nothing is lost.",
      Review:
        "7 you wrote, one statement each, wait in Review to keep in one go.",
      Dream:
        "101 that agents wrote, and longer notes, go to Dream, which folds them into proposals.",
      "Notes only":
        "4 stay notes and are never proposed: 3 archived and 1 holds a credential.",
      People:
        "3 people keep their access. V1 admins become members who can forget, and viewers can propose.",
      Agents:
        "Claude Code at Propose and Cursor at Read connect, and each is told on its next response.",
      Files:
        "AGENTS.md and CLAUDE.md compile from the record. Two-way sync of its agent files stops.",
      Decisions: "1 waiting on V1's board moves once its agent is connected.",
      History: "4 V1 Dream runs stay, read-only.",
      Plan: "Your pro plan carries over.",
    });
    const alone = switchRows(
      en.ledger.today.switch,
      {
        ...DEMO_SWITCH_PREVIEW,
        notes: {
          total: 0,
          candidates: 0,
          fold: 0,
          kept: 0,
          archived: 0,
          secret: 0,
        },
        members: DEMO_SWITCH_PREVIEW.members.slice(0, 1),
        agents: [],
        targets: [],
        gates: 0,
        dreamRuns: 0,
        plan: null,
      },
      "en",
    );
    expect(alone.map((r) => r.text)).toEqual([
      "There are no V1 memories to move.",
      "Only you.",
    ]);
  });

  it("says it in Chinese too", () => {
    const rows = switchRows(zh.ledger.today.switch, DEMO_SWITCH_PREVIEW, "zh");
    expect(rows[0]!.text).toBe(
      "112 条 V1 记忆变成笔记（N-）：可以搜索，不会编译进文件，一条不丢。",
    );
    expect(rows.find((r) => r.key === "agents")?.text).toBe(
      "Claude Code（提议）和 Cursor（只读）会接入，并在下一次响应里得知这次切换。",
    );
  });
});
