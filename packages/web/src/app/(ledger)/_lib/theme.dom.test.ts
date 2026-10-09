// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  getThemePreference,
  readThemeCookie,
  resolveTheme,
  setThemePreference,
  themeInitScript,
  THEME_COOKIE,
} from "./theme";

type Listener = (event: { matches: boolean }) => void;

function stubSystem(dark: boolean) {
  const listeners: Listener[] = [];
  const query = {
    matches: dark,
    addEventListener: (_: string, fn: Listener) => listeners.push(fn),
  };
  vi.stubGlobal(
    "matchMedia",
    vi.fn(() => query),
  );
  return {
    change(nextDark: boolean) {
      query.matches = nextDark;
      for (const fn of listeners) fn({ matches: nextDark });
    },
  };
}

function clearCookies() {
  for (const part of document.cookie.split(";")) {
    const name = part.split("=")[0]?.trim();
    if (name) document.cookie = `${name}=; Path=/; Max-Age=0`;
  }
}

function runInitScript() {
  new Function(themeInitScript)();
  return document.documentElement.getAttribute("data-theme");
}

afterEach(() => {
  clearCookies();
  document.documentElement.removeAttribute("data-theme");
  vi.unstubAllGlobals();
});

describe("readThemeCookie", () => {
  it.each([
    ["memax_theme=dark", "dark"],
    ["a=1; memax_theme=light; b=2", "light"],
    ["memax_theme=blue", null],
    ["xmemax_theme=dark", null],
    ["", null],
  ])("reads %j as %j", (cookie, expected) => {
    expect(readThemeCookie(cookie)).toBe(expected);
  });
});

describe("themeInitScript", () => {
  it.each([
    { cookie: null, systemDark: false },
    { cookie: null, systemDark: true },
    { cookie: "light", systemDark: true },
    { cookie: "dark", systemDark: false },
    { cookie: "sepia", systemDark: true },
  ])(
    "agrees with resolveTheme for cookie=$cookie, systemDark=$systemDark",
    ({ cookie, systemDark }) => {
      stubSystem(systemDark);
      if (cookie) document.cookie = `${THEME_COOKIE}=${cookie}; Path=/`;
      expect(runInitScript()).toBe(
        resolveTheme(readThemeCookie(document.cookie), systemDark),
      );
    },
  );

  it("follows the system while no theme is stored", () => {
    const system = stubSystem(false);
    expect(runInitScript()).toBe("light");
    system.change(true);
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("ignores the system once a theme is stored", () => {
    const system = stubSystem(false);
    document.cookie = `${THEME_COOKIE}=light; Path=/`;
    runInitScript();
    system.change(true);
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("never throws, even without matchMedia", () => {
    vi.stubGlobal("matchMedia", undefined);
    expect(() => runInitScript()).not.toThrow();
  });
});

describe("setThemePreference", () => {
  it("stores Carbon and applies it", () => {
    stubSystem(false);
    expect(setThemePreference("dark")).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(getThemePreference()).toBe("dark");
  });

  it("clears the cookie for system and follows the system", () => {
    stubSystem(true);
    setThemePreference("light");
    expect(getThemePreference()).toBe("light");
    expect(setThemePreference("system")).toBe("dark");
    expect(getThemePreference()).toBe("system");
    expect(document.cookie).not.toContain(THEME_COOKIE);
  });
});
