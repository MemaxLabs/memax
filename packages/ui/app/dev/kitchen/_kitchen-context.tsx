"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

// ─── Text Hierarchy ───

export interface TextHierarchy {
  /** Main headings, titles — highest contrast */
  primary: number;
  /** Body text, descriptions — clearly readable */
  secondary: number;
  /** Supporting text, timestamps, metadata — visible but receding */
  tertiary: number;
  /** Placeholder text, disabled labels — barely there */
  muted: number;
}

// ─── Palette Presets ───

export interface GrayStep {
  l: number;
  c: number;
  h: number;
  label: string;
}

export interface Palette {
  name: string;
  desc: string;
  signature: string;
  signatureMuted: string;
  grays: readonly GrayStep[];
  text: TextHierarchy;
}

// MECE signature color options (for exploration):
// 1. Safe (neutral) — no accent, pure black/gray. Safe default.
// 2. Warm Apricot — previous direction. Kept for comparison.
// 3. Cool Slate — cool blue-gray. Linear/Vercel direction.
// 4. Earth Copper — deeper warm. Notion/editorial direction.
// 5. Dream Violet — CURRENT. Purple-blue inspired by dream feature. Distinctive.
export const PALETTES = {
  safe: {
    name: "Safe Neutral",
    desc: "No signature accent. Pure black/gray. The safest default — let content speak.",
    signature: "oklch(0.50 0 0)",
    signatureMuted: "oklch(0.50 0 0 / 0.10)",
    text: {
      primary: 0.9,
      secondary: 0.5,
      tertiary: 0.38,
      muted: 0.25,
    },
    grays: [
      { l: 0.145, c: 0, h: 0, label: "bg" },
      { l: 0.205, c: 0, h: 0, label: "card" },
      { l: 0.269, c: 0, h: 0, label: "muted" },
      { l: 0.275, c: 0, h: 0, label: "border" },
      { l: 0.371, c: 0, h: 0, label: "ring" },
      { l: 0.439, c: 0, h: 0, label: "input" },
      { l: 0.556, c: 0, h: 0, label: "muted-fg" },
      { l: 0.708, c: 0, h: 0, label: "secondary" },
      { l: 0.922, c: 0, h: 0, label: "lt-border" },
      { l: 0.965, c: 0, h: 0, label: "lt-bg" },
    ],
  },
  warm: {
    name: "Warm Apricot",
    desc: "Signature warm apricot (hue 60). Warm gray tint. Premium but approachable.",
    signature: "oklch(0.72 0.14 60)",
    signatureMuted: "oklch(0.72 0.14 60 / 0.15)",
    text: {
      primary: 0.9,
      secondary: 0.5,
      tertiary: 0.4,
      muted: 0.25,
    },
    grays: [
      { l: 0.145, c: 0.005, h: 70, label: "bg" },
      { l: 0.205, c: 0.005, h: 70, label: "card" },
      { l: 0.269, c: 0.004, h: 70, label: "muted" },
      { l: 0.275, c: 0.004, h: 70, label: "border" },
      { l: 0.371, c: 0.004, h: 70, label: "ring" },
      { l: 0.439, c: 0.003, h: 70, label: "input" },
      { l: 0.556, c: 0.003, h: 70, label: "muted-fg" },
      { l: 0.708, c: 0.003, h: 70, label: "secondary" },
      { l: 0.922, c: 0.003, h: 70, label: "lt-border" },
      { l: 0.965, c: 0.002, h: 70, label: "lt-bg" },
    ],
  },
  cool: {
    name: "Cool Slate",
    desc: "Cool blue-gray accent (hue 240). Linear/Vercel direction. Technical, precise.",
    signature: "oklch(0.65 0.10 240)",
    signatureMuted: "oklch(0.65 0.10 240 / 0.12)",
    text: {
      primary: 0.9,
      secondary: 0.52,
      tertiary: 0.4,
      muted: 0.25,
    },
    grays: [
      { l: 0.145, c: 0.004, h: 250, label: "bg" },
      { l: 0.205, c: 0.004, h: 250, label: "card" },
      { l: 0.269, c: 0.003, h: 250, label: "muted" },
      { l: 0.275, c: 0.003, h: 250, label: "border" },
      { l: 0.371, c: 0.003, h: 250, label: "ring" },
      { l: 0.439, c: 0.003, h: 250, label: "input" },
      { l: 0.556, c: 0.002, h: 250, label: "muted-fg" },
      { l: 0.708, c: 0.002, h: 250, label: "secondary" },
      { l: 0.922, c: 0.002, h: 250, label: "lt-border" },
      { l: 0.965, c: 0.002, h: 250, label: "lt-bg" },
    ],
  },
  earth: {
    name: "Earth Copper",
    desc: "Deep copper accent (hue 45). Notion/editorial direction. Grounded, trustworthy.",
    signature: "oklch(0.62 0.14 45)",
    signatureMuted: "oklch(0.62 0.14 45 / 0.12)",
    text: {
      primary: 0.92,
      secondary: 0.55,
      tertiary: 0.42,
      muted: 0.28,
    },
    grays: [
      { l: 0.145, c: 0.006, h: 55, label: "bg" },
      { l: 0.205, c: 0.006, h: 55, label: "card" },
      { l: 0.269, c: 0.005, h: 55, label: "muted" },
      { l: 0.275, c: 0.005, h: 55, label: "border" },
      { l: 0.371, c: 0.005, h: 55, label: "ring" },
      { l: 0.439, c: 0.004, h: 55, label: "input" },
      { l: 0.556, c: 0.004, h: 55, label: "muted-fg" },
      { l: 0.708, c: 0.003, h: 55, label: "secondary" },
      { l: 0.922, c: 0.003, h: 55, label: "lt-border" },
      { l: 0.965, c: 0.002, h: 55, label: "lt-bg" },
    ],
  },
  dream: {
    name: "Dream Violet",
    desc: "Purple-blue inspired by dreams. Distinctive, creative, memax's signature feature.",
    signature: "oklch(0.62 0.16 290)",
    signatureMuted: "oklch(0.62 0.16 290 / 0.12)",
    text: {
      primary: 0.9,
      secondary: 0.52,
      tertiary: 0.4,
      muted: 0.25,
    },
    grays: [
      { l: 0.145, c: 0.005, h: 290, label: "bg" },
      { l: 0.205, c: 0.005, h: 290, label: "card" },
      { l: 0.269, c: 0.004, h: 290, label: "muted" },
      { l: 0.275, c: 0.004, h: 290, label: "border" },
      { l: 0.371, c: 0.004, h: 290, label: "ring" },
      { l: 0.439, c: 0.003, h: 290, label: "input" },
      { l: 0.556, c: 0.003, h: 290, label: "muted-fg" },
      { l: 0.708, c: 0.003, h: 290, label: "secondary" },
      { l: 0.922, c: 0.003, h: 290, label: "lt-border" },
      { l: 0.965, c: 0.002, h: 290, label: "lt-bg" },
    ],
  },
} as const;

export type PaletteKey = keyof typeof PALETTES;

export const ACCENTS = [
  {
    name: "Ember",
    oklch: "oklch(0.65 0.18 35)",
    dark: "oklch(0.70 0.17 35)",
    use: "CTA / primary actions",
  },
  {
    name: "Honeycomb",
    oklch: "oklch(0.82 0.16 85)",
    dark: "oklch(0.78 0.14 85)",
    use: "Highlights / hover / search match",
  },
  {
    name: "Sage",
    oklch: "oklch(0.72 0.10 155)",
    dark: "oklch(0.75 0.09 155)",
    use: "Success / saved / connected",
  },
  {
    name: "Dusk",
    oklch: "oklch(0.55 0.08 260)",
    dark: "oklch(0.65 0.08 260)",
    use: "Links / info accents",
  },
  {
    name: "Plum",
    oklch: "oklch(0.52 0.14 310)",
    dark: "oklch(0.62 0.13 310)",
    use: "Premium / Pro indicators",
  },
  {
    name: "Clay",
    oklch: "oklch(0.58 0.06 50)",
    dark: "oklch(0.45 0.05 50)",
    use: "Secondary / tertiary UI",
  },
  {
    name: "Signal",
    oklch: "oklch(0.62 0.22 25)",
    dark: "oklch(0.68 0.20 25)",
    use: "Error / destructive / critical",
  },
] as const;

// ─── Context ───

interface KitchenState {
  paletteKey: PaletteKey;
  palette: Palette;
  setPaletteKey: (key: PaletteKey) => void;
  grayOklch: (g: GrayStep) => string;
  isDark: boolean;
  toggleDark: () => void;
}

const KitchenContext = createContext<KitchenState | null>(null);

export function KitchenProvider({ children }: { children: React.ReactNode }) {
  const [paletteKey, setPaletteKey] = useState<PaletteKey>("dream");
  // Default true (matches SSR), sync from DOM after mount to avoid hydration mismatch
  const [isDark, setIsDark] = useState(true);

  useEffect(() => {
    setIsDark(document.documentElement.classList.contains("dark"));
  }, []);

  const palette = PALETTES[paletteKey];

  const grayOklch = useCallback(
    (g: GrayStep) => `oklch(${g.l} ${g.c} ${g.h})`,
    [],
  );

  const toggleDark = useCallback(() => {
    setIsDark((prev) => {
      const next = !prev;
      if (next) {
        document.documentElement.classList.add("dark");
      } else {
        document.documentElement.classList.remove("dark");
      }
      return next;
    });
  }, []);

  const value = useMemo(
    () => ({
      paletteKey,
      palette,
      setPaletteKey,
      grayOklch,
      isDark,
      toggleDark,
    }),
    [paletteKey, palette, grayOklch, isDark, toggleDark],
  );

  return (
    <KitchenContext.Provider value={value}>
      <div
        style={
          {
            "--signature": palette.signature,
            "--signature-muted": palette.signatureMuted,
          } as React.CSSProperties
        }
      >
        {children}
      </div>
    </KitchenContext.Provider>
  );
}

export function useKitchen() {
  const ctx = useContext(KitchenContext);
  if (!ctx) throw new Error("useKitchen must be inside KitchenProvider");
  return ctx;
}
