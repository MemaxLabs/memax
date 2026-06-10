import { describe, expect, it } from "vitest";
import { en } from "@/i18n/locales/en";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";

describe("hub display", () => {
  it("uses the translated default name for empty personal hubs", () => {
    expect(getHubDisplayName({ hub_type: "personal", name: "" }, en)).toBe(
      en.hubs.defaultPersonalHubName,
    );
  });

  it("normalizes legacy untouched personal hub names", () => {
    expect(
      getHubDisplayName({ hub_type: "personal", name: "Jiahao Ye's Hub" }, en, {
        displayName: "Jiahao Ye",
      }),
    ).toBe(en.hubs.defaultPersonalHubName);
  });

  it("preserves user-renamed personal hub names", () => {
    expect(
      getHubDisplayName({ hub_type: "personal", name: "Research Vault" }, en, {
        displayName: "Jiahao Ye",
      }),
    ).toBe("Research Vault");
  });

  it("keeps team hub names unchanged", () => {
    expect(getHubDisplayName({ hub_type: "team", name: "Platform" }, en)).toBe(
      "Platform",
    );
  });

  it("prefers explicit hub icons over derived initials", () => {
    expect(
      getHubDisplayInitial(
        { hub_type: "team", name: "Platform", icon: "🛠" },
        en,
      ),
    ).toBe("🛠");
  });

  it("keeps whole emoji graphemes for hub icons", () => {
    expect(
      getHubDisplayInitial(
        { hub_type: "team", name: "Platform", icon: "👨‍💻" },
        en,
      ),
    ).toBe("👨‍💻");
  });

  it("derives initials from the resolved display name", () => {
    expect(
      getHubDisplayInitial(
        { hub_type: "personal", name: "Jiahao Ye's Hub" },
        en,
        { displayName: "Jiahao Ye" },
      ),
    ).toBe("M");
  });
});
