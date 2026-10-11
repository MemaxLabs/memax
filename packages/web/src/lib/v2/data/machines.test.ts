import { describe, expect, it, vi } from "vitest";
import type { V2 } from "memax-sdk";
import { CommandFailedError, toFailure } from "./command-error";
import { createDemoSource } from "./demo-source";
import { DEMO_SPACES } from "./demo-dataset";
import { DEMO_MACHINE_IDS } from "./demo-machines-data";
import { createSdkMachines, machineOf, machinesOrNull } from "./machines-sdk";
import type { V2Client } from "./sdk-records";

// A personal space's machines through memax.v2.machines and in the demo:
// what's asked for (If-Match on a rename, a key on every command), how
// the answers read, and what an unlink does to the machine's files.

const PERSONAL = DEMO_SPACES.find((s) => s.slug === "personal")!;

const answer: V2.Machine = {
  id: "m1",
  space_id: "s1",
  label: "machine-1",
  name: "work-mbp",
  platform: "darwin",
  device_id: "d1",
  session_id: "sess-1",
  linked: true,
  linked_at: "2026-10-11T09:00:00Z",
  last_seen_at: "2026-10-11T09:05:00Z",
  version: 3,
  created_receipt_id: "r1",
  last_receipt_id: "r2",
  created_at: "2026-10-11T09:00:00Z",
  updated_at: "2026-10-11T09:05:00Z",
};

const result = (machine: V2.Machine) => ({
  outcome: "applied",
  policy: { effect: "apply" },
  machine,
  targets: [],
  receipts: [],
});

function client(machines: Record<string, unknown>): V2Client {
  return { v2: { machines } } as unknown as V2Client;
}

describe("machines through memax.v2.machines", () => {
  it("reads a machine as the screens need it", () => {
    expect(machineOf(answer)).toEqual({
      id: "m1",
      label: "machine-1",
      name: "work-mbp",
      platform: "darwin",
      linked: true,
      linkedAt: "2026-10-11T09:00:00Z",
      unlinkedAt: null,
      lastSeenAt: "2026-10-11T09:05:00Z",
      sessionId: "sess-1",
      version: 3,
    });
    expect(
      machineOf({ ...answer, last_seen_at: undefined, session_id: undefined }),
    ).toMatchObject({ lastSeenAt: null, sessionId: null });
  });

  it("renames with If-Match on the version and the intent's key, and unlinks with a key", async () => {
    const rename = vi
      .fn()
      .mockResolvedValue(result({ ...answer, name: "studio", version: 4 }));
    const unlink = vi
      .fn()
      .mockResolvedValue(result({ ...answer, linked: false, version: 4 }));
    const source = createSdkMachines(client({ rename, unlink }));
    const machine = machineOf(answer);
    const renamed = await source.rename({
      space: PERSONAL,
      machine,
      name: "studio",
      idempotencyKey: "k1",
    });
    expect(rename).toHaveBeenCalledWith(
      "m1",
      { name: "studio" },
      { idempotencyKey: "k1", ifMatch: 3 },
    );
    expect(renamed).toMatchObject({ name: "studio", version: 4 });
    const off = await source.unlink({
      space: PERSONAL,
      machine,
      idempotencyKey: "k2",
    });
    expect(unlink).toHaveBeenCalledWith("m1", {}, { idempotencyKey: "k2" });
    expect(off.linked).toBe(false);
  });

  it("lists by the space's slug, and reads as none when the list is refused", async () => {
    const list = vi.fn().mockResolvedValue({ items: [answer] });
    expect(
      await createSdkMachines(client({ list })).list({ space: PERSONAL }),
    ).toHaveLength(1);
    expect(list).toHaveBeenCalledWith("personal", { signal: undefined });
    const refused = vi.fn().mockRejectedValue(new Error("machines_personal"));
    expect(await machinesOrNull(client({ list: refused }), "memax-v2")).toBe(
      null,
    );
  });
});

describe("the demo's machines", () => {
  const demo = () =>
    createDemoSource({ commandDelayMs: 0, streamDelayMs: 0, settleMs: 5 });

  it("has Personal on two machines, the laptop online and the desktop not", async () => {
    const source = demo();
    const list = await source.machines.list({ space: PERSONAL });
    expect(list.map((m) => m.name)).toEqual(["ziyang-mbp", "home-linux"]);
    const memax = DEMO_SPACES.find((s) => s.slug === "memax-v2")!;
    await expect(source.machines.list({ space: memax })).rejects.toThrow(
      CommandFailedError,
    );
  });

  it("renames by version: a stale one clashes, and the same key replays", async () => {
    const source = demo();
    const [laptop] = await source.machines.list({ space: PERSONAL });
    const renamed = await source.machines.rename({
      space: PERSONAL,
      machine: laptop!,
      name: "  studio-mbp ",
      idempotencyKey: "r1",
    });
    expect(renamed).toMatchObject({ name: "studio-mbp", version: 2 });
    const again = await source.machines.rename({
      space: PERSONAL,
      machine: laptop!,
      name: "studio-mbp",
      idempotencyKey: "r1",
    });
    expect(again).toEqual(renamed);
    const clash = await source.machines
      .rename({
        space: PERSONAL,
        machine: laptop!,
        name: "other",
        idempotencyKey: "r2",
      })
      .catch(toFailure);
    expect(clash).toEqual({ kind: "clash", currentVersion: 2 });
  });

  it("unlinks: every file of the machine stops, the status line counts what's left", async () => {
    const source = demo();
    const machines = await source.machines.list({ space: PERSONAL });
    const desktop = machines.find((m) => m.id === DEMO_MACHINE_IDS.desktop)!;
    const before = await source.overview(PERSONAL);
    expect(before.status).toEqual({
      kind: "machine-drifted",
      machine: "ziyang-mbp",
    });
    const off = await source.machines.unlink({
      space: PERSONAL,
      machine: desktop,
      idempotencyKey: "u1",
    });
    expect(off).toMatchObject({ linked: false, version: 3 });
    const targets = await source.targets.list({ space: PERSONAL });
    expect(
      targets
        .filter((t) => t.machine?.id === desktop.id)
        .map((t) => t.syncState),
    ).toEqual(["off", "off", "off"]);
    // A second unlink changes nothing.
    const twice = await source.machines.unlink({
      space: PERSONAL,
      machine: off,
      idempotencyKey: "u2",
    });
    expect(twice).toEqual(off);
  });

  it("leaves an offline machine's recompiled file waiting for it", async () => {
    const source = demo();
    const targets = await source.targets.list({ space: PERSONAL });
    const desktopClaude = targets.find(
      (t) =>
        t.machine?.id === DEMO_MACHINE_IDS.desktop && t.kind === "claude_user",
    )!;
    await source.targets.compile({
      space: PERSONAL,
      target: desktopClaude,
      idempotencyKey: "c1",
    });
    await new Promise((r) => setTimeout(r, 20));
    const after = await source.targets.list({ space: PERSONAL });
    expect(after.find((t) => t.id === desktopClaude.id)).toMatchObject({
      syncState: "pending_delivery",
      lastCompile: { status: "compiled" },
    });
  });
});
