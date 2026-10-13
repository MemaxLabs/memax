import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { zh } from "@/i18n/locales/zh";
import { CLOUD_POSTURE } from "./cloud-posture";
import { NAMES, processorRows } from "./settings-copy";

// The public Security page's facts (cloud-posture.json, written by the
// server's internal/trust from its defaults). The page words them with
// Settings › Security's catalogue and NAMES, so every value must have a
// name and a sentence; and the docs site's Security page must name the
// same services, hosts and models, so the two public pages agree.

const docsSecurity = readFileSync(
  path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../../../../docs-site/content/docs/concepts/security.mdx",
  ),
  "utf8",
);

const hostCompany = (host: string) => host.split("/", 1)[0]!;

describe("Memax cloud's posture", () => {
  it("is the server's defaults: Neon in us-west-2, Fly in sjc, R2 and Workers", () => {
    expect(CLOUD_POSTURE.residency).toEqual([
      { holds: "database", provider: "neon", region: "us-west-2" },
      { holds: "compute", provider: "fly", region: "sjc" },
      { holds: "objects", provider: "r2", region: null },
      { holds: "edge", provider: "cloudflare", region: null },
    ]);
    expect(CLOUD_POSTURE.backupDays).toBe(7);
  });

  it("routes every model call to pinned zero-retention hosts at fp8 or better", () => {
    const openRouter = CLOUD_POSTURE.processors.find(
      (p) => p.name === "openrouter",
    );
    expect(openRouter?.retention).toBe("zero");
    for (const use of openRouter?.uses ?? []) {
      expect(use.zeroRetention, use.use).toBe(true);
      expect(use.minPrecision, use.use).toBe("fp8");
      expect(use.hosts.length, use.use).toBeGreaterThan(0);
    }
  });

  it("says Voyage's retention isn't confirmed", () => {
    const voyage = CLOUD_POSTURE.processors.find((p) => p.name === "voyage");
    expect(voyage?.retention).toBe("unconfirmed");
    const row = processorRows(en.ledger.settings.security, [voyage!], "en")[0]!;
    expect(row.unconfirmed).toBe(true);
    expect(row.keeps).toMatch(/^Not confirmed\./);
  });

  it("has a name for every provider, service, host and model", () => {
    for (const place of CLOUD_POSTURE.residency) {
      expect(NAMES.providers, place.provider).toHaveProperty(place.provider);
    }
    for (const processor of CLOUD_POSTURE.processors) {
      expect(NAMES.processors, processor.name).toHaveProperty(processor.name);
      for (const use of processor.uses) {
        // The gateway's models have names; Voyage's read as their slugs
        // (voyage-4), which is what Voyage calls them.
        if (use.model && processor.name === "openrouter") {
          expect(NAMES.models, use.model).toHaveProperty([use.model]);
        }
        for (const host of use.hosts) {
          expect(NAMES.hosts, host).toHaveProperty([hostCompany(host)]);
        }
      }
    }
  });

  it("has words for everything it holds and does, in both languages", () => {
    for (const catalogue of [en, zh]) {
      const words = catalogue.ledger.settings.security;
      for (const place of CLOUD_POSTURE.residency) {
        expect(words.residency.holds[place.holds], place.holds).toBeTruthy();
      }
      for (const processor of CLOUD_POSTURE.processors) {
        expect(words.processors.retention[processor.retention]).toBeTruthy();
        for (const use of processor.uses) {
          expect(words.processors.uses[use.use], use.use).toBeTruthy();
        }
      }
    }
  });

  it("names the same services, hosts and models as the docs site's Security page", () => {
    const named = new Set<string>();
    for (const processor of CLOUD_POSTURE.processors) {
      named.add(NAMES.processors[processor.name]);
      for (const use of processor.uses) {
        if (use.model && processor.name === "openrouter") {
          named.add(NAMES.models[use.model as keyof typeof NAMES.models]);
        }
        for (const host of use.hosts) {
          named.add(NAMES.hosts[hostCompany(host) as keyof typeof NAMES.hosts]);
        }
      }
    }
    const missing = [...named].filter((name) => !docsSecurity.includes(name));
    expect(
      missing,
      "add them to packages/docs-site/content/docs/concepts/security.mdx",
    ).toEqual([]);
  });
});
