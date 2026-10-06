import { Fragment, createElement, type ReactNode } from "react";

/** A count-dependent string. Chinese has no inflection, so `one` and `other` read the same there. */
export interface Plural {
  one: string;
  other: string;
}

const PLACEHOLDER = /\{(\w+)\}/g;

/**
 * Fills `{name}` placeholders. Unknown placeholders are left as written, so a
 * missing value is visible in review instead of silently disappearing.
 */
export function format(
  template: string,
  values: Record<string, string | number>,
): string {
  return template.replace(PLACEHOLDER, (match, key: string) =>
    key in values ? String(values[key]) : match,
  );
}

/**
 * Like `format`, but the values may be React nodes (a styled count, an ID in
 * mono). Punctuation stays in the template's own text run, so there is never a
 * stray space before it (design review §2).
 */
export function formatNodes(
  template: string,
  values: Record<string, ReactNode>,
): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let index = 0;
  for (const match of template.matchAll(PLACEHOLDER)) {
    const [whole, key] = match;
    const at = match.index;
    if (at > last) out.push(template.slice(last, at));
    out.push(
      key !== undefined && key in values
        ? createElement(Fragment, { key: `v${index++}` }, values[key])
        : whole,
    );
    last = at + whole.length;
  }
  if (last < template.length) out.push(template.slice(last));
  return out;
}

/** Picks the singular or plural form and fills `{n}`. */
export function plural(
  forms: Plural,
  n: number,
  formatNumber: (n: number) => string = String,
): string {
  return n === 1 ? forms.one : format(forms.other, { n: formatNumber(n) });
}
