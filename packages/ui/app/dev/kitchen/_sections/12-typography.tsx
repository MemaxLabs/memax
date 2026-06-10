// Maps to: globals.css typography tokens, font system, text-fg-* hierarchy
// Source of truth: packages/ui/src/typography.css (shared across web + ui)
// Source of truth: --text-input through --text-nano in globals.css
// Usage: every text element in the product maps to one row in the type scale
"use client";

import { Section, DemoCard } from "../_shared";

/* ── Type Scale ── */

interface TypeRow {
  token: string;
  px: string;
  tailwind: string;
  weight: string;
  usage: string;
  sample: string;
}

const TYPE_SCALE: TypeRow[] = [
  {
    token: "--text-title",
    px: "18px",
    tailwind: "text-[18px]",
    weight: "bold",
    usage: "Modal title, page header",
    sample: "Memory title here",
  },
  {
    token: "--text-input",
    px: "15→16px",
    tailwind: "text-[15px] sm:text-[16px]",
    weight: "regular",
    usage: "Bar input, placeholders",
    sample: "What did we agree on for v2?",
  },
  {
    token: "--text-body",
    px: "14px",
    tailwind: "text-[14px]",
    weight: "regular",
    usage: "Card title, body, AI answer",
    sample:
      "Your deployment strategy combines blue-green with canary releases.",
  },
  {
    token: "--text-secondary",
    px: "13px",
    tailwind: "text-[13px]",
    weight: "regular",
    usage: "Snippet, citation, button label",
    sample: "Copy to clipboard",
  },
  {
    token: "--text-caption",
    px: "12px",
    tailwind: "text-[12px]",
    weight: "regular",
    usage: "Kind, timestamp, meta",
    sample: "core · 2h ago · 3 sources",
  },
  {
    token: "--text-micro",
    px: "11px",
    tailwind: "text-[11px]",
    weight: "regular",
    usage: "Group header, keyboard hint",
    sample: "RECENT MEMORIES",
  },
  {
    token: "--text-nano",
    px: "10px",
    tailwind: "text-[10px]",
    weight: "semibold",
    usage: "Citation badge, section label, DemoCard label",
    sample: "TYPE SCALE",
  },
];

/* ── Heading Hierarchy ── */

interface HeadingRow {
  level: string;
  px: string;
  weight: string;
  tailwind: string;
  usage: string;
  sample: string;
}

const HEADINGS: HeadingRow[] = [
  {
    level: "H1",
    px: "21px",
    weight: "700 (bold)",
    tailwind: "text-[21px] font-bold",
    usage: "Memory detail title, page header (one per view)",
    sample: "Settings",
  },
  {
    level: "H2",
    px: "16px",
    weight: "700 (bold)",
    tailwind: "text-[16px] font-bold",
    usage: "Section header, note detail title",
    sample: "Deployment Strategy",
  },
  {
    level: "H3",
    px: "14px",
    weight: "600 (semibold)",
    tailwind: "text-[14px] font-semibold",
    usage: "Card title, list item title, sub-section",
    sample: "React Server Components",
  },
];

/* ── Weight Scale ── */

const WEIGHTS = [
  {
    value: 400,
    name: "Regular",
    tailwind: "font-normal",
    usage: "Body text, descriptions, input text",
  },
  {
    value: 500,
    name: "Medium",
    tailwind: "font-medium",
    usage: "Emphasis within body, dropdown items, nav labels",
  },
  {
    value: 600,
    name: "Semibold",
    tailwind: "font-semibold",
    usage: "Card titles, badges, section sub-headers (H3)",
  },
  {
    value: 700,
    name: "Bold",
    tailwind: "font-bold",
    usage: "Page headings (H1, H2), modal titles",
  },
];

/* ── Font Families ──
 *
 * Native sans + one mono webfont. Sans = whatever the OS ships; mono =
 * Geist Mono via next/font.
 *
 * Why native sans (and not Geist / Inter / Space Grotesk):
 *   1. SF Pro on macOS & iOS. Segoe UI on Windows. Roboto on Android. These
 *      are tuned by the OS vendor for native UI rendering and pair perfectly
 *      with the rest of the operating system's chrome. No webfont matches
 *      that tuning on a given platform.
 *   2. The "admin tool feels more polished than the product" bug we hit on
 *      2026-04-14 was caused by next/font/google injecting a literal `Geist`
 *      fallback into every font stack. On developer machines with Geist
 *      installed locally, Chrome rendered the local Geist instead of SF Pro
 *      — thin, geometric, and mismatched against every other native element.
 *      Non-devs never saw the product the way we did.
 *   3. Zero webfont bytes, zero FOUT, zero FOIT. Hook budget stays tight and
 *      the bar feels instant even on throttled networks.
 *   4. Content > chrome — native fonts disappear into the OS chrome and
 *      let memory content be the loudest thing on screen. Adopting a brand
 *      sans would have been chrome shouting.
 *
 * font-heading and font-display both resolve to var(--font-sans) — semantic
 * intent in markup, single native family at runtime. Hierarchy is still
 * size × weight × tracking, never family swap.
 *
 * Mono stays a deliberate webfont (Geist Mono) because cross-OS system mono
 * drift is too wide — Menlo vs Consolas vs DejaVu Sans Mono renders code
 * blocks completely differently, and the bar's kbd glyphs (⌘ ↵ ⌥) need
 * identical metrics everywhere.
 */

const FONTS = [
  {
    name: "Sans — native system",
    tailwind: "font-sans",
    sample: "Remember what your agents learn. Recall it anywhere.",
    desc: "SF Pro on macOS/iOS, Segoe UI on Windows, Roboto on Android, system-ui elsewhere. Every piece of UI text. Zero webfont load.",
  },
  {
    name: "Heading — native (alias)",
    tailwind: "font-heading",
    sample: "The bar is a search input that happens to save.",
    desc: "Semantic alias of font-sans. Use it on section headers to signal intent. Same native family, tighter tracking via text-display-* classes.",
  },
  {
    name: "Mono — Geist Mono",
    tailwind: "font-mono",
    sample: "const memory = await memax.recall(query);",
    desc: "Code snippets, keyboard glyphs, tokens, CLI output. The only webfont we ship — cross-OS mono drift (Menlo vs Consolas vs DejaVu) is too wide to leave to the system.",
  },
];

/* ── Line Height ── */

const LINE_HEIGHTS = [
  {
    value: "1.0",
    tailwind: "leading-none",
    usage: "Icons, single-line labels, badges",
  },
  {
    value: "1.4",
    tailwind: "leading-tight",
    usage: "Card titles, compact text",
  },
  {
    value: "1.5",
    tailwind: "leading-snug",
    usage: "Bar input (22px line-height on 15px)",
  },
  {
    value: "1.65",
    tailwind: "leading-[1.65]",
    usage: "AI answers, prose body, readable paragraphs",
  },
];

export function TypographySection() {
  return (
    <Section
      title="12. Typography"
      description="Complete type system. Every text element maps to a CSS variable + Tailwind class. 7-step scale, 3-level heading hierarchy, 4 weights."
    >
      {/* ── Native sans rationale ── */}
      <DemoCard label="Why native sans (and one mono webfont)">
        <div className="space-y-2 text-[12px] text-fg-2 leading-[1.65]">
          <p>
            <span className="text-fg-1 font-semibold">
              Sans is the OS default
            </span>{" "}
            — SF Pro on macOS/iOS, Segoe UI on Windows, Roboto on Android. No
            brand typeface, no Geist, no Inter, no Space Grotesk.{" "}
            <span className="text-fg-1 font-semibold">Geist Mono</span> is the
            only webfont we ship, for code/kbd/tokens.
          </p>
          <p>
            Hierarchy is{" "}
            <span className="text-fg-1">size × weight × tracking</span>, never
            family swap. A heading is the same typeface as a memory row — just
            bigger, bolder, tighter. Content stays louder than chrome because
            the chrome literally{" "}
            <span className="text-fg-1">disappears into the OS</span>.
          </p>
          <p className="text-fg-3">
            <span className="font-mono text-[11px]">font-heading</span> and{" "}
            <span className="font-mono text-[11px]">font-display</span> both
            alias to <span className="font-mono text-[11px]">font-sans</span>.
            Semantic intent in markup, native family at runtime. The stack is
            declared once in{" "}
            <span className="font-mono text-[11px]">globals.css :root</span> —
            never hardcode font-family.
          </p>
          <p className="text-fg-3">
            <span className="text-fg-2 font-semibold">Scar tissue:</span>{" "}
            2026-04-14 shipped{" "}
            <span className="font-mono text-[11px]">next/font/google</span>{" "}
            Geist, which injected a literal{" "}
            <span className="font-mono text-[11px]">Geist</span> into every font
            stack via its{" "}
            <span className="font-mono text-[11px]">src: local()</span>{" "}
            fallback. Designers with Geist locally installed saw their local
            Geist instead of SF Pro — thin, geometric, mismatched against every
            native element. Non-devs saw a completely different product. Never
            import Google web sans fonts into product surfaces again.
          </p>
        </div>
      </DemoCard>

      {/* ── Font Families ── */}
      <DemoCard label="Font families">
        <div className="space-y-5">
          {FONTS.map((f) => (
            <div key={f.name}>
              <div className="flex items-baseline gap-3 mb-1.5">
                <span className="text-[13px] text-fg-2 font-medium">
                  {f.name}
                </span>
                <code className="text-[11px] text-fg-3 font-mono">
                  {f.tailwind}
                </code>
              </div>
              {f.tailwind === "font-heading" ? (
                <div className="space-y-1.5 mb-1">
                  <div className="flex flex-wrap items-center gap-3">
                    <p className="text-[18px] text-fg-1 font-sans">
                      {f.sample}
                    </p>
                    <span className="text-[10px] text-fg-4">
                      normal tracking
                    </span>
                  </div>
                  <div className="flex flex-wrap items-center gap-3">
                    <p className="text-display-sm text-fg-1 font-heading">
                      {f.sample}
                    </p>
                    <span className="text-[10px] text-fg-4">
                      display tracking
                    </span>
                  </div>
                </div>
              ) : (
                <p className={`text-[18px] text-fg-1 ${f.tailwind} mb-1`}>
                  {f.sample}
                </p>
              )}
              <p className="text-[11px] text-fg-4">{f.desc}</p>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── CJK Fallback Sanity ── */}
      <DemoCard label="CJK fallback sanity">
        <div className="space-y-2">
          <p className="text-[18px] text-fg-1 font-sans">
            小宝记忆空间 remember what your agents learn
          </p>
          <p className="text-display-sm text-fg-1 font-heading">
            小宝记忆空间 remember what your agents learn
          </p>
          <p className="text-[11px] text-fg-4">
            Latin glyphs should stay in Geist. CJK falls back to system
            PingFang/YaHei/Noto without changing hierarchy.
          </p>
        </div>
      </DemoCard>

      {/* ── Heading Hierarchy ── */}
      <DemoCard label="Heading hierarchy (3 levels)">
        <div className="space-y-4">
          {HEADINGS.map((h) => (
            <div key={h.level} className="flex items-start gap-4">
              <div className="w-10 shrink-0">
                <span className="text-[11px] font-mono text-fg-3 bg-surface-2 px-1.5 py-0.5 rounded">
                  {h.level}
                </span>
              </div>
              <div className="flex-1 min-w-0">
                <p className={h.tailwind + " text-foreground mb-0.5"}>
                  {h.sample}
                </p>
                <div className="flex items-center gap-2 flex-wrap">
                  <code className="text-[10px] text-fg-3 font-mono bg-surface-1 px-1.5 py-0.5 rounded">
                    {h.tailwind}
                  </code>
                  <span className="text-[10px] text-fg-4">
                    {h.px} · {h.weight}
                  </span>
                  <span className="text-[10px] text-fg-4">— {h.usage}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          Rule: one H1 per view. H2 for sections within a page. H3 for
          cards/list items. Never skip levels.
        </p>
      </DemoCard>

      {/* ── Type Scale (full) ── */}
      <DemoCard label="Type scale — CSS variables">
        <div className="space-y-0.5">
          {/* Header row */}
          <div className="grid grid-cols-[100px_60px_1fr_1fr] gap-2 pb-2 border-b border-border/30 mb-2">
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Token
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Size
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Tailwind
            </span>
            <span className="text-[10px] text-fg-4 font-mono uppercase">
              Usage
            </span>
          </div>
          {TYPE_SCALE.map((row) => (
            <div
              key={row.token}
              className="grid grid-cols-[100px_60px_1fr_1fr] gap-2 py-2 border-b border-border/10 items-baseline"
            >
              <code className="text-[11px] text-fg-2 font-mono">
                {row.token}
              </code>
              <span className="text-[11px] text-fg-3 font-mono">{row.px}</span>
              <code className="text-[10px] text-fg-3 font-mono">
                {row.tailwind}
              </code>
              <span className="text-[10px] text-fg-4">{row.usage}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Type Scale (live) ── */}
      <DemoCard label="Type scale — live rendering">
        <div className="space-y-3">
          {TYPE_SCALE.map((row) => (
            <div
              key={row.token + "-live"}
              className="flex items-baseline gap-4"
            >
              <span
                className="text-foreground shrink-0 min-w-0"
                style={{
                  fontSize: `var(${row.token})`,
                  fontWeight:
                    row.weight === "bold"
                      ? 700
                      : row.weight === "semibold"
                        ? 600
                        : 400,
                }}
              >
                {row.sample}
              </span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Weight Scale ── */}
      <DemoCard label="Weight scale">
        <div className="space-y-3">
          {WEIGHTS.map((w) => (
            <div key={w.value} className="flex items-baseline gap-4">
              <div className="w-20 shrink-0 flex items-center gap-2">
                <span className="text-[12px] text-fg-3 font-mono">
                  {w.value}
                </span>
                <span className="text-[11px] text-fg-4">{w.name}</span>
              </div>
              <p
                className="text-[15px] text-fg-1 flex-1 min-w-0"
                style={{ fontWeight: w.value }}
              >
                The quick brown fox jumps over the lazy dog
              </p>
              <code className="text-[10px] text-fg-4 font-mono shrink-0 hidden sm:block">
                {w.tailwind}
              </code>
            </div>
          ))}
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          Rule: never use font-light (300) or font-black (900). Product uses
          400–700 range only.
        </p>
      </DemoCard>

      {/* ── Text Color Hierarchy ── */}
      <DemoCard label="Text color hierarchy">
        <div className="space-y-3">
          <div className="flex items-center gap-4">
            <code className="text-[11px] font-mono text-fg-3 w-14 shrink-0">
              fg-1
            </code>
            <span className="text-[15px] text-fg-1">
              Primary text — titles, body, input value
            </span>
            <code className="text-[10px] font-mono text-fg-4 shrink-0 hidden sm:block">
              opacity: 0.9
            </code>
          </div>
          <div className="flex items-center gap-4">
            <code className="text-[11px] font-mono text-fg-3 w-14 shrink-0">
              fg-2
            </code>
            <span className="text-[15px] text-fg-2">
              Secondary text — descriptions, placeholders
            </span>
            <code className="text-[10px] font-mono text-fg-4 shrink-0 hidden sm:block">
              opacity: 0.65
            </code>
          </div>
          <div className="flex items-center gap-4">
            <code className="text-[11px] font-mono text-fg-3 w-14 shrink-0">
              fg-3
            </code>
            <span className="text-[15px] text-fg-3">
              Tertiary text — timestamps, meta, labels
            </span>
            <code className="text-[10px] font-mono text-fg-4 shrink-0 hidden sm:block">
              opacity: 0.4
            </code>
          </div>
          <div className="flex items-center gap-4">
            <code className="text-[11px] font-mono text-fg-3 w-14 shrink-0">
              fg-4
            </code>
            <span className="text-[15px] text-fg-4">
              Decorative — hints, divider text, annotations
            </span>
            <code className="text-[10px] font-mono text-fg-4 shrink-0 hidden sm:block">
              opacity: 0.2
            </code>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 mt-3">
          All computed from --foreground via oklch opacity. Adapts automatically
          to dark mode. Never use raw foreground/XX — always use text-fg-N
          classes.
        </p>
      </DemoCard>

      {/* ── Line Height ── */}
      <DemoCard label="Line height">
        <div className="space-y-3">
          {LINE_HEIGHTS.map((lh) => (
            <div key={lh.value} className="flex items-start gap-4">
              <div className="w-24 shrink-0 flex items-center gap-2">
                <code className="text-[11px] text-fg-3 font-mono">
                  {lh.value}
                </code>
                <code className="text-[10px] text-fg-4 font-mono">
                  {lh.tailwind}
                </code>
              </div>
              <span className="text-[12px] text-fg-3">{lh.usage}</span>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── Composition Example ── */}
      <DemoCard label="Composition — memory card">
        <div
          className="max-w-sm rounded-xl border border-border p-4 space-y-1.5"
          style={{ background: "var(--card)" }}
        >
          <div className="flex items-center gap-2">
            <span
              className="w-1.5 h-1.5 rounded-full shrink-0"
              style={{ background: "#3b82f6" }}
            />
            <span className="text-[14px] font-semibold text-fg-1">
              React Server Components
            </span>
            <span className="text-[10px] text-fg-4 ml-auto font-mono">H3</span>
          </div>
          <p className="text-[13px] text-fg-2 leading-[1.65] line-clamp-2">
            Server components render on the server and send HTML. Client
            components hydrate on the client.
          </p>
          <div className="flex items-center gap-2 pt-0.5">
            <span className="text-[12px] text-fg-3">core</span>
            <span className="text-[12px] text-fg-4">&middot;</span>
            <span className="text-[12px] text-fg-3">2h ago</span>
          </div>
        </div>
        <div className="mt-2 space-y-0.5 text-[10px] text-fg-4 font-mono">
          <p>Title: text-[14px] font-semibold text-fg-1 (H3)</p>
          <p>Body: text-[13px] text-fg-2 leading-[1.65] line-clamp-2</p>
          <p>Meta: text-[12px] text-fg-3 / text-fg-4</p>
        </div>
      </DemoCard>
    </Section>
  );
}
