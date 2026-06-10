export const HUB_ACCENTS = [
  "violet",
  "blue",
  "green",
  "amber",
  "rose",
  "slate",
] as const;

export type HubAccent = (typeof HUB_ACCENTS)[number];

interface HubAccentStyle {
  background: string;
  foreground: string;
  halo: string;
  swatch: string;
}

const HUB_ACCENT_STYLES: Record<HubAccent, HubAccentStyle> = {
  violet: {
    background:
      "linear-gradient(135deg, oklch(0.55 0.15 290 / 0.16), oklch(0.62 0.16 260 / 0.12))",
    foreground: "oklch(0.45 0.15 290)",
    halo: "radial-gradient(closest-side, oklch(0.55 0.15 290 / 0.24), transparent 70%)",
    swatch:
      "linear-gradient(135deg, oklch(0.55 0.15 290), oklch(0.62 0.16 260))",
  },
  blue: {
    background:
      "linear-gradient(135deg, oklch(0.62 0.11 245 / 0.16), oklch(0.72 0.1 210 / 0.12))",
    foreground: "oklch(0.5 0.11 245)",
    halo: "radial-gradient(closest-side, oklch(0.62 0.11 245 / 0.24), transparent 70%)",
    swatch:
      "linear-gradient(135deg, oklch(0.62 0.11 245), oklch(0.72 0.1 210))",
  },
  green: {
    background:
      "linear-gradient(135deg, oklch(0.7 0.13 155 / 0.16), oklch(0.76 0.11 185 / 0.12))",
    foreground: "oklch(0.54 0.12 160)",
    halo: "radial-gradient(closest-side, oklch(0.7 0.13 155 / 0.24), transparent 70%)",
    swatch:
      "linear-gradient(135deg, oklch(0.7 0.13 155), oklch(0.76 0.11 185))",
  },
  amber: {
    background:
      "linear-gradient(135deg, oklch(0.68 0.14 35 / 0.16), oklch(0.72 0.15 55 / 0.12))",
    foreground: "oklch(0.5 0.16 35)",
    halo: "radial-gradient(closest-side, oklch(0.68 0.14 35 / 0.24), transparent 70%)",
    swatch: "linear-gradient(135deg, oklch(0.68 0.14 35), oklch(0.72 0.15 55))",
  },
  rose: {
    background:
      "linear-gradient(135deg, oklch(0.66 0.17 8 / 0.16), oklch(0.7 0.14 345 / 0.12))",
    foreground: "oklch(0.53 0.16 8)",
    halo: "radial-gradient(closest-side, oklch(0.66 0.17 8 / 0.24), transparent 70%)",
    swatch: "linear-gradient(135deg, oklch(0.66 0.17 8), oklch(0.7 0.14 345))",
  },
  slate: {
    background:
      "linear-gradient(135deg, oklch(0.68 0.02 260 / 0.16), oklch(0.78 0.01 260 / 0.12))",
    foreground: "oklch(0.48 0.02 260)",
    halo: "radial-gradient(closest-side, oklch(0.68 0.02 260 / 0.24), transparent 70%)",
    swatch:
      "linear-gradient(135deg, oklch(0.68 0.02 260), oklch(0.78 0.01 260))",
  },
};

export function isHubAccent(value?: string | null): value is HubAccent {
  return HUB_ACCENTS.includes((value ?? "") as HubAccent);
}

export function getDefaultHubAccent(hubType?: string | null): HubAccent {
  return hubType === "team" ? "amber" : "violet";
}

export function resolveHubAccent(
  accent?: string | null,
  hubType?: string | null,
): HubAccent {
  return isHubAccent(accent) ? accent : getDefaultHubAccent(hubType);
}

export function getHubAccentStyle(
  accent?: string | null,
  hubType?: string | null,
) {
  const key = resolveHubAccent(accent, hubType);
  return {
    key,
    ...HUB_ACCENT_STYLES[key],
  };
}
