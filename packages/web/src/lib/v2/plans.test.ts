import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import {
  EVERY_PLAN,
  monthlyOfYearly,
  monthsFree,
  PLAN_CURRENCY,
  PLANS,
  yearlyDiscount,
  type PlanId,
} from "./plans";

// The plans are D9 (accepted Oct 6, 2026) as internal-docs 01 spells it
// out. A change here is a pricing change: it goes through the business
// docs first (.agents/skills/business, "Pricing Change Protocol").

const plan = (id: PlanId) => PLANS.find((p) => p.id === id)!;
const value = (id: PlanId, feature: string) =>
  plan(id).features.find((f) => f.id === feature)?.values;

describe("the plans", () => {
  it("are Free, Pro, Team and Enterprise, in that order", () => {
    expect(PLANS.map((p) => p.id)).toEqual([
      "free",
      "pro",
      "team",
      "enterprise",
    ]);
    expect(PLAN_CURRENCY).toBe("USD");
  });

  it("cost what D9 accepted, monthly and yearly", () => {
    expect(plan("free").price).toMatchObject({ monthly: 0, yearly: 0 });
    expect(plan("pro").price).toMatchObject({
      monthly: 12,
      yearly: 120,
      per: "account",
    });
    expect(plan("team").price).toMatchObject({
      monthly: 20,
      yearly: 192,
      per: "seat",
    });
    expect(plan("enterprise").price).toBeNull();
  });

  it("put the yearly prices as 01 does", () => {
    const pro = plan("pro").price!;
    const team = plan("team").price!;
    // Pro: $120 a year, $10 a month, "2 months free".
    expect([monthlyOfYearly(pro), monthsFree(pro)]).toEqual([10, 2]);
    expect(pro.yearlySaving).toBe("months");
    // Team: $192 a seat a year, $16 a month, 20% off.
    expect([monthlyOfYearly(team), yearlyDiscount(team)]).toEqual([16, 20]);
    expect(team.yearlySaving).toBe("percent");
  });

  it("hold D9's limits", () => {
    expect(value("free", "spaces")).toEqual({ personal: 1, project: 1 });
    expect(value("free", "agentKinds")).toEqual({ n: 3 });
    expect(value("free", "askMonthly")).toEqual({ n: 50 });
    expect(value("free", "activityDays")).toEqual({ n: 30 });
    expect(value("pro", "dreamNightly")).toEqual({ spaces: 5 });
    expect(value("pro", "viewers")).toEqual({ n: 3 });
    expect(value("pro", "askMonthly")).toEqual({ n: 1000 });
    expect(value("pro", "activityDays")).toEqual({ n: 90 });
  });

  it("say what isn't built yet", () => {
    const coming = PLANS.flatMap((p) =>
      p.features
        .filter((f) => f.availability === "coming")
        .map((f) => `${p.id}.${f.id}`),
    );
    expect(coming).toEqual([
      "pro.handoffsGates",
      "pro.viewers",
      "team.teamSpaces",
      "team.decisionLedger",
      "team.routing",
      "team.integrations",
      "team.auditYear",
      "enterprise.sso",
      "enterprise.euResidency",
      "enterprise.selfHosting",
    ]);
  });

  it("sell nothing while billing is closed", () => {
    expect(PLANS.map((p) => [p.id, p.action])).toEqual([
      ["free", "sign_in"],
      ["pro", "coming"],
      ["team", "coming"],
      ["enterprise", "contact"],
    ]);
  });

  it("keep receipts, export and Forget on every plan", () => {
    expect([...EVERY_PLAN]).toEqual(["receipts", "export", "forget"]);
  });

  it("have every sentence in both languages, with the numbers it needs", () => {
    for (const catalogue of [en, zh]) {
      const words = catalogue.ledger.public.pricing.features;
      for (const p of PLANS) {
        for (const f of p.features) {
          const sentence = words[f.id];
          expect(sentence, f.id).toBeTruthy();
          const wanted = [...sentence.matchAll(/\{(\w+)\}/g)].map((m) => m[1]);
          expect(wanted.sort(), `${p.id}.${f.id}`).toEqual(
            Object.keys(f.values ?? {}).sort(),
          );
        }
      }
    }
  });
});
