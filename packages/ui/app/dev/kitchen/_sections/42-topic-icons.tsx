"use client";

// ═══════════════════════════════════════════════
// 42. Topic Icons — auto-generated identity system
//
// PROPOSAL — not yet shipped. Live preview of what topic identity
// looks like end-to-end if we move from today's Folder-monoculture
// to a 3-layer system:
//
//   1. Curated Lucide set (~80 icons) organized by semantic category
//   2. Emoji fallback for niche/playful topics
//   3. Accent color (12 tones) as the real recognition primitive
//
// Today: `topics.icon string` is free-form; LLM picks from ~5 prompt
// examples; frontend hardcodes 19 icons; everything else renders as
// Folder. Result: 80%+ of topic rows look identical.
//
// Proposed: LLM returns { icon, accent }; frontend renders tile with
// accent-tinted background + icon in accent stroke. Glance-recognition
// goes from O(n) label-reading to O(1) color lookup.
//
// Maps to (future): topic-icon.tsx, topic-card.tsx, topic-chip.tsx,
// internal/dreams/organize_llm.go, internal/dreams/restructure.go
// ═══════════════════════════════════════════════

import {
  // Work / Business
  Briefcase,
  Building2,
  Presentation,
  Handshake,
  Target,
  TrendingUp,
  Calendar,
  Clock,
  Mail,
  Phone,
  Users,
  UserPlus,
  // Code / Engineering
  Code,
  Terminal,
  GitBranch,
  Database,
  Server,
  Cloud,
  Cpu,
  Bug,
  Wrench,
  Package,
  Box,
  Workflow,
  // Knowledge / Learning
  Book,
  BookOpen,
  GraduationCap,
  Lightbulb,
  Brain,
  FlaskConical,
  Microscope,
  Compass,
  Map,
  Globe,
  Library,
  Pencil,
  // Life / Personal
  Heart,
  Home,
  Coffee,
  Utensils,
  Dumbbell,
  Plane,
  Music,
  Camera,
  Gift,
  Smile,
  Sun,
  Moon,
  // Creative / Media
  Palette,
  PenTool,
  Image as ImageIcon,
  Film,
  Mic,
  Headphones,
  Sparkles,
  Wand2,
  Layers,
  Feather,
  Scissors,
  Layout as LayoutIcon,
  // Money / Ops
  DollarSign,
  CreditCard,
  LineChart,
  PieChart,
  Receipt,
  BarChart3,
  Shield,
  Lock,
  Key,
  AlertCircle,
  Flag,
  Bookmark,
  // Status / meta for demo
  Folder,
  ArrowRight,
  type LucideIcon,
} from "lucide-react";
import { Section, DemoCard } from "../_shared";

// ─── Accent palette ─────────────────────────────────────────────────
// 12 tones across warm/cool. Signature (290° violet) is reserved for
// Memax chrome — never assigned to topics. Saturated row is the
// default surface tint; muted row is for dense lists where too much
// chroma becomes noisy. Both use oklch for perceptual uniformity
// across light/dark themes.

type AccentName =
  | "rose"
  | "amber"
  | "lime"
  | "teal"
  | "sky"
  | "indigo"
  | "coral"
  | "honey"
  | "moss"
  | "mint"
  | "plum"
  | "slate";

const ACCENTS: Record<AccentName, { stroke: string; tile: string }> = {
  rose: {
    stroke: "oklch(0.62 0.14 20)",
    tile: "oklch(0.62 0.14 20 / 0.14)",
  },
  amber: {
    stroke: "oklch(0.68 0.14 60)",
    tile: "oklch(0.68 0.14 60 / 0.14)",
  },
  lime: {
    stroke: "oklch(0.66 0.14 130)",
    tile: "oklch(0.66 0.14 130 / 0.14)",
  },
  teal: {
    stroke: "oklch(0.64 0.12 190)",
    tile: "oklch(0.64 0.12 190 / 0.14)",
  },
  sky: {
    stroke: "oklch(0.64 0.12 230)",
    tile: "oklch(0.64 0.12 230 / 0.14)",
  },
  indigo: {
    stroke: "oklch(0.58 0.14 260)",
    tile: "oklch(0.58 0.14 260 / 0.14)",
  },
  coral: {
    stroke: "oklch(0.68 0.10 30)",
    tile: "oklch(0.68 0.10 30 / 0.14)",
  },
  honey: {
    stroke: "oklch(0.72 0.08 80)",
    tile: "oklch(0.72 0.08 80 / 0.14)",
  },
  moss: {
    stroke: "oklch(0.60 0.08 140)",
    tile: "oklch(0.60 0.08 140 / 0.14)",
  },
  mint: {
    stroke: "oklch(0.70 0.08 170)",
    tile: "oklch(0.70 0.08 170 / 0.14)",
  },
  plum: {
    stroke: "oklch(0.54 0.10 320)",
    tile: "oklch(0.54 0.10 320 / 0.14)",
  },
  slate: {
    stroke: "oklch(0.58 0.02 250)",
    tile: "oklch(0.58 0.02 250 / 0.14)",
  },
};

const ACCENT_ORDER: AccentName[] = [
  "rose",
  "amber",
  "lime",
  "teal",
  "sky",
  "indigo",
  "coral",
  "honey",
  "moss",
  "mint",
  "plum",
  "slate",
];

// ─── Icon library organized by category ─────────────────────────────

interface IconEntry {
  name: string;
  Icon: LucideIcon;
}

const LIBRARY: { category: string; icons: IconEntry[] }[] = [
  {
    category: "Work / Business",
    icons: [
      { name: "briefcase", Icon: Briefcase },
      { name: "building", Icon: Building2 },
      { name: "presentation", Icon: Presentation },
      { name: "handshake", Icon: Handshake },
      { name: "target", Icon: Target },
      { name: "trending-up", Icon: TrendingUp },
      { name: "calendar", Icon: Calendar },
      { name: "clock", Icon: Clock },
      { name: "mail", Icon: Mail },
      { name: "phone", Icon: Phone },
      { name: "users", Icon: Users },
      { name: "user-plus", Icon: UserPlus },
    ],
  },
  {
    category: "Code / Engineering",
    icons: [
      { name: "code", Icon: Code },
      { name: "terminal", Icon: Terminal },
      { name: "git-branch", Icon: GitBranch },
      { name: "database", Icon: Database },
      { name: "server", Icon: Server },
      { name: "cloud", Icon: Cloud },
      { name: "cpu", Icon: Cpu },
      { name: "bug", Icon: Bug },
      { name: "wrench", Icon: Wrench },
      { name: "package", Icon: Package },
      { name: "box", Icon: Box },
      { name: "workflow", Icon: Workflow },
    ],
  },
  {
    category: "Knowledge / Learning",
    icons: [
      { name: "book", Icon: Book },
      { name: "book-open", Icon: BookOpen },
      { name: "graduation-cap", Icon: GraduationCap },
      { name: "lightbulb", Icon: Lightbulb },
      { name: "brain", Icon: Brain },
      { name: "flask", Icon: FlaskConical },
      { name: "microscope", Icon: Microscope },
      { name: "compass", Icon: Compass },
      { name: "map", Icon: Map },
      { name: "globe", Icon: Globe },
      { name: "library", Icon: Library },
      { name: "pencil", Icon: Pencil },
    ],
  },
  {
    category: "Life / Personal",
    icons: [
      { name: "heart", Icon: Heart },
      { name: "home", Icon: Home },
      { name: "coffee", Icon: Coffee },
      { name: "utensils", Icon: Utensils },
      { name: "dumbbell", Icon: Dumbbell },
      { name: "plane", Icon: Plane },
      { name: "music", Icon: Music },
      { name: "camera", Icon: Camera },
      { name: "gift", Icon: Gift },
      { name: "smile", Icon: Smile },
      { name: "sun", Icon: Sun },
      { name: "moon", Icon: Moon },
    ],
  },
  {
    category: "Creative / Media",
    icons: [
      { name: "palette", Icon: Palette },
      { name: "pen-tool", Icon: PenTool },
      { name: "image", Icon: ImageIcon },
      { name: "film", Icon: Film },
      { name: "mic", Icon: Mic },
      { name: "headphones", Icon: Headphones },
      { name: "sparkles", Icon: Sparkles },
      { name: "wand", Icon: Wand2 },
      { name: "layers", Icon: Layers },
      { name: "feather", Icon: Feather },
      { name: "scissors", Icon: Scissors },
      { name: "layout", Icon: LayoutIcon },
    ],
  },
  {
    category: "Money / Ops / Security",
    icons: [
      { name: "dollar", Icon: DollarSign },
      { name: "credit-card", Icon: CreditCard },
      { name: "line-chart", Icon: LineChart },
      { name: "pie-chart", Icon: PieChart },
      { name: "receipt", Icon: Receipt },
      { name: "bar-chart", Icon: BarChart3 },
      { name: "shield", Icon: Shield },
      { name: "lock", Icon: Lock },
      { name: "key", Icon: Key },
      { name: "alert", Icon: AlertCircle },
      { name: "flag", Icon: Flag },
      { name: "bookmark", Icon: Bookmark },
    ],
  },
];

// ─── Tile primitive — the proposed topic identity ───────────────────

function TopicTile({
  Icon,
  accent,
  size = 32,
  emoji,
}: {
  Icon?: LucideIcon;
  accent: AccentName;
  size?: number;
  emoji?: string;
}) {
  const { stroke, tile } = ACCENTS[accent];
  const iconPx = Math.round(size * 0.52);
  return (
    <div
      className="flex items-center justify-center rounded-[8px] shrink-0"
      style={{
        width: size,
        height: size,
        background: tile,
        boxShadow: `inset 0 0 0 1px ${stroke.replace("/ 1", "/ 0.18")}`,
      }}
    >
      {emoji ? (
        <span style={{ fontSize: Math.round(size * 0.58), lineHeight: 1 }}>
          {emoji}
        </span>
      ) : Icon ? (
        <Icon
          style={{ width: iconPx, height: iconPx, color: stroke }}
          strokeWidth={2}
        />
      ) : null}
    </div>
  );
}

// ─── Section component ─────────────────────────────────────────────

export function TopicIconsSection() {
  return (
    <Section
      title="42. Topic Icons"
      description="Proposal — how auto-generated topic identity would look end-to-end. Today every topic renders as Folder; this preview shows the full system live so you can decide whether to greenlight."
    >
      {/* ── Before / After ─────────────────────────────────── */}
      <DemoCard label="Before → After (glance test)">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {/* Before */}
          <div>
            <div className="text-[11px] text-fg-4 uppercase tracking-wider mb-2">
              Today — Folder monoculture
            </div>
            <div className="rounded-xl border border-border/60 overflow-hidden">
              {TOPIC_SAMPLES.map((s, i) => (
                <div
                  key={s.label}
                  className={`flex items-center gap-3 px-3 py-2.5 ${
                    i > 0 ? "border-t border-border/30" : ""
                  }`}
                >
                  <Folder className="h-4 w-4 text-fg-3 shrink-0" />
                  <span className="text-[13px] text-fg-2 truncate">
                    {s.label}
                  </span>
                </div>
              ))}
            </div>
            <p className="text-[11px] text-fg-4 mt-2">
              Scan cost: O(n) — every label must be read
            </p>
          </div>

          {/* After */}
          <div>
            <div className="text-[11px] text-fg-4 uppercase tracking-wider mb-2">
              Proposed — icon + accent tile
            </div>
            <div className="rounded-xl border border-border/60 overflow-hidden">
              {TOPIC_SAMPLES.map((s, i) => (
                <div
                  key={s.label}
                  className={`flex items-center gap-3 px-3 py-2.5 ${
                    i > 0 ? "border-t border-border/30" : ""
                  }`}
                >
                  <TopicTile Icon={s.Icon} accent={s.accent} size={28} />
                  <span className="text-[13px] text-fg-2 truncate">
                    {s.label}
                  </span>
                </div>
              ))}
            </div>
            <p className="text-[11px] text-fg-4 mt-2">
              Scan cost: O(1) — color lookup before label is read
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── Accent palette ─────────────────────────────────── */}
      <DemoCard label="Accent palette — 12 tones">
        <div className="grid grid-cols-6 md:grid-cols-12 gap-2">
          {ACCENT_ORDER.map((name) => (
            <div key={name} className="flex flex-col items-center gap-1.5">
              <div
                className="w-full aspect-square rounded-lg"
                style={{
                  background: ACCENTS[name].tile,
                  boxShadow: `inset 0 0 0 1px ${ACCENTS[name].stroke.replace("/ 1", "/ 0.2")}`,
                }}
              />
              <span className="text-[10px] text-fg-3">{name}</span>
            </div>
          ))}
        </div>
        <p className="text-[11px] text-fg-4 mt-3">
          Signature violet (290°) is reserved for Memax chrome — never assigned
          to topics. Tile = 14% chroma mixed into background; stroke = full
          accent at the icon.
        </p>
      </DemoCard>

      {/* ── Chip variants at different sizes ───────────────── */}
      <DemoCard label="Same identity, three contexts">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* Inline list row */}
          <div>
            <div className="text-[11px] text-fg-4 uppercase tracking-wider mb-2">
              List row — 28px
            </div>
            <div className="rounded-xl border border-border/60 divide-y divide-border/30">
              <ChipRow Icon={Shield} accent="indigo" label="Auth refactor" />
              <ChipRow
                Icon={Database}
                accent="sky"
                label="Postgres deep dive"
              />
              <ChipRow Icon={Palette} accent="rose" label="Design language" />
            </div>
          </div>

          {/* Grid card */}
          <div>
            <div className="text-[11px] text-fg-4 uppercase tracking-wider mb-2">
              Grid card — 40px
            </div>
            <div className="grid grid-cols-2 gap-2">
              <GridCard
                Icon={Target}
                accent="teal"
                label="Q2 roadmap"
                count={14}
              />
              <GridCard
                Icon={Utensils}
                accent="amber"
                label="Recipe ideas"
                count={9}
              />
            </div>
          </div>

          {/* Detail header */}
          <div>
            <div className="text-[11px] text-fg-4 uppercase tracking-wider mb-2">
              Detail header — 56px
            </div>
            <div
              className="rounded-xl border border-border/60 p-4"
              style={{ background: "var(--background)" }}
            >
              <div className="flex items-center gap-3">
                <TopicTile Icon={Sparkles} accent="plum" size={56} />
                <div className="min-w-0">
                  <div className="text-[18px] font-semibold text-fg-1 tracking-tight truncate">
                    Dream engine notes
                  </div>
                  <div className="text-[12px] text-fg-3">
                    23 memories · synthesised last night
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── Density test ─────────────────────────────────── */}
      <DemoCard label="Density test — 24 topics at a glance">
        <div className="grid grid-cols-2 md:grid-cols-3 gap-2">
          {DENSE_SAMPLES.map((s) => (
            <div
              key={s.label}
              className="flex items-center gap-2.5 px-2.5 py-2 rounded-lg"
              style={{ background: "var(--card)" }}
            >
              <TopicTile Icon={s.Icon} accent={s.accent} size={24} />
              <span className="text-[12px] text-fg-2 truncate">{s.label}</span>
            </div>
          ))}
        </div>
        <p className="text-[11px] text-fg-4 mt-3">
          You can find the blue ones (code), green ones (health/money), amber
          ones (food/triage) without reading a single label. That's the whole
          point.
        </p>
      </DemoCard>

      {/* ── Curated icon library ─────────────────────────────── */}
      <DemoCard label="Curated library — 72 icons across 6 categories">
        <div className="space-y-5">
          {LIBRARY.map((group) => (
            <div key={group.category}>
              <div className="text-[11px] text-fg-3 uppercase tracking-wider mb-2">
                {group.category}
              </div>
              <div className="grid grid-cols-6 md:grid-cols-12 gap-2">
                {group.icons.map((entry) => (
                  <div
                    key={entry.name}
                    className="flex flex-col items-center gap-1"
                    title={entry.name}
                  >
                    <div
                      className="w-9 h-9 rounded-md flex items-center justify-center"
                      style={{
                        background: "var(--card)",
                        boxShadow: "inset 0 0 0 1px var(--border)",
                      }}
                    >
                      <entry.Icon className="w-4 h-4 text-fg-2" />
                    </div>
                    <span
                      className="text-[9px] text-fg-4 truncate w-full text-center"
                      title={entry.name}
                    >
                      {entry.name}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
        <p className="text-[11px] text-fg-4 mt-3">
          LLM receives category list + icon names in its prompt, routes by
          semantic match. Unknown returns fall back to Folder or emoji.
        </p>
      </DemoCard>

      {/* ── Emoji fallback ─────────────────────────────────── */}
      <DemoCard label="Emoji fallback — when no curated icon fits">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {EMOJI_SAMPLES.map((s) => (
            <div
              key={s.label}
              className="flex items-center gap-3 px-3 py-2.5 rounded-xl"
              style={{ background: "var(--card)" }}
            >
              <TopicTile emoji={s.emoji} accent={s.accent} size={32} />
              <span className="text-[13px] text-fg-1">{s.label}</span>
              <code className="ml-auto text-[10px] text-fg-4">
                {`{ icon: "${s.emoji}", accent: "${s.accent}" }`}
              </code>
            </div>
          ))}
        </div>
        <p className="text-[11px] text-fg-4 mt-3">
          Frontend detection:{" "}
          <code className="text-fg-3">icon.length ≤ 4 && !ICON_MAP[icon]</code>{" "}
          → render as emoji span inside the same tile chrome. Keeps niche topics
          playful without exploding the curated set.
        </p>
      </DemoCard>

      {/* ── LLM output examples ─────────────────────────────── */}
      <DemoCard label="LLM routing — topic name → { icon, accent }">
        <div className="rounded-xl overflow-hidden border border-border/60">
          <table className="w-full text-[12px]">
            <thead>
              <tr
                className="text-left text-fg-3 text-[11px] uppercase tracking-wider"
                style={{ background: "var(--card)" }}
              >
                <th className="px-3 py-2 font-medium">Topic name</th>
                <th className="px-3 py-2 font-medium w-16">Preview</th>
                <th className="px-3 py-2 font-medium">LLM output</th>
              </tr>
            </thead>
            <tbody>
              {LLM_EXAMPLES.map((ex, i) => (
                <tr
                  key={ex.label}
                  className={i > 0 ? "border-t border-border/30" : ""}
                >
                  <td className="px-3 py-2 text-fg-1">{ex.label}</td>
                  <td className="px-3 py-2">
                    <TopicTile
                      Icon={ex.Icon}
                      emoji={ex.emoji}
                      accent={ex.accent}
                      size={28}
                    />
                  </td>
                  <td className="px-3 py-2">
                    <code className="text-[11px] text-fg-3">
                      {`{ icon: "${ex.iconName}", accent: "${ex.accent}" }`}
                    </code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </DemoCard>

      {/* ── End-state topic explore ─────────────────────────── */}
      <DemoCard label="End state — Topic Explore with full identity">
        <div className="rounded-xl border border-border/60 overflow-hidden">
          <div
            className="px-4 py-3 border-b border-border/30"
            style={{ background: "var(--card)" }}
          >
            <div className="text-[10px] uppercase tracking-[0.12em] text-fg-3">
              Topics
            </div>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 divide-y md:divide-y-0 md:divide-x divide-border/30">
            {/* Left: dense list */}
            <div>
              {EXPLORE_SAMPLES.map((s, i) => (
                <div
                  key={s.label}
                  className={`flex items-center gap-3 px-4 py-3 ${
                    i > 0 ? "border-t border-border/30" : ""
                  }`}
                >
                  <TopicTile Icon={s.Icon} accent={s.accent} size={32} />
                  <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-medium text-fg-1 truncate">
                      {s.label}
                    </div>
                    <div className="text-[11px] text-fg-4">
                      {s.count} memories · {s.age}
                    </div>
                  </div>
                  <ArrowRight className="h-3.5 w-3.5 text-fg-4 shrink-0" />
                </div>
              ))}
            </div>
            {/* Right: ambient cloud */}
            <div className="p-4 flex flex-wrap gap-2 content-start">
              {DENSE_SAMPLES.slice(0, 16).map((s) => (
                <div
                  key={`cloud-${s.label}`}
                  className="flex items-center gap-1.5 pl-1 pr-2.5 py-1 rounded-full"
                  style={{
                    background: ACCENTS[s.accent].tile,
                    boxShadow: `inset 0 0 0 1px ${ACCENTS[s.accent].stroke.replace("/ 1", "/ 0.16")}`,
                  }}
                >
                  <TopicTile Icon={s.Icon} accent={s.accent} size={18} />
                  <span
                    className="text-[11px]"
                    style={{ color: ACCENTS[s.accent].stroke }}
                  >
                    {s.label}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
        <p className="text-[11px] text-fg-4 mt-3">
          Left: dense list variant with 32px tiles. Right: topic cloud showing
          how accents alone carry category identity even without reading labels
          — blue cluster = code, amber cluster = food / triage, moss = money /
          ops.
        </p>
      </DemoCard>
    </Section>
  );
}

// ─── Sub-components ─────────────────────────────────────────────────

function ChipRow({
  Icon,
  accent,
  label,
}: {
  Icon: LucideIcon;
  accent: AccentName;
  label: string;
}) {
  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      <TopicTile Icon={Icon} accent={accent} size={28} />
      <span className="text-[13px] text-fg-2 truncate">{label}</span>
    </div>
  );
}

function GridCard({
  Icon,
  accent,
  label,
  count,
}: {
  Icon: LucideIcon;
  accent: AccentName;
  label: string;
  count: number;
}) {
  return (
    <div
      className="rounded-xl p-3 flex flex-col gap-2"
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
      }}
    >
      <TopicTile Icon={Icon} accent={accent} size={40} />
      <div className="min-w-0">
        <div className="text-[12px] font-medium text-fg-1 truncate">
          {label}
        </div>
        <div className="text-[10px] text-fg-4">{count} memories</div>
      </div>
    </div>
  );
}

// ─── Sample data ─────────────────────────────────────────────────

const TOPIC_SAMPLES: {
  label: string;
  Icon: LucideIcon;
  accent: AccentName;
}[] = [
  { label: "Caching Strategy", Icon: Cpu, accent: "sky" },
  { label: "Recipe ideas", Icon: Utensils, accent: "amber" },
  { label: "Investor calls", Icon: Handshake, accent: "moss" },
  { label: "Spanish vocab", Icon: BookOpen, accent: "rose" },
  { label: "Q2 roadmap", Icon: Target, accent: "teal" },
  { label: "Auth refactor", Icon: Shield, accent: "indigo" },
  { label: "Weekend trips", Icon: Plane, accent: "coral" },
];

const DENSE_SAMPLES: {
  label: string;
  Icon: LucideIcon;
  accent: AccentName;
}[] = [
  { label: "Caching Strategy", Icon: Cpu, accent: "sky" },
  { label: "Postgres tuning", Icon: Database, accent: "sky" },
  { label: "Git workflows", Icon: GitBranch, accent: "sky" },
  { label: "Auth refactor", Icon: Shield, accent: "indigo" },
  { label: "Bugs to triage", Icon: Bug, accent: "amber" },
  { label: "Recipe ideas", Icon: Utensils, accent: "amber" },
  { label: "Coffee notes", Icon: Coffee, accent: "honey" },
  { label: "Morning routines", Icon: Sun, accent: "honey" },
  { label: "Q2 roadmap", Icon: Target, accent: "teal" },
  { label: "Design language", Icon: Palette, accent: "rose" },
  { label: "Spanish vocab", Icon: BookOpen, accent: "rose" },
  { label: "Kids' activities", Icon: Smile, accent: "rose" },
  { label: "Investor calls", Icon: Handshake, accent: "moss" },
  { label: "Expenses", Icon: Receipt, accent: "moss" },
  { label: "Climbing goals", Icon: Dumbbell, accent: "mint" },
  { label: "Running log", Icon: Heart, accent: "mint" },
  { label: "Weekend trips", Icon: Plane, accent: "coral" },
  { label: "Photography", Icon: Camera, accent: "coral" },
  { label: "Dream engine", Icon: Sparkles, accent: "plum" },
  { label: "Music discovery", Icon: Music, accent: "plum" },
  { label: "Research notes", Icon: FlaskConical, accent: "lime" },
  { label: "Reading list", Icon: Book, accent: "lime" },
  { label: "Meetings", Icon: Calendar, accent: "slate" },
  { label: "Inbox zero", Icon: Mail, accent: "slate" },
];

const EMOJI_SAMPLES: {
  label: string;
  emoji: string;
  accent: AccentName;
}[] = [
  { label: "Ramen spots in Tokyo", emoji: "🍜", accent: "coral" },
  { label: "Dog training", emoji: "🐕", accent: "honey" },
  { label: "Mars colonization", emoji: "🚀", accent: "indigo" },
  { label: "Tarot readings", emoji: "🔮", accent: "plum" },
];

const LLM_EXAMPLES: {
  label: string;
  Icon?: LucideIcon;
  emoji?: string;
  iconName: string;
  accent: AccentName;
}[] = [
  {
    label: "Caching Strategy",
    Icon: Cpu,
    iconName: "cpu",
    accent: "sky",
  },
  {
    label: "Recipe ideas",
    Icon: Utensils,
    iconName: "utensils",
    accent: "amber",
  },
  {
    label: "Investor calls",
    Icon: Handshake,
    iconName: "handshake",
    accent: "moss",
  },
  {
    label: "Auth refactor",
    Icon: Shield,
    iconName: "shield",
    accent: "indigo",
  },
  {
    label: "Ramen spots in Tokyo",
    emoji: "🍜",
    iconName: "🍜",
    accent: "coral",
  },
  {
    label: "Dream engine notes",
    Icon: Sparkles,
    iconName: "sparkles",
    accent: "plum",
  },
];

const EXPLORE_SAMPLES: {
  label: string;
  Icon: LucideIcon;
  accent: AccentName;
  count: number;
  age: string;
}[] = [
  {
    label: "Caching Strategy",
    Icon: Cpu,
    accent: "sky",
    count: 18,
    age: "2h ago",
  },
  {
    label: "Recipe ideas",
    Icon: Utensils,
    accent: "amber",
    count: 9,
    age: "yesterday",
  },
  {
    label: "Q2 roadmap",
    Icon: Target,
    accent: "teal",
    count: 14,
    age: "3d ago",
  },
  {
    label: "Dream engine notes",
    Icon: Sparkles,
    accent: "plum",
    count: 23,
    age: "last night",
  },
  {
    label: "Investor calls",
    Icon: Handshake,
    accent: "moss",
    count: 6,
    age: "1w ago",
  },
];
