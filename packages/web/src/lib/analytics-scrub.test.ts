import type { CaptureResult } from "posthog-js";
import { describe, expect, it } from "vitest";
import { scrubEvent, scrubURL } from "./analytics-scrub";

// PostHog never receives a token: every URL it is sent goes without its
// query string, fragment or secret path segments.

const APP = "https://memax.app";

describe("scrubURL", () => {
  it.each([
    [`${APP}/oauth/authorize?request=8f2c1a&x=1#top`, `${APP}/oauth/authorize`],
    [`${APP}/signin/callback?code=one-time`, `${APP}/signin/callback`],
    [`${APP}/auth/callback?code=one-time&state=s`, `${APP}/auth/callback`],
    [`${APP}/device?code=WQRT-4821`, `${APP}/device`],
    [`${APP}/unsubscribe?token=tok_123`, `${APP}/unsubscribe`],
    [`${APP}/register?invite=inv_123`, `${APP}/register`],
    [`${APP}/join/inv_abc123`, `${APP}/join/:invite`],
    [`${APP}/join/inv_abc123/accept?x=1`, `${APP}/join/:invite/accept`],
    [`${APP}/invite/tok_abc123`, `${APP}/invite/:token`],
    [
      "https://api.memax.app/v1/invites/tok_abc/accept",
      "https://api.memax.app/v1/invites/:token/accept",
    ],
    // Pages with nothing secret keep their path.
    [`${APP}/memax-v2/memories/M-0219`, `${APP}/memax-v2/memories/M-0219`],
    ["/oauth/authorize?request=8f2c1a", "/oauth/authorize"],
    ["/join/inv_abc123", "/join/:invite"],
    ["/invite", "/invite"],
    ["memax.app", "memax.app"],
    ["$direct", "$direct"],
  ])("%s → %s", (input, want) => {
    expect(scrubURL(input)).toBe(want);
  });
});

describe("scrubEvent", () => {
  it("scrubs every URL property, the person's initial ones and clicked links", () => {
    const event = {
      uuid: "u1",
      event: "$pageview",
      properties: {
        $current_url: `${APP}/oauth/authorize?request=8f2c1a`,
        $pathname: "/oauth/authorize",
        $referrer:
          "https://api.memax.app/v1/auth/github/callback?code=gh&state=mcp:8f2c1a",
        $initial_current_url: `${APP}/signin/callback?code=one-time`,
        $initial_referrer: "$direct",
        $session_entry_url: `${APP}/device?code=WQRT-4821`,
        $prev_pageview_pathname: "/join/inv_abc123",
        $host: "memax.app",
        $referring_domain: "api.memax.app",
        landing: `${APP}/unsubscribe?token=tok_123`,
        step: "signed_in",
        files: 3,
        $elements: [{ tag_name: "a", attr__href: "/join/inv_abc123?ref=mail" }],
        $elements_chain: `a:attr__href="/unsubscribe?token=tok_123"nth-child="1"`,
      },
      $set: { $current_url: `${APP}/oauth/authorize?request=8f2c1a` },
      $set_once: { $initial_current_url: `${APP}/invite/tok_abc123` },
    } as unknown as CaptureResult;

    const out = scrubEvent(event)!;
    expect(out.properties).toEqual({
      $current_url: `${APP}/oauth/authorize`,
      $pathname: "/oauth/authorize",
      $referrer: "https://api.memax.app/v1/auth/github/callback",
      $initial_current_url: `${APP}/signin/callback`,
      $initial_referrer: "$direct",
      $session_entry_url: `${APP}/device`,
      $prev_pageview_pathname: "/join/:invite",
      $host: "memax.app",
      $referring_domain: "api.memax.app",
      landing: `${APP}/unsubscribe`,
      step: "signed_in",
      files: 3,
      $elements: [{ tag_name: "a", attr__href: "/join/:invite" }],
      $elements_chain: `a:attr__href="/unsubscribe"nth-child="1"`,
    });
    expect(out.$set).toEqual({ $current_url: `${APP}/oauth/authorize` });
    expect(out.$set_once).toEqual({
      $initial_current_url: `${APP}/invite/:token`,
    });
    // Nothing that looked like a token is left anywhere.
    expect(JSON.stringify(out)).not.toMatch(
      /8f2c1a|one-time|WQRT|tok_|inv_|code=|token=/,
    );
    expect(scrubEvent(null)).toBeNull();
  });
});
