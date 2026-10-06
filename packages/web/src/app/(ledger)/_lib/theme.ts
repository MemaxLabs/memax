// Ledger theme: Paper (light, the default) or Carbon (dark), set as
// `data-theme` on <html>. tokens.css scopes Carbon to
// [data-theme="dark"], so any element can also opt in on its own.
//
// The choice is a `memax_theme` cookie (`light` | `dark`); no cookie
// means "follow the system". The root layout stays static (it reads no
// cookies, so (ledger) pages can still be prerendered), and the inline
// script below applies the theme in <head> before the first paint, so
// there is no flash. No next-themes and no class-based dark mode.

export const THEME_COOKIE = "memax_theme";

export type Theme = "light" | "dark";
export type ThemePreference = Theme | "system";

const ONE_YEAR_SECONDS = 60 * 60 * 24 * 365;

export function parseTheme(value: string | null | undefined): Theme | null {
  return value === "light" || value === "dark" ? value : null;
}

/** The explicit theme stored in a `document.cookie` string, if any. */
export function readThemeCookie(cookieString: string): Theme | null {
  for (const part of cookieString.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name === THEME_COOKIE) return parseTheme(rest.join("="));
  }
  return null;
}

export function resolveTheme(explicit: Theme | null, systemDark: boolean) {
  return explicit ?? (systemDark ? "dark" : "light");
}

/**
 * Runs inline in <head> before first paint. It must stay tiny,
 * dependency-free ES5, and in step with readThemeCookie/resolveTheme
 * (theme.test.ts runs it against both). It also follows live changes to
 * the system preference while no explicit theme is stored.
 */
export const themeInitScript = `(function(){try{var r=document.documentElement,q=window.matchMedia("(prefers-color-scheme: dark)"),c=function(){var m=document.cookie.match(/(?:^|;\\s*)${THEME_COOKIE}=(light|dark)(?:;|$)/);return m?m[1]:null},a=function(){r.setAttribute("data-theme",c()||(q.matches?"dark":"light"))};a();q.addEventListener("change",function(){if(!c())a()})}catch(e){}})();`;

function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** Client only: the stored preference ("system" when no cookie). */
export function getThemePreference(): ThemePreference {
  return readThemeCookie(document.cookie) ?? "system";
}

/** Client only: store a preference and apply it to <html> immediately. */
export function setThemePreference(preference: ThemePreference): Theme {
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  if (preference === "system") {
    document.cookie = `${THEME_COOKIE}=; Path=/; Max-Age=0; SameSite=Lax${secure}`;
  } else {
    document.cookie = `${THEME_COOKIE}=${preference}; Path=/; Max-Age=${ONE_YEAR_SECONDS}; SameSite=Lax${secure}`;
  }
  const theme = resolveTheme(parseTheme(preference), systemPrefersDark());
  document.documentElement.setAttribute("data-theme", theme);
  return theme;
}
