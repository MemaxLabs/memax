import { describe, expect, it } from "vitest";
import { ledgerMachinesEn } from "@/i18n/locales/ledger/machines-en";
import { ledgerMachinesZh } from "@/i18n/locales/ledger/machines-zh";
import { ledgerBriefEn } from "@/i18n/locales/ledger/brief-en";
import type { TargetView } from "./data/targets";
import {
  homeDriftNotice,
  homeStatusText,
  machineMeta,
  machineStateText,
  platformName,
  seenText,
} from "./machines-copy";

// The words for Personal's machines and home files, in both locales.

const en = ledgerMachinesEn;
const zh = ledgerMachinesZh;
const NOW = new Date("2026-10-05T14:40:00-07:00");
const when = { now: NOW, timeZone: "America/Vancouver", locale: "en" as const };
const zhWhen = { ...when, locale: "zh" as const };
const ago = (minutes: number) =>
  new Date(NOW.getTime() - minutes * 60_000).toISOString();
const names: Record<string, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
};
const agentName = (key: string) => names[key] ?? key;

describe("when a machine was last online", () => {
  it("says online now, then minutes, hours, yesterday, days, and a date after a week", () => {
    expect(seenText(en, ago(2), when, true)).toBe("online now");
    expect(seenText(en, ago(40), when, false)).toBe("last online 40 min ago");
    expect(seenText(en, ago(180), when, false)).toBe("last online 3 h ago");
    expect(seenText(en, "2026-10-04T20:00:00-07:00", when, false)).toBe(
      "last online yesterday",
    );
    expect(seenText(en, "2026-10-02T21:10:00-07:00", when, false)).toBe(
      "last online 3 days ago",
    );
    expect(seenText(en, "2026-09-20T10:00:00-07:00", when, false)).toBe(
      "last online Sep 20",
    );
    expect(seenText(en, null, when, false)).toBe("not online yet");
    expect(seenText(zh, "2026-10-02T21:10:00-07:00", zhWhen, false)).toBe(
      "3 天前在线",
    );
    expect(seenText(zh, ago(2), zhWhen, true)).toBe("现在在线");
  });
});

describe("a home file's status across machines", () => {
  it("names the one machine, or counts them", () => {
    expect(
      homeStatusText(en, { kind: "in_sync", machines: 2, machine: null }, when),
    ).toEqual({ label: "In sync on 2 machines", detail: null, state: "kept" });
    expect(
      homeStatusText(
        en,
        { kind: "in_sync", machines: 1, machine: "work-mbp" },
        when,
      ).label,
    ).toBe("In sync on work-mbp");
    expect(
      homeStatusText(
        zh,
        { kind: "in_sync", machines: 2, machine: null },
        zhWhen,
      ).label,
    ).toBe("2 台电脑上都已同步");
  });

  it("says a hand edit and a hold in ochre, by machine", () => {
    expect(
      homeStatusText(en, { kind: "drifted", machines: ["work-mbp"] }, when),
    ).toEqual({
      label: "Hand edit on work-mbp",
      detail: null,
      state: "proposed",
    });
    expect(
      homeStatusText(en, { kind: "drifted", machines: ["a", "b"] }, when).label,
    ).toBe("Hand edits on 2 machines");
    expect(
      homeStatusText(
        en,
        { kind: "held", machines: ["work-mbp"], proposals: 2 },
        when,
      ).label,
    ).toBe("Held for Review on work-mbp");
    expect(
      homeStatusText(zh, { kind: "drifted", machines: ["work-mbp"] }, zhWhen)
        .label,
    ).toBe("work-mbp 上有手动改动");
  });

  it("says which machine is behind and why, on its own line", () => {
    const behind = {
      kind: "behind" as const,
      reached: 1,
      of: 2,
      offline: { machine: "home-linux", since: "2026-10-02T21:10:00-07:00" },
      waiting: null,
    };
    expect(homeStatusText(en, behind, when)).toEqual({
      label: "On 1 of 2 machines",
      detail: "home-linux last online 3 days ago",
      state: "working",
    });
    expect(homeStatusText(zh, behind, zhWhen)).toEqual({
      label: "2 台电脑里 1 台已同步",
      detail: "home-linux 3 天前在线",
      state: "working",
    });
    expect(
      homeStatusText(
        en,
        { ...behind, offline: null, waiting: "home-linux" },
        when,
      ).detail,
    ).toBe("Waiting for home-linux");
    // On its one machine, the reason is the status.
    expect(
      homeStatusText(
        en,
        { ...behind, reached: 0, of: 1, offline: null, waiting: "work-mbp" },
        when,
      ),
    ).toEqual({
      label: "Waiting for work-mbp",
      detail: null,
      state: "working",
    });
    expect(homeStatusText(en, { kind: "off" }, when).state).toBe("off");
  });

  it("words one machine's copy", () => {
    expect(machineStateText(en, { kind: "offline", since: null })).toEqual({
      label: "Offline",
      state: "working",
    });
    expect(machineStateText(en, { kind: "unlinked", since: null }).label).toBe(
      "Unlinked",
    );
    expect(machineStateText(zh, { kind: "stopped" }).label).toBe("已停止");
  });
});

describe("a machine", () => {
  it("says what it is and who reads its files", () => {
    expect(platformName(en, "darwin")).toBe("macOS");
    expect(platformName(en, "freebsd")).toBe("freebsd");
    expect(
      machineMeta(
        en,
        { platform: "linux" },
        ["claude-code", "codex"],
        agentName,
        "en",
      ),
    ).toBe("Linux · Claude Code and Codex");
    expect(machineMeta(en, { platform: "darwin" }, [], agentName, "en")).toBe(
      "macOS · No agent's file",
    );
    expect(
      machineMeta(
        zh,
        { platform: "linux" },
        ["claude-code", "codex"],
        agentName,
        "zh",
      ),
    ).toMatch(/^Linux · Claude Code和 ?Codex$/);
  });

  it("words the Brief's drift notice for one machine's copy", () => {
    const target = {
      kind: "codex_user",
      label: "~/.codex/AGENTS.md",
      openDrift: 1,
    } as TargetView;
    const notice = homeDriftNotice(
      en,
      ledgerBriefEn,
      target,
      "work-mbp",
      { observedAt: "2026-10-05T11:20:00-07:00" } as never,
      agentName,
      when,
    );
    expect(notice).toEqual({
      title: "Codex's copy on work-mbp has one local edit.",
      body: "Someone changed ~/.codex/AGENTS.md by hand on work-mbp today at 11:20. Memax never overwrites a hand edit without asking.",
    });
  });
});
