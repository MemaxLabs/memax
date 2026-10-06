import type { Metadata } from "next";
import tokens from "@memaxlabs/ledger-tokens/tokens.json";
import conflict from "@memaxlabs/ledger-tokens/assets/icons/state-conflict.svg";
import faded from "@memaxlabs/ledger-tokens/assets/icons/state-faded.svg";
import forgotten from "@memaxlabs/ledger-tokens/assets/icons/state-forgotten.svg";
import kept from "@memaxlabs/ledger-tokens/assets/icons/state-kept.svg";
import merged from "@memaxlabs/ledger-tokens/assets/icons/state-merged.svg";
import proposed from "@memaxlabs/ledger-tokens/assets/icons/state-proposed.svg";
import stale from "@memaxlabs/ledger-tokens/assets/icons/state-stale.svg";
import { en } from "@/i18n/locales/en";
import {
  TokenSpecimen,
  type SpecimenData,
  type TypeStyleName,
} from "./token-specimen";

// /dev/ledger/tokens: every Ledger colour token as a swatch in Paper and
// Carbon side by side, every type style from type.css, the space,
// radius and size scales, and the state glyph SVGs. Built from
// tokens.json, so it can't drift from the package (specimen.test.ts
// checks tokens.json against tokens.css and type.css). The first
// visual-regression fixture (e2e/ledger-tokens.e2e.ts).

export const metadata: Metadata = { title: en.ledger.devTokens.title };

const CONTROL_SIZES = new Set(["control-sm", "control-md", "control-lg"]);

function isTypeStyleName(name: string): name is TypeStyleName {
  return name in en.ledger.devTokens.samples;
}

function metrics(style: {
  fontSize: string;
  lineHeight: string;
  fontWeight: number;
  letterSpacing?: string;
  fontStyle?: string;
}) {
  return [
    `${style.fontSize}/${style.lineHeight}`,
    String(style.fontWeight),
    style.fontStyle,
    style.letterSpacing,
  ]
    .filter(Boolean)
    .join(" · ");
}

function specimenData(): SpecimenData {
  return {
    colours: tokens.color.tokens.map(({ name, value }) => ({
      name,
      light: value.light,
      dark: value.dark,
    })),
    shadows: tokens.shadow.tokens.map(({ name }) => name),
    typeStyles: tokens.type.groups.flatMap((group) =>
      group.styles.flatMap((style) =>
        isTypeStyleName(style.name)
          ? [{ name: style.name, metrics: metrics(style) }]
          : [],
      ),
    ),
    spacing: tokens.spacing.tokens.map(({ name, value }) => ({ name, value })),
    radius: tokens.radius.tokens.map(({ name, value }) => ({ name, value })),
    controls: tokens.size.tokens
      .filter(({ name }) => CONTROL_SIZES.has(name))
      .map(({ name, value }) => ({ name, value })),
    measures: tokens.size.tokens
      .filter(({ name }) => !CONTROL_SIZES.has(name))
      .map(({ name, value }) => ({ name, value })),
    states: [
      { name: "proposed", src: proposed.src },
      { name: "kept", src: kept.src },
      { name: "merged", src: merged.src },
      { name: "stale", src: stale.src },
      { name: "faded", src: faded.src },
      { name: "conflict", src: conflict.src },
      { name: "forgotten", src: forgotten.src },
    ],
  };
}

export default function LedgerTokensPage() {
  return <TokenSpecimen data={specimenData()} />;
}
