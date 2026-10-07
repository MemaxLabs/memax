import { describe, expect, it } from "vitest";
import cases from "../../../server/internal/oauthredirect/testdata/cases.json";
import { acceptedRedirect, opensInBrowser } from "./oauth-redirects";

// The consent page accepts the redirect URIs the server does: the same
// rules (redirects.json) and the same cases as the server's test.

describe("acceptedRedirect", () => {
  it("has the server's cases", () => {
    expect(cases.valid.length).toBeGreaterThan(20);
  });

  it.each(cases.valid as [string, boolean][])("%s → %s", (uri, want) => {
    expect(acceptedRedirect(uri)).toBe(want);
  });
});

describe("opensInBrowser", () => {
  it("tells a page the browser loads from an app's scheme", () => {
    expect(opensInBrowser("https://vscode.dev/redirect?code=c")).toBe(true);
    expect(opensInBrowser("http://127.0.0.1:59656/?code=c")).toBe(true);
    expect(
      opensInBrowser("cursor://anysphere.cursor-mcp/oauth/callback?code=c"),
    ).toBe(false);
  });
});
