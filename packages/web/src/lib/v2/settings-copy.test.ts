import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import type { AgentConnectionView } from "./data/agents";
import { createDemoDream } from "./data/dream-demo";
import { createDemoSettings, DEMO_SECURITY } from "./data/settings-demo";
import {
  agentSummary,
  deliveryNote,
  eventText,
  placeText,
  processorRows,
  quietZoneText,
  unreachableLines,
} from "./settings-copy";

const settings = () =>
  createDemoSettings({ dream: createDemoDream() }).peekNotifications!()!;
const n = (locale: "en" | "zh") =>
  (locale === "en" ? en : zh).ledger.settings.notifications;
const sec = (locale: "en" | "zh") =>
  (locale === "en" ? en : zh).ledger.settings.security;

describe("Settings › Notifications in words", () => {
  it("says when the morning edition comes: the end of quiet hours over Dream's night", () => {
    const s = settings();
    const morning = s.events.find((e) => e.event === "morning_edition")!;
    expect(eventText(n("en"), morning, s)).toEqual({
      title: "The morning edition",
      meta: "What Dream changed overnight, at 08:00",
    });
    expect(eventText(n("zh"), morning, s).meta).toBe(
      "Dream 夜里改了什么，08:00 送到",
    );
    const off = { ...s, quietHours: { ...s.quietHours, on: false } };
    expect(eventText(n("en"), morning, off).meta).toBe(
      "What Dream changed overnight, as soon as it's done",
    );
    const lunch = {
      ...s,
      quietHours: { ...s.quietHours, from: "12:00", until: "13:00" },
    };
    expect(eventText(n("en"), morning, lunch).meta).toBe(
      "What Dream changed overnight, as soon as it's done",
    );
  });

  it("names the Review reminder's wait", () => {
    const s = settings();
    const review = s.events.find((e) => e.event === "review_waiting")!;
    expect(eventText(n("en"), review, s).title).toBe("Proposals waiting a day");
    expect(eventText(n("en"), review, { ...s, reviewAfterDays: 3 }).title).toBe(
      "Proposals waiting 3 days",
    );
    expect(eventText(n("zh"), review, { ...s, reviewAfterDays: 3 }).title).toBe(
      "提议等了 3 天",
    );
  });

  it("says which email goes out today, and that phone and Slack wait", () => {
    const s = settings();
    expect(deliveryNote(n("en"), s, "en")).toBe(
      "Memax emails the morning edition today. It keeps your other choices and follows them as each email arrives. Phone and Slack aren't available yet.",
    );
    const none = {
      ...s,
      events: s.events.map((e) => ({ ...e, emailSent: false })),
    };
    expect(deliveryNote(n("en"), none, "en")).toBe(
      "This server sends no email yet. Memax keeps your choices for when it does. Phone and Slack aren't available yet.",
    );
    expect(deliveryNote(n("zh"), s, "zh")).toBe(
      "Memax 现在会发邮件的只有晨报。其他选择会先记着，等相应的邮件上线后照办。手机和 Slack 暂时还不能用。",
    );
  });

  it("says which zone the quiet hours are in", () => {
    const s = settings();
    expect(quietZoneText(n("en"), s)).toBe("In America/Vancouver");
    expect(quietZoneText(n("en"), { ...s, timeZoneSource: "default" })).toBe(
      "In UTC, until Memax learns your time zone",
    );
  });
});

describe("Settings › Security in words", () => {
  it("names where data lives, or the raw provider a server names", () => {
    expect(placeText(sec("en"), DEMO_SECURITY.residency[0]!)).toBe(
      "Neon Postgres, us-west-2",
    );
    expect(placeText(sec("en"), DEMO_SECURITY.residency[2]!)).toBe(
      "Cloudflare R2",
    );
    expect(
      placeText(sec("zh"), {
        holds: "database",
        provider: "hetzner",
        region: "fsn1",
      }),
    ).toBe("hetzner，fsn1");
  });

  it("lists each processor's uses with its pinned hosts, and says Voyage's retention isn't confirmed", () => {
    const rows = processorRows(sec("en"), DEMO_SECURITY.processors, "en");
    expect(rows.map((r) => r.name)).toEqual([
      "OpenRouter",
      "Voyage AI",
      "Resend",
    ]);
    // Uses on the same model and hosts read as one line.
    expect(rows[0]!.gets).toEqual([
      "The judge's first pass, Ask's answers and Dream: DeepSeek V4.1 Flash, on Together AI, Baseten, CoreWeave and DeepInfra, fp8 or better",
      "The judge's fallback and Dream's fallback: Claude Haiku 4.5, on Amazon Bedrock and Google Vertex AI, fp8 or better",
      "The judge's check on decisions in force and Dream's check on decisions in force: Claude Sonnet 5.5, on Google Vertex AI, fp8 or better",
    ]);
    expect(rows[1]!.gets).toEqual([
      "Embeddings of what you keep and propose: voyage-4",
      "Embeddings of searches: voyage-4-lite",
      "Reranking search results: rerank-3-lite",
    ]);
    expect(rows[0]!.keeps).toBe(
      "Nothing. Every call goes to zero-retention endpoints only.",
    );
    expect(rows[1]!.unconfirmed).toBe(true);
    expect(rows[1]!.keeps).toBe(
      "Not confirmed. Voyage AI keeps API inputs for training unless the account opts out, and Memax hasn't confirmed its opt-out yet. Treat what it was sent as held under its terms.",
    );
    expect(rows[2]!.gets).toEqual(["The morning email, to you"]);
    const zh = processorRows(sec("zh"), DEMO_SECURITY.processors, "zh");
    expect(zh[0]!.gets[0]).toBe(
      "核对的第一轮、提问的回答和 Dream：DeepSeek V4.1 Flash，运行在 Together AI、Baseten、CoreWeave和 DeepInfra，fp8 或更高精度",
    );
    // Once the opt-out is confirmed, the server says zero.
    const confirmed = processorRows(
      sec("en"),
      [{ ...DEMO_SECURITY.processors[1]!, retention: "zero" }],
      "en",
    );
    expect(confirmed[0]!.unconfirmed).toBe(false);
  });

  it("says what Forget can't reach, with the backup window and the providers that keep", () => {
    expect(unreachableLines(sec("en"), DEMO_SECURITY, "en")).toEqual([
      "Database backups, for 7 days. Memax applies every Forget again after any restore.",
      "Earlier commits of a compiled file, in your repository's history",
      "What agents wrote to their own memory",
      "Files with a hand edit Memax won't write over",
      "Copies pasted elsewhere, such as a ChatGPT project",
      "What Voyage AI and Resend kept of what they were sent, under their terms (below)",
    ]);
    const quiet = unreachableLines(
      sec("en"),
      { ...DEMO_SECURITY, backupDays: 1, processors: [] },
      "en",
    );
    expect(quiet[0]).toBe(
      "Database backups, for 1 day. Memax applies every Forget again after any restore.",
    );
    expect(quiet).toHaveLength(5);
  });

  it("sums up the agents by the most each may do", () => {
    const agent = (
      id: string,
      levels: ("read" | "propose" | "write")[],
      state: AgentConnectionView["state"] = "active",
    ) =>
      ({
        id,
        state,
        spaces: levels.map((autonomy, i) => ({
          spaceId: `s${i}`,
          autonomy,
        })),
      }) as AgentConnectionView;
    const agents = [
      agent("a", ["read", "write"]),
      agent("b", ["propose"]),
      agent("c", ["propose", "read"], "paused"),
      agent("d", ["write"], "disconnected"),
    ];
    expect(agentSummary(sec("en"), agents, "en")).toBe(
      "3 agents connected across 2 spaces. At most: 2 propose and 1 write. 1 paused.",
    );
    expect(agentSummary(sec("zh"), agents, "zh")).toBe(
      "3 个 Agent 接入了 2 个空间。最多能做：2 个提议和 1 个写入。1 个已暂停。",
    );
    expect(agentSummary(sec("en"), [], "en")).toBe(
      "No agents are connected to your spaces.",
    );
  });
});
