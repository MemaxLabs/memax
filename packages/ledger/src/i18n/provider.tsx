import {
  createContext,
  use,
  useMemo,
  type AnchorHTMLAttributes,
  type ComponentType,
  type ReactNode,
  type Ref,
} from "react";
import { AGENTS, type AgentInfo } from "../lib/agents";
import { en, type LedgerStrings } from "./en";
import { zh } from "./zh";

export type LedgerLocale = "en" | "zh";

/** The catalogues, by locale. */
export const ledgerStrings: Record<LedgerLocale, LedgerStrings> = { en, zh };

/** Props every Ledger link passes. Next.js `Link` satisfies this. */
export interface LedgerLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  href: string;
  ref?: Ref<HTMLAnchorElement>;
}

export type LedgerLinkComponent = ComponentType<LedgerLinkProps>;

type DeepPartial<T> = {
  [K in keyof T]?: T[K] extends string ? T[K] : DeepPartial<T[K]>;
};

/** Overrides for some strings of a locale, merged over its catalogue. */
export type LedgerStringOverrides = DeepPartial<LedgerStrings>;

export interface LedgerContextValue {
  locale: LedgerLocale;
  strings: LedgerStrings;
  agents: Readonly<Record<string, AgentInfo>>;
  Link: LedgerLinkComponent;
  formatNumber: (n: number) => string;
  /** "1, 2 or 3" (disjunction) and "A, B and C" (conjunction), without a serial comma. */
  formatList: (items: string[], type?: "conjunction" | "disjunction") => string;
}

function DefaultLink(props: LedgerLinkProps) {
  return <a {...props} />;
}

// House style has no serial comma ("emails and issue comments", "1, 2 or 3"),
// which is the en-GB list style. Numbers use the US grouping the boards show.
const INTL_TAGS: Record<LedgerLocale, { number: string; list: string }> = {
  en: { number: "en-US", list: "en-GB" },
  zh: { number: "zh-CN", list: "zh-CN" },
};

function createValue(
  locale: LedgerLocale,
  strings: LedgerStrings,
  agents: Readonly<Record<string, AgentInfo>>,
  Link: LedgerLinkComponent,
): LedgerContextValue {
  const tags = INTL_TAGS[locale];
  const numbers = new Intl.NumberFormat(tags.number);
  const lists = {
    conjunction: new Intl.ListFormat(tags.list, { type: "conjunction" }),
    disjunction: new Intl.ListFormat(tags.list, { type: "disjunction" }),
  };
  return {
    locale,
    strings,
    agents,
    Link,
    formatNumber: (n) => numbers.format(n),
    formatList: (items, type = "conjunction") => lists[type].format(items),
  };
}

const DEFAULT_VALUE = createValue("en", en, AGENTS, DefaultLink);

const LedgerContext = createContext<LedgerContextValue>(DEFAULT_VALUE);

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function mergeDeep<T>(base: T, overrides: unknown): T {
  if (!isPlainObject(base) || !isPlainObject(overrides)) return base;
  const out: Record<string, unknown> = { ...base };
  for (const [key, value] of Object.entries(overrides)) {
    if (value === undefined) continue;
    out[key] = isPlainObject(value) ? mergeDeep(out[key], value) : value;
  }
  return out as T;
}

export interface LedgerProviderProps {
  /** Which catalogue to use. Also set `lang` on the page (`zh-CN`) so the CJK type rules apply. */
  locale?: LedgerLocale;
  /** Product-specific wording, merged over the locale's catalogue. */
  strings?: LedgerStringOverrides;
  /** Extra agents, merged over the shipped `AGENTS` registry. */
  agents?: Readonly<Record<string, AgentInfo>>;
  /** Renders every Ledger link (rail items, `Button href`, `Cite href`). Pass Next.js `Link`. */
  linkComponent?: LedgerLinkComponent;
  children?: ReactNode;
}

/**
 * Supplies the locale, strings, agent registry and link component to every
 * Ledger component below it. Without a provider, components use English and
 * plain anchors.
 */
export function LedgerProvider({
  locale = "en",
  strings,
  agents,
  linkComponent,
  children,
}: LedgerProviderProps) {
  const value = useMemo(
    () =>
      createValue(
        locale,
        strings
          ? mergeDeep(ledgerStrings[locale], strings)
          : ledgerStrings[locale],
        agents ? { ...AGENTS, ...agents } : AGENTS,
        linkComponent ?? DefaultLink,
      ),
    [locale, strings, agents, linkComponent],
  );
  return <LedgerContext value={value}>{children}</LedgerContext>;
}

/** The current Ledger locale, strings, registry and link component. */
export function useLedger(): LedgerContextValue {
  return use(LedgerContext);
}
