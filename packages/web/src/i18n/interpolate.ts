/**
 * Template interpolation for i18n strings.
 *
 * Lives in its own module (separate from `@/i18n/index.tsx`) so that
 * tests which mock the React-layer `@/i18n` module — replacing
 * `useLocale` and `useInterpolate` — don't accidentally break code
 * that calls the pure string helper outside React. The React-level
 * `@/i18n` still re-exports this function so one-name-one-home remains
 * true from a consumer's perspective.
 *
 * Missing keys render back as `{key}` so stale interpolations are
 * visible at a glance rather than silently rendering as empty strings.
 */
export function interpolate(
  str: string,
  vars?: Record<string, string | number>,
): string {
  if (!vars) return str;
  return str.replace(/\{(\w+)\}/g, (_, key) => String(vars[key] ?? `{${key}}`));
}
