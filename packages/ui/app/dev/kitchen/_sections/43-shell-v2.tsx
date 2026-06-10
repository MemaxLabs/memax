// Plan 19+ shell-v2 architecture: 3-pane desktop, drawer + push-stack mobile.
//
// Design tokens reused (NOT reinvented):
//   - <MemaxLogo /> from @memaxlabs/ui for the brand mark (NEUTRAL, painted in
//     text-foreground per kitchen 11-branding.tsx canonical rule).
//   - --signature is SCARCE: hub avatar fill (◐), active tab indicator,
//     primary CTA, and the system_notice accent. NOT used for hover, halos,
//     decorations, or the wordmark — that collapses hierarchy ("the violet
//     thing" anti-pattern, kitchen 11).
//   - --glass-start/--glass-end/--glass-border/--glass-shadow for surfaces
//     that should feel layered (left rail, secondary panel, floating widgets).
//   - bg-surface-2 / bg-surface-3 / text-fg-2 / text-fg-3 / text-fg-4 are the
//     canonical neutral tokens (calibrated foreground at op-bg-* opacities).
//
// Refero: Slab #081669c7 + Metaview #ecfd62da (3-column productivity), Linear
// apps/204 (left-rail nav), Manus #e7ebc0ff (card grid + file-type icons),
// Notion apps/64 (mobile drawer + push transitions).
"use client";

import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
  Brain,
  Bot,
  Inbox as InboxIcon,
  Library,
  Search,
  Plus,
  ChevronRight,
  ChevronLeft,
  ChevronDown,
  ChevronsLeft,
  ChevronsRight,
  Layout,
  Check,
  FileText,
  Link as LinkIcon,
  Image as ImageIcon,
  FilePlus,
  X,
  Menu,
  MoreHorizontal,
  ArrowRight,
  User,
  Settings,
  Quote,
} from "lucide-react";
import {
  NORMAL,
  EASE,
  GLASS_ENTER_DURATION,
  GLASS_ENTER_EASE,
  GLASS_EXIT_DURATION,
  GLASS_EXIT_EASE,
} from "@memaxlabs/ui/tokens/motion";
import {
  MemaxLogo,
  MemaxWordmark,
  TopicIcon,
  getHubAccentStyle,
  getHubHeaderTimeBucket,
  type HubHeaderTimeBucket,
} from "@memaxlabs/ui";

// Aurora gradients lifted from packages/ui/src/components/hub-header-banner.tsx:22-34
// to repurpose the existing per-hub time-based aurora system at the page-background
// layer (in production today, aurora only renders inside HubHeaderBanner). Plan 19
// will wire this through a real CSS variable so a hub-level mode switch can
// override it (e.g. "calm" mode disables page aurora).
const AURORA_GRADIENTS: Record<HubHeaderTimeBucket, string> = {
  "deep-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.55 0.15 260 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.15 290 / 0.14), transparent 70%)",
  morning:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.82 0.14 75 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.78 0.14 35 / 0.16), transparent 70%)",
  noon: "radial-gradient(80% 140% at 25% 20%, oklch(0.84 0.12 75 / 0.2), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.86 0.1 45 / 0.14), transparent 70%)",
  afternoon:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.78 0.14 55 / 0.2), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.72 0.13 25 / 0.14), transparent 70%)",
  evening:
    "radial-gradient(80% 140% at 25% 20%, oklch(0.68 0.16 25 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.6 0.16 340 / 0.16), transparent 70%)",
  "late-night":
    "radial-gradient(80% 140% at 25% 20%, oklch(0.5 0.14 285 / 0.22), transparent 65%), radial-gradient(60% 100% at 80% 0%, oklch(0.55 0.14 265 / 0.16), transparent 70%)",
};
import { Section, DemoCard } from "../_shared";

// Tiny stand-in for production HubBadge (packages/web/src/components/features/hub-badge.tsx).
// Kitchen can't import from packages/web; this mirrors the production rounded-md
// avatar with getHubAccentStyle ("hubs are containers — rounded square not circle").
function HubAvatar({
  label,
  accent,
  kind = "personal",
  size = "md",
}: {
  label: string;
  accent?: string;
  kind?: "personal" | "team";
  size?: "sm" | "md" | "lg";
}) {
  const style = getHubAccentStyle(accent, kind);
  const dim =
    size === "lg"
      ? "h-9 w-9 text-[16px]"
      : size === "sm"
        ? "h-4 w-4 text-[9px]"
        : "h-5 w-5 text-[11px]";
  return (
    <div
      className={`flex shrink-0 items-center justify-center rounded-md font-medium ${dim}`}
      style={{ background: style.background, color: style.foreground }}
    >
      {label.trim().slice(0, 1) || "?"}
    </div>
  );
}

/* ═══════════════════════════════════════════════
   Tab + data constants
   ═══════════════════════════════════════════════ */

type ShellTab = "brain" | "memories" | "agents" | "inbox";

const SHELL_TABS: {
  id: ShellTab;
  icon: typeof Brain;
  label: string;
  badge?: number;
  hasSecondaryPanel: boolean;
}[] = [
  { id: "brain", icon: Brain, label: "Brain", hasSecondaryPanel: false },
  { id: "memories", icon: Library, label: "Memories", hasSecondaryPanel: true },
  { id: "agents", icon: Bot, label: "Agents", hasSecondaryPanel: false },
  {
    id: "inbox",
    icon: InboxIcon,
    label: "Inbox",
    badge: 2,
    hasSecondaryPanel: false,
  },
];

/* Seed onboarding memories. Admin-configurable in production (plan 23).
   Each is simultaneously: a beautiful example of memax content,
   a tutorial by theme, and askable. Borges anchors brand voice. */
// Mirror production memory-row.tsx attribution tiers:
//   1. Agent capture (source_agent) → Bot icon in identity color + name
//   2. Team human (author_name)     → Avatar (or initial-letter)  + name
//   3. Personal default              → kind dot + content type
//
// Seed memories show all three tiers for kitchen demo fidelity. In real
// production, seed memories are all system-authored; the agent + user
// examples here demonstrate the spectrum.
type SeedMemory = {
  id: string;
  fileType: "doc" | "link" | "quote" | "image";
  title: string;
  preview: string;
  actor:
    | { kind: "system"; display: string }
    | {
        kind: "agent";
        agentName: string;
        display: string;
        color: string;
      }
    | {
        kind: "user";
        display: string;
        initial: string;
      };
  kind?: "rationale" | "semantic" | "episodic" | "procedural"; // memax classification
  age: string;
  topic?: string;
  hub?: string;
  isQuote?: boolean;
};

const SEED_MEMORIES: SeedMemory[] = [
  {
    id: "memory-as-mirror",
    fileType: "quote",
    title: "Memory as Mirror",
    preview:
      '"We are our memory, we are that chimerical museum of shifting shapes, that pile of broken mirrors." — Jorge Luis Borges',
    actor: { kind: "system", display: "memax" },
    kind: "rationale",
    age: "seed",
    topic: "Welcome",
    isQuote: true,
  },
  {
    id: "claude-pr-stack",
    fileType: "doc",
    title: "Hatch Jarvis PR stack",
    preview:
      "Foundation: PR #3370 (p1, metering crate) → PR #3374 (p2, credit-watcher daemon). Gating PRs stacked on foundation: PR #3126, PR #3127, PR #3128.",
    actor: {
      kind: "agent",
      agentName: "claude-code",
      display: "claude-code",
      color: "oklch(0.65 0.12 30)", // warm orange identity
    },
    kind: "procedural",
    age: "2h ago",
    topic: "Tutorial",
    hub: "Personal",
  },
  {
    id: "ziyang-design-note",
    fileType: "doc",
    title: "Topic explorer design north star",
    preview:
      "Floating glass panel with 12px insets, 296px width, GLASS_ENTER animation. Containers are chrome; brand color goes on affordances inside.",
    actor: { kind: "user", display: "Ziyang", initial: "Z" },
    kind: "rationale",
    age: "yesterday",
    topic: "Tutorial",
    hub: "Engineering",
  },
  {
    id: "how-recall-works",
    fileType: "doc",
    title: "How recall works",
    preview:
      "Type a question, hit ↵. memax pulls from everything you've dumped — across hubs, agents, file types — ranked by semantic match plus recency.",
    actor: { kind: "system", display: "memax" },
    kind: "semantic",
    age: "seed",
    topic: "Tutorial",
  },
  {
    id: "what-dreams-do",
    fileType: "doc",
    title: "What dreams do",
    preview:
      "Every night memax connects the dots — surfaces contradictions, merges duplicates, organizes loose memories into topics. You wake up to a tidier brain.",
    actor: { kind: "system", display: "memax" },
    kind: "semantic",
    age: "seed",
    topic: "Welcome",
  },
  {
    id: "connect-claude",
    fileType: "link",
    title: "Connect Claude Code in 30s",
    preview:
      "npx memax-cli setup --agent claude-code. One command syncs your settings + memories. Memory flows both ways.",
    actor: { kind: "system", display: "memax" },
    kind: "procedural",
    age: "seed",
    topic: "Tutorial",
  },
];

type TopicNode = {
  id: string;
  name: string;
  count: number;
  icon: string;
  children?: TopicNode[];
};

// Icon names match the production TopicIcon ICON_MAP keys
// (packages/ui/src/components/topic-icon.tsx). Topics carry lucide
// icon NAMES (string), not glyphs — TopicIcon resolves the name to
// a component, with a Folder fallback for unknown names.
const TOPICS_DEMO: TopicNode[] = [
  {
    id: "welcome",
    name: "Welcome",
    count: 4,
    icon: "star",
    children: [
      { id: "welcome-quotes", name: "Quotes", count: 1, icon: "book" },
    ],
  },
  {
    id: "tutorial",
    name: "Tutorial",
    count: 3,
    icon: "lightbulb",
    children: [
      { id: "tutorial-recall", name: "Recall", count: 1, icon: "globe" },
      { id: "tutorial-dreams", name: "Dreams", count: 1, icon: "zap" },
    ],
  },
];

/* Tree node mirroring production TopicTreeNode (depth indent + rotating chevron). */
function DemoTopicNode({
  node,
  depth,
  expandedIds,
  activeId,
  onToggleExpand,
  onSelect,
}: {
  node: TopicNode;
  depth: number;
  expandedIds: Set<string>;
  activeId?: string;
  onToggleExpand: (id: string) => void;
  onSelect: (id: string) => void;
}) {
  const hasChildren = (node.children?.length ?? 0) > 0;
  const isExpanded = expandedIds.has(node.id);
  const isActive = activeId === node.id;
  return (
    <div>
      <div
        className={`flex items-center gap-1 py-1.5 pr-2 rounded-md cursor-pointer transition-colors ${
          isActive ? "bg-foreground/[0.06]" : "hover:bg-foreground/[0.03]"
        }`}
        style={{ paddingLeft: `${8 + depth * 16}px` }}
        onClick={() => onSelect(node.id)}
      >
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            if (hasChildren) onToggleExpand(node.id);
          }}
          className={`p-0.5 rounded shrink-0 transition-transform ${
            hasChildren
              ? "text-fg-4 hover:text-fg-3"
              : "text-transparent pointer-events-none"
          } ${isExpanded ? "rotate-90" : ""}`}
          aria-label={isExpanded ? "Collapse" : "Expand"}
        >
          <ChevronRight className="h-3 w-3" />
        </button>
        <TopicIcon
          name={node.icon}
          className="h-3.5 w-3.5 text-fg-3 shrink-0"
        />
        <span
          className={`text-[12px] truncate flex-1 ${
            isActive ? "text-foreground font-medium" : "text-fg-2"
          }`}
        >
          {node.name}
        </span>
        <span className="text-[10px] tabular-nums text-fg-4 shrink-0">
          {node.count}
        </span>
      </div>
      {isExpanded &&
        hasChildren &&
        node.children!.map((c) => (
          <DemoTopicNode
            key={c.id}
            node={c}
            depth={depth + 1}
            expandedIds={expandedIds}
            activeId={activeId}
            onToggleExpand={onToggleExpand}
            onSelect={onSelect}
          />
        ))}
    </div>
  );
}

const HUBS_DEMO: {
  id: string;
  name: string;
  kind: "personal" | "team";
  accent?: string;
  active: boolean;
}[] = [
  { id: "personal", name: "Personal hub", kind: "personal", active: true },
  {
    id: "engineering",
    name: "Engineering",
    kind: "team",
    accent: "blue",
    active: false,
  },
  {
    id: "design",
    name: "Design craft",
    kind: "team",
    accent: "rose",
    active: false,
  },
];

/* ═══════════════════════════════════════════════
   Shared glass surface helper. Matches kitchen 14-surfaces
   liquid-glass + the bar-shadow recipe.
   ═══════════════════════════════════════════════ */

const glassSurfaceStyle: React.CSSProperties = {
  background:
    "linear-gradient(to bottom, var(--glass-start), var(--glass-end))",
  borderRight: "1px solid var(--glass-border)",
  backdropFilter: "blur(24px) saturate(140%)",
  WebkitBackdropFilter: "blur(24px) saturate(140%)",
};

/* ═══════════════════════════════════════════════
   Left rail (collapsible)
   Refero: Linear apps/204 — bottom-fixed user, top brand mark, vertical nav.
   Brand bubble is NEUTRAL (kitchen 11 canonical rule); signature is reserved
   for the active tab indicator only.
   ═══════════════════════════════════════════════ */

function ShellLeftRail({
  active,
  onChange,
  collapsed,
  onToggleCollapse,
  width,
}: {
  active: ShellTab;
  onChange: (t: ShellTab) => void;
  collapsed: boolean;
  onToggleCollapse: () => void;
  width: number;
}) {
  return (
    <motion.div
      className="flex flex-col"
      animate={{ width }}
      transition={{ duration: NORMAL, ease: EASE }}
      style={{
        // Floating glass-panel recipe — same material as the tree panel
        // so they read as one family. Production .glass-panel from
        // packages/web/src/app/globals.css.
        // Z-index 3 — sits ABOVE the secondary panel so it appears the
        // secondary slides out from underneath the rail.
        position: "absolute",
        top: TREE_PANEL_INSET,
        left: TREE_PANEL_INSET,
        bottom: TREE_PANEL_INSET,
        zIndex: 3,
        background: "color-mix(in srgb, var(--card) 72%, transparent)",
        backdropFilter: "blur(36px) saturate(180%) contrast(1.05)",
        WebkitBackdropFilter: "blur(36px) saturate(180%) contrast(1.05)",
        border: "1px solid oklch(0 0 0 / 0.05)",
        borderRadius: "var(--app-radius-surface, 20px)",
        boxShadow:
          "inset 0 1px 0 oklch(1 0 0 / 0.5), 0 20px 40px -10px oklch(0 0 0 / 0.08), 0 6px 16px -4px oklch(0 0 0 / 0.06), 0 2px 6px -2px oklch(0 0 0 / 0.04)",
      }}
    >
      {/* Top: brand mark IS the collapse trigger.
          Default: MemaxLogo. Hover: ChevronsLeft / ChevronsRight cross-fade.
          Mirrors production BrandMark in topic-tree-panel.tsx where the
          brand mark IS the panel opener — no separate collapse button. */}
      <div className="flex items-center gap-2 px-3 h-12 shrink-0">
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          className="group relative w-7 h-7 rounded-lg flex items-center justify-center shrink-0 text-foreground bg-surface-2 hover:bg-surface-3 transition-colors"
        >
          <MemaxLogo
            size={16}
            className="absolute opacity-100 group-hover:opacity-0 transition-opacity duration-150"
          />
          {collapsed ? (
            <ChevronsRight
              className="absolute w-3.5 h-3.5 opacity-0 group-hover:opacity-100 transition-opacity duration-150"
              strokeWidth={2.2}
            />
          ) : (
            <ChevronsLeft
              className="absolute w-3.5 h-3.5 opacity-0 group-hover:opacity-100 transition-opacity duration-150"
              strokeWidth={2.2}
            />
          )}
        </button>
        {!collapsed && (
          // Wordmark text glyph ONLY — the SVG also contains the icon, so
          // we crop the leading icon portion via overflow-hidden + negative
          // margin. The toggle button on the left already shows the icon
          // (with hover→chevron). Production fix: add a MemaxTextLogo
          // component to @memaxlabs/ui that renders only the text path.
          <div
            className="overflow-hidden shrink-0"
            style={{ width: 56, height: 16 }}
          >
            <MemaxWordmark
              height={16}
              className="text-foreground"
              style={{ marginLeft: -20 }}
            />
          </div>
        )}
      </div>

      {/* Tabs — only active state uses signature */}
      <nav className="flex flex-col gap-0.5 px-2 mt-2">
        {SHELL_TABS.map((tab) => {
          const Icon = tab.icon;
          const isActive = active === tab.id;
          return (
            <button
              key={tab.id}
              type="button"
              onClick={() => onChange(tab.id)}
              className="flex items-center gap-2.5 h-8 px-2 rounded-lg transition-colors"
              style={{
                background: isActive ? "var(--surface-3)" : "transparent",
                color: isActive ? "var(--foreground)" : "var(--fg-2)",
              }}
              onMouseEnter={(e) => {
                if (!isActive)
                  e.currentTarget.style.background = "var(--surface-2)";
              }}
              onMouseLeave={(e) => {
                if (!isActive) e.currentTarget.style.background = "transparent";
              }}
              title={collapsed ? tab.label : undefined}
            >
              <Icon
                className="w-3.5 h-3.5 shrink-0"
                strokeWidth={isActive ? 2.2 : 1.8}
              />
              {!collapsed && (
                <>
                  <span className="text-[13px] font-medium flex-1 text-left">
                    {tab.label}
                  </span>
                  {tab.badge != null && (
                    <span className="text-[10px] font-semibold tabular-nums px-1.5 py-0.5 rounded-full bg-surface-3 text-fg-2">
                      {tab.badge}
                    </span>
                  )}
                </>
              )}
            </button>
          );
        })}
      </nav>

      {/* Bottom: user + settings */}
      <div className="mt-auto px-2 pb-2">
        <div
          aria-hidden
          className="h-px mx-1 mb-2"
          style={{ background: "var(--glass-border)" }}
        />
        <button
          type="button"
          className="flex items-center gap-2 h-9 px-1.5 w-full rounded-lg hover:bg-surface-2 transition-colors"
        >
          <div className="w-6 h-6 rounded-full flex items-center justify-center shrink-0 bg-surface-3">
            <User className="w-3 h-3 text-fg-2" strokeWidth={2} />
          </div>
          {!collapsed && (
            <>
              <span className="text-[12px] font-medium text-foreground truncate flex-1 text-left">
                Jiahao
              </span>
              <MoreHorizontal
                className="w-3.5 h-3.5 text-fg-4 shrink-0"
                strokeWidth={2}
              />
            </>
          )}
        </button>
      </div>
    </motion.div>
  );
}

/* ═══════════════════════════════════════════════
   Secondary panel — FLOATING glass surface, NOT edge-to-edge.
   Mirrors production PinnedTreeSidebar
   (packages/web/src/components/features/topic-tree-panel.tsx:280-440):
     - position: absolute (within the demo shell — fixed in production)
     - 12px insets on all edges
     - 296px width
     - .glass-panel material (kitchen inlines the recipe; production uses
       the .glass-panel class from web globals.css)
     - enter: opacity 0→1, x -12→0, scale 0.985→1, blur 8px→0
     - exit:  reverse with GLASS_EXIT_DURATION / GLASS_EXIT_EASE
   ═══════════════════════════════════════════════ */

const TREE_PANEL_INSET = 12;
const TREE_PANEL_WIDTH = 296;

function ShellSecondaryPanel({
  tab,
  railWidth,
  hidden,
  onClose,
}: {
  tab: ShellTab;
  railWidth: number;
  hidden: boolean;
  onClose: () => void;
}) {
  const tabHasPanel =
    SHELL_TABS.find((t) => t.id === tab)?.hasSecondaryPanel ?? false;
  const expanded = tabHasPanel && !hidden;
  return (
    <AnimatePresence initial={false}>
      {expanded && (
        <motion.aside
          key={tab}
          className="flex flex-col"
          style={{
            // Production glass-panel recipe inlined (kitchen globals doesn't
            // export the .glass-panel class).
            position: "absolute",
            top: TREE_PANEL_INSET,
            left: railWidth + TREE_PANEL_INSET, // after the left rail
            bottom: TREE_PANEL_INSET,
            width: TREE_PANEL_WIDTH,
            background: "color-mix(in srgb, var(--card) 72%, transparent)",
            backdropFilter: "blur(36px) saturate(180%) contrast(1.05)",
            WebkitBackdropFilter: "blur(36px) saturate(180%) contrast(1.05)",
            border: "1px solid oklch(0 0 0 / 0.05)",
            borderRadius: "var(--app-radius-surface, 20px)",
            // Slightly lighter shadow than rail — secondary sits BELOW
            // (z-index 2) so it doesn't compete with the rail's depth.
            boxShadow:
              "inset 0 1px 0 oklch(1 0 0 / 0.4), 0 12px 28px -10px oklch(0 0 0 / 0.06), 0 4px 12px -4px oklch(0 0 0 / 0.04)",
            zIndex: 2,
          }}
          // Push-from-underneath: starts hidden BEHIND the rail (negative x
          // matches the panel's offset from rail-right-edge), slides into
          // place. Rail (z:3) clips the panel during transit so it visibly
          // emerges from beneath.
          initial={{ opacity: 0, x: -(TREE_PANEL_WIDTH + TREE_PANEL_INSET) }}
          animate={{ opacity: 1, x: 0 }}
          exit={{ opacity: 0, x: -(TREE_PANEL_WIDTH + TREE_PANEL_INSET) }}
          transition={{
            duration: NORMAL,
            ease: EASE,
          }}
          aria-label={`${tab} secondary panel`}
        >
          {tab === "memories" && <SecondaryMemories onClose={onClose} />}
          {/* inbox + agents + brain have NO secondary panel — only memories does (topic explorer) */}
        </motion.aside>
      )}
    </AnimatePresence>
  );
}

function SecondaryMemories({ onClose }: { onClose: () => void }) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set(["welcome"]));
  const [active, setActive] = useState<string | undefined>("welcome");
  const toggle = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  return (
    <>
      {/* Hub switcher — hub avatar IS signature (canonical) */}
      <div className="px-3 pt-3 pb-2">
        <button
          type="button"
          className="flex items-center gap-2 w-full h-9 px-2 rounded-lg hover:bg-surface-2 transition-colors"
        >
          <HubAvatar label="Personal" kind="personal" />
          <span className="text-[13px] font-medium text-foreground">
            Personal hub
          </span>
          <ChevronDown className="w-3 h-3 text-fg-4 ml-auto" strokeWidth={2} />
        </button>
      </div>

      {/* Overview entry — active state is NEUTRAL (signature is for dreams) */}
      <div className="px-2">
        <button
          type="button"
          className="flex items-center gap-2 h-8 px-2 w-full rounded-lg transition-colors"
          style={{
            background: "var(--surface-3)",
            color: "var(--foreground)",
          }}
        >
          <Layout className="w-3 h-3 shrink-0" strokeWidth={2} />
          <span className="text-[13px] font-medium">Overview</span>
        </button>
      </div>

      {/* Topics — production TopicTreeNode pattern: rotating chevron, depth
          indent, count badge. Read-only (no user-create endpoint). */}
      <div className="px-3 pt-4 pb-1.5 flex items-center gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-fg-4">
          Topics
        </span>
        <span className="text-[10px] tabular-nums text-fg-4">
          {TOPICS_DEMO.length}
        </span>
        <button
          type="button"
          onClick={onClose}
          className="ml-auto p-0.5 rounded text-fg-4 hover:text-fg-2 hover:bg-surface-2 transition-colors"
          aria-label="Hide panel"
          title="Hide panel (or click the active tab again)"
        >
          <ChevronsLeft className="h-3 w-3" />
        </button>
      </div>
      <div className="flex flex-col px-2 gap-px">
        {TOPICS_DEMO.map((t) => (
          <DemoTopicNode
            key={t.id}
            node={t}
            depth={0}
            expandedIds={expanded}
            activeId={active}
            onToggleExpand={toggle}
            onSelect={setActive}
          />
        ))}
      </div>

      <div
        className="mt-auto px-3 py-2.5"
        style={{ borderTop: "1px solid var(--glass-border)" }}
      >
        <div className="flex items-center gap-2 text-[11px] text-fg-3 mb-1">
          3 unassigned memories
        </div>
        <p className="text-[10px] text-fg-4 leading-snug">
          Topics emerge from dreams; users can&apos;t create them directly.
        </p>
      </div>
    </>
  );
}

/* ═══════════════════════════════════════════════
   Memory card — `surface="card"` preset
   Refero: Manus library #e7ebc0ff (file-type icons + soft halo + 3-dot menu).
   Halo on hover is NEUTRAL (foreground-tinted), not signature — depth
   without color cast. Signature only used on the quote-type card accent
   (Borges, the brand-y memory).
   ═══════════════════════════════════════════════ */

/* Actor attribution — mirrors production memory-row.tsx tiers:
     1. Agent capture  → Bot icon in agent identity color + agent name
     2. Team author    → Avatar (initial-letter) + name
     3. Personal/self  → memax logo + "memax" (system seed)            */
function MemoryCardAttribution({ actor }: { actor: SeedMemory["actor"] }) {
  if (actor.kind === "agent") {
    return (
      <span className="flex items-center gap-1 text-[11px] min-w-0">
        <span
          className="w-3.5 h-3.5 rounded-md flex items-center justify-center shrink-0"
          style={{
            background: `oklch(from ${actor.color} l c h / 0.12)`,
          }}
          title={actor.display}
        >
          <Bot
            className="w-2 h-2"
            strokeWidth={2.2}
            style={{ color: actor.color }}
          />
        </span>
        <span className="truncate" style={{ color: actor.color }}>
          {actor.display}
        </span>
      </span>
    );
  }
  if (actor.kind === "user") {
    return (
      <span className="flex items-center gap-1 text-[11px] min-w-0">
        <span
          className="w-3.5 h-3.5 rounded-full flex items-center justify-center shrink-0 text-[8px] font-semibold text-fg-2"
          style={{
            background: "oklch(from var(--foreground) l c h / 0.08)",
          }}
          title={actor.display}
        >
          {actor.initial}
        </span>
        <span className="text-fg-3 truncate">{actor.display}</span>
      </span>
    );
  }
  return (
    <span className="flex items-center gap-1 text-[11px] min-w-0">
      <span className="w-3.5 h-3.5 rounded-md flex items-center justify-center shrink-0 bg-surface-3 text-fg-2">
        <MemaxLogo size={8} />
      </span>
      <span className="text-fg-3 truncate">{actor.display}</span>
    </span>
  );
}

function MemoryCard({ memory }: { memory: SeedMemory }) {
  const FileIcon =
    memory.fileType === "link"
      ? LinkIcon
      : memory.fileType === "image"
        ? ImageIcon
        : memory.fileType === "quote"
          ? Quote
          : FileText;
  // 2026 minimal hover: hairline + inner ring darken slightly. No drop
  // shadow, no transform, no lift. The glass material conveys depth.
  const REST_BORDER = "oklch(0 0 0 / 0.06)";
  const HOVER_BORDER = "oklch(0 0 0 / 0.10)";
  const REST_RING = "0 0 0 1px oklch(0 0 0 / 0.03)";
  const HOVER_RING = "0 0 0 1px oklch(0 0 0 / 0.06)";
  return (
    <button
      type="button"
      className="group relative flex flex-col text-left rounded-[20px] p-4 h-full"
      style={{
        // Production .glass-card recipe (packages/web/src/app/globals.css:800)
        // — 75% card + 12px blur + 0.06 hairline + inset 0.03 ring.
        background: "color-mix(in srgb, var(--card) 75%, transparent)",
        backdropFilter: "blur(12px) saturate(120%)",
        WebkitBackdropFilter: "blur(12px) saturate(120%)",
        border: `1px solid ${REST_BORDER}`,
        boxShadow: REST_RING,
        transition:
          "border-color 180ms cubic-bezier(0.2, 0.8, 0.2, 1), box-shadow 180ms cubic-bezier(0.2, 0.8, 0.2, 1)",
      }}
      onMouseEnter={(e) => {
        e.currentTarget.style.borderColor = HOVER_BORDER;
        e.currentTarget.style.boxShadow = HOVER_RING;
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.borderColor = REST_BORDER;
        e.currentTarget.style.boxShadow = REST_RING;
      }}
    >
      {/* Header: file-type icon + title + actions menu */}
      <div className="flex items-start gap-2 mb-3">
        <span className="w-6 h-6 rounded-md flex items-center justify-center shrink-0 bg-surface-2">
          <FileIcon className="w-3 h-3 text-fg-3" strokeWidth={2} />
        </span>
        <h4
          className="text-[13px] font-semibold text-foreground flex-1 leading-snug"
          style={{ letterSpacing: "-0.005em" }}
        >
          {memory.title}
        </h4>
        <button
          type="button"
          tabIndex={-1}
          className="w-5 h-5 -mr-0.5 rounded-md flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity text-fg-4 hover:text-fg-2 hover:bg-surface-2"
          aria-label="More actions"
          onClick={(e) => e.stopPropagation()}
        >
          <MoreHorizontal className="w-3 h-3" strokeWidth={2} />
        </button>
      </div>

      {/* Preview */}
      <p
        className={`text-[12px] leading-relaxed line-clamp-3 mb-3 flex-1 ${
          memory.isQuote ? "text-fg-2 italic" : "text-fg-3"
        }`}
      >
        {memory.preview}
      </p>

      {/* Footer: tiered actor attribution + age + (cross-hub origin chip) + topic.
          Production memory-row.tsx renders the hub label only when the
          memory is from a hub OTHER than the current view (e.g., recall
          surfaces a memory from a team hub while you're viewing personal).
          On a single-hub view, the hub chip is suppressed — your hub is
          implicit. */}
      <div className="flex items-center gap-2 text-[11px] text-fg-4 min-w-0">
        <MemoryCardAttribution actor={memory.actor} />
        <span className="text-fg-4">·</span>
        <span className="tabular-nums text-fg-4 shrink-0">{memory.age}</span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {memory.hub && memory.hub !== "Personal" && (
            <span
              className="text-[10px] font-medium px-1.5 py-0.5 rounded-md flex items-center gap-1"
              style={{
                background:
                  memory.hub === "Engineering"
                    ? getHubAccentStyle("blue", "team").background
                    : getHubAccentStyle(undefined, "team").background,
                color:
                  memory.hub === "Engineering"
                    ? getHubAccentStyle("blue", "team").foreground
                    : getHubAccentStyle(undefined, "team").foreground,
              }}
              title={`From ${memory.hub} hub`}
            >
              {memory.hub}
            </span>
          )}
          {memory.topic && (
            <span className="truncate text-fg-3 text-[10px] px-1.5 py-0.5 rounded-md bg-surface-2">
              {memory.topic}
            </span>
          )}
        </div>
      </div>
    </button>
  );
}

/* ═══════════════════════════════════════════════════════════════════════════
   Topic folder card — only new card type in plan 20.

   Same `.glass-card` material as the existing MemoryCard (no special shadow
   recipe, no chromatic edge-light, no content peek). The folder affordance
   comes from one move only:
     - Stacked-depth illusion — two pseudo-edges peek from underneath, like
       macOS Stack / Apple Notes folder. Eye reads "container" instantly.

   Optionally: peek of 2-3 child names beneath the description, so the user
   sees what's inside before opening. Hover reveals "Open →".

   In production: `layoutId={topic-${id}}` for framer-motion morph into the
   topic detail page header. Static kitchen can't animate; left as a comment.

   Memory cards on this page use the baseline MemoryCard above — production-
   aligned attribution via MemoryCardAttribution helper, same recipe as the
   mobile shell already ships.
   ═══════════════════════════════════════════════════════════════════════════ */

/* Memory card — uses the baseline `MemoryCard` (above). Plan 20 keeps it as-is.
   Per-card spec the user confirmed:
   - title
   - summary
   - actor (via existing MemoryCardAttribution helper)
   - optional topic chip OR dream-delta badge (one slot)
   - time
   No content peek, no chromatic edge-light, no fabricated agent glyphs.
   The mobile shell already implements this shape — desktop matches it. */

/* Topic folder card.
   - Stacked-depth illusion via two absolute-positioned pseudo-edges peeking
     from below the main card.
   - Peek-of-contents: 2-3 child names fade in under the description.
   - In production: `layoutId={topic-${id}}` for framer-motion morph into
     topic detail page header.
*/
type TopicFolderDemo = {
  id: string;
  icon: string; // emoji-as-stand-in for the production TopicIcon component
  name: string;
  description: string;
  memoryCount: number;
  subtopicCount: number;
  preview: string[]; // child names that peek through the card
  accentColor?: string; // optional topic accent for icon glow
};

const TOPIC_FOLDERS: TopicFolderDemo[] = [
  {
    id: "welcome",
    icon: "✦",
    name: "Welcome",
    description:
      "First-light memories — Borges, what is memax, how to capture.",
    memoryCount: 4,
    subtopicCount: 0,
    preview: ["Memory as Mirror", "What is memax", "How recall works"],
    accentColor: "oklch(0.72 0.10 80)",
  },
  {
    id: "tutorial",
    icon: "📖",
    name: "Tutorial",
    description: "Concrete examples of agent captures, dumps, and dreams.",
    memoryCount: 8,
    subtopicCount: 2,
    preview: [
      "Hatch Jarvis PR stack",
      "Topic explorer design north star",
      "What dreams do",
    ],
    accentColor: "oklch(0.68 0.10 220)",
  },
  {
    id: "engineering",
    icon: "🛠",
    name: "Engineering",
    description: "Code architecture, infra notes, debugging recipes.",
    memoryCount: 23,
    subtopicCount: 4,
    preview: [
      "Tree-shaking config",
      "Postgres RLS plan",
      "Edge runtime caveats",
    ],
    accentColor: "oklch(0.62 0.14 270)",
  },
];

function TopicFolderCard({ topic }: { topic: TopicFolderDemo }) {
  // Per user spec: topic icon + title + description.
  // Same baseline `.glass-card` material as the production MemoryCard
  // (no chromatic edge-light, no inflated shadow recipe). Plus a clean
  // vertical paper-stack to signal "container" without numbers.
  // Other data fields (memoryCount, subtopicCount, preview, accentColor)
  // stay on the type so we can iterate UI later without re-plumbing data.
  const accent = topic.accentColor ?? "oklch(from var(--foreground) l c h)";

  return (
    <div className="relative" style={{ paddingBottom: 8 }}>
      {/* Vertical stack hint — two pseudo-cards drawn behind, narrower on each
          side and translated DOWN by a uniform 4px per layer. Same recipe as
          the main card; no chromatic accent. */}
      <div
        aria-hidden
        className="absolute rounded-[20px] pointer-events-none"
        style={{
          left: 12,
          right: 12,
          top: 0,
          bottom: 0,
          transform: "translateY(8px)",
          background: "color-mix(in srgb, var(--card) 60%, transparent)",
          backdropFilter: "blur(10px)",
          WebkitBackdropFilter: "blur(10px)",
          border: "1px solid oklch(0 0 0 / 0.04)",
          opacity: 0.5,
        }}
      />
      <div
        aria-hidden
        className="absolute rounded-[20px] pointer-events-none"
        style={{
          left: 6,
          right: 6,
          top: 0,
          bottom: 0,
          transform: "translateY(4px)",
          background: "color-mix(in srgb, var(--card) 70%, transparent)",
          backdropFilter: "blur(11px)",
          WebkitBackdropFilter: "blur(11px)",
          border: "1px solid oklch(0 0 0 / 0.05)",
          opacity: 0.8,
        }}
      />
      {/* Main card — same minimal-hover treatment as MemoryCard.
          Hairline + inner ring darken on hover. No drop shadow growth,
          no transform. */}
      <button
        type="button"
        className="group relative flex flex-col text-left rounded-[20px] p-4 w-full h-full"
        style={{
          background: "color-mix(in srgb, var(--card) 75%, transparent)",
          backdropFilter: "blur(12px) saturate(120%)",
          WebkitBackdropFilter: "blur(12px) saturate(120%)",
          border: "1px solid oklch(0 0 0 / 0.06)",
          boxShadow: "0 0 0 1px oklch(0 0 0 / 0.03)",
          transition:
            "border-color 180ms cubic-bezier(0.2, 0.8, 0.2, 1), box-shadow 180ms cubic-bezier(0.2, 0.8, 0.2, 1)",
        }}
        onMouseEnter={(e) => {
          e.currentTarget.style.borderColor = "oklch(0 0 0 / 0.10)";
          e.currentTarget.style.boxShadow = "0 0 0 1px oklch(0 0 0 / 0.06)";
        }}
        onMouseLeave={(e) => {
          e.currentTarget.style.borderColor = "oklch(0 0 0 / 0.06)";
          e.currentTarget.style.boxShadow = "0 0 0 1px oklch(0 0 0 / 0.03)";
        }}
      >
        {/* Header: topic icon + title */}
        <div className="flex items-start gap-2 mb-2">
          <span
            className="w-7 h-7 rounded-lg flex items-center justify-center shrink-0 text-[14px]"
            style={{
              background: `oklch(from ${accent} l c h / 0.08)`,
              color: `oklch(from ${accent} l c h)`,
            }}
          >
            {topic.icon}
          </span>
          <h4
            className="text-[13px] font-semibold text-foreground flex-1 leading-snug"
            style={{ letterSpacing: "-0.005em" }}
          >
            {topic.name}
          </h4>
        </div>

        {/* Description */}
        <p className="text-[12px] text-fg-3 leading-relaxed line-clamp-3">
          {topic.description}
        </p>
      </button>
    </div>
  );
}

/* `+` plus card — first cell. Variant decides label + hint per tab. NEUTRAL hover. */
type PlusCardVariant = "memory" | "agent";

function PlusCard({
  variant,
  onClick,
}: {
  variant: PlusCardVariant;
  onClick: () => void;
}) {
  const labels =
    variant === "memory"
      ? {
          title: "New memory",
          hint: (
            <>
              Drop · type · paste
              <br />
              <kbd className="text-[10px] px-1 py-px rounded bg-surface-2 mt-1 inline-block">
                ⌘⇧↵
              </kbd>
            </>
          ),
        }
      : {
          title: "Connect a new agent",
          hint: <>Claude Code · Cursor · Codex · MCP</>,
        };
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex flex-col items-center justify-center rounded-[20px] p-4 h-full transition-all bg-transparent hover:bg-surface-1"
      style={{
        border: "1px dashed var(--glass-border)",
        minHeight: 168,
      }}
    >
      <span className="w-9 h-9 rounded-full flex items-center justify-center mb-2 transition-colors bg-surface-2 group-hover:bg-surface-3">
        <Plus
          className="w-4 h-4 text-fg-3 group-hover:text-foreground"
          strokeWidth={2.2}
        />
      </span>
      <span className="text-[13px] font-medium text-foreground mb-0.5">
        {labels.title}
      </span>
      <span className="text-[11px] text-fg-4 text-center leading-snug max-w-35">
        {labels.hint}
      </span>
    </button>
  );
}

/* ═══════════════════════════════════════════════
   Hub header — minimal stand-in for prod HubHeader.
   Hub avatar uses signature — that's the canonical accent (kitchen 11).
   ═══════════════════════════════════════════════ */

function FakeHubHeader() {
  return (
    <div className="flex items-start gap-3 mb-6">
      <HubAvatar label="Personal" kind="personal" size="lg" />
      <div className="flex-1 min-w-0">
        <h2
          className="text-[20px] font-semibold text-foreground"
          style={{ letterSpacing: "-0.015em" }}
        >
          Personal hub
        </h2>
        <p className="text-[13px] text-fg-3 mt-0.5">
          47 memories · 12 topics · 3 dreams this week
        </p>
      </div>
      {/* No second search button — bottom-right floating ⌘K scan is the
          single persistent entrypoint to recall / ask. */}
    </div>
  );
}

/* ═══════════════════════════════════════════════
   Memories Overview (default route content)
   ═══════════════════════════════════════════════ */

function MainViewMemoriesOverview({ onCompose }: { onCompose: () => void }) {
  return (
    <div className="px-8 py-6 max-w-275 mx-auto">
      <FakeHubHeader />

      <p className="text-[14px] text-fg-2 mb-5">
        Hi, Jiahao.{" "}
        <span className="text-fg-4">
          Drop something below — or connect an agent and let it dump for you.
        </span>
      </p>

      {/* Topic folder cards preview — only NEW card type in plan 20.
          Memory cards stay at the baseline below (uses production
          MemoryCardAttribution helper, same shape as mobile shell). */}
      <div className="mb-8">
        <div className="flex items-center gap-3 mb-3">
          <h3 className="text-[14px] font-semibold text-foreground">
            Topic folder cards
          </h3>
          <span className="text-[12px] text-fg-4">plan 20 preview</span>
        </div>
        <p className="text-[11px] text-fg-4 mb-3 max-w-prose leading-relaxed">
          On the topic detail page (`/h/&lt;slug&gt;/topics/&lt;id&gt;`), folder
          cards sit at top with subtopics; memory cards below. Folder card is{" "}
          <span className="text-fg-3">topic icon + title + description</span>.
          UI iterates; data shape (memoryCount, subtopicCount, preview,
          accentColor) stays so we can layer in moves without re-plumbing data.
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 mb-2">
          {TOPIC_FOLDERS.map((t) => (
            <TopicFolderCard key={t.id} topic={t} />
          ))}
        </div>
      </div>

      {/* Section header — no decorator icon, type carries it */}
      <div className="flex items-center gap-3 mb-3">
        <h3 className="text-[14px] font-semibold text-foreground">
          Fresh memory
        </h3>
        <span className="text-[12px] text-fg-4">7 days · all actors</span>
        <button
          type="button"
          className="ml-auto text-[12px] text-fg-3 hover:text-foreground inline-flex items-center gap-1"
        >
          Filter
          <ChevronDown className="w-3 h-3" strokeWidth={2} />
        </button>
      </div>

      {/* Card grid */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
        <PlusCard variant="memory" onClick={onCompose} />
        {SEED_MEMORIES.map((m) => (
          <MemoryCard key={m.id} memory={m} />
        ))}
      </div>

      <p className="text-[11px] text-fg-4 mt-3 text-center">
        6 seed memories. Admin-configurable in production (plan 23).
      </p>
    </div>
  );
}

/* Brain stub — neutral brand mark + lucide Brain icon as accent */

function MainViewBrain() {
  return (
    <div className="flex-1 flex flex-col items-center justify-center text-center px-8">
      <div className="w-12 h-12 rounded-[20px] flex items-center justify-center mb-4 bg-surface-2 text-foreground">
        <MemaxLogo size={24} />
      </div>
      <h2 className="text-[20px] font-semibold text-foreground mb-2">Brain</h2>
      <p className="text-[13px] text-fg-3 max-w-[400px]">
        v1: existing <span className="font-mono">BrainView</span> (bar + badge)
        renders here. v2: agentic conversation thread + chat sessions in the
        secondary panel — Ziyang&apos;s work.
      </p>
    </div>
  );
}

// Agents — vertical Surface row list per production AgentConfigsSection
// (packages/web/src/components/features/agent-configs-section.tsx:660 uses
// <Surface variant="subtle" rounded="2xl" className="px-5 py-4">). Each row
// shows agent identity icon (color-tinted bg), display name, status dot,
// recent activity, and a chevron to expand. NOT a card grid.
type AgentDemo = {
  id: string;
  name: string;
  glyph: string; // emoji or letter (production uses resolveAgentIdentity for lucide icons)
  color: string; // identity color
  status: "just_now" | "today" | "idle";
  recentActivity: string;
  memories: number;
  configs: number;
  keys: number;
};

const AGENTS_DEMO: AgentDemo[] = [
  {
    id: "claude-code",
    name: "Claude Code",
    glyph: "🤖",
    color: "oklch(0.65 0.12 30)",
    status: "just_now",
    recentActivity: "Captured 2 memories · just now",
    memories: 142,
    configs: 4,
    keys: 1,
  },
  {
    id: "cursor",
    name: "Cursor",
    glyph: "▣",
    color: "oklch(0.6 0.04 240)",
    status: "today",
    recentActivity: "Last sync 3h ago",
    memories: 28,
    configs: 2,
    keys: 1,
  },
  {
    id: "codex",
    name: "Codex",
    glyph: "⌬",
    color: "oklch(0.55 0.02 200)",
    status: "idle",
    recentActivity: "Idle for 4 days",
    memories: 7,
    configs: 1,
    keys: 1,
  },
];

function StatusDot({ status }: { status: AgentDemo["status"] }) {
  // Mirrors production status-dot semantics (agent-configs-section.tsx:362).
  const filled = status !== "idle";
  const colors: Record<AgentDemo["status"], string> = {
    just_now: "oklch(0.72 0.19 145)", // emerald, live
    today: "oklch(0.72 0.19 145)",
    idle: "var(--fg-3)",
  };
  return (
    <span className="relative flex h-2 w-2 shrink-0" aria-hidden>
      {status === "just_now" && (
        <span
          className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
          style={{ backgroundColor: colors[status] }}
        />
      )}
      <span
        className="relative inline-flex h-2 w-2 rounded-full"
        style={{
          backgroundColor: filled ? colors[status] : "transparent",
          border: filled ? "none" : "1px solid var(--fg-3)",
        }}
      />
    </span>
  );
}

function AgentDemoRow({ agent }: { agent: AgentDemo }) {
  return (
    <button
      type="button"
      className="w-full flex items-center gap-3 rounded-[20px] px-5 py-4 text-left transition-colors"
      style={{
        // Surface variant="subtle" recipe — production uses --surface-subtle.
        background: "color-mix(in srgb, var(--card) 75%, transparent)",
        backdropFilter: "blur(12px) saturate(120%)",
        WebkitBackdropFilter: "blur(12px) saturate(120%)",
        border: "1px solid oklch(0 0 0 / 0.06)",
        boxShadow: "0 0 0 1px oklch(0 0 0 / 0.03)",
      }}
    >
      {/* Identity icon — color-tinted square */}
      <div
        className="flex items-center justify-center h-9 w-9 rounded-md shrink-0"
        style={{ background: `oklch(from ${agent.color} l c h / 0.12)` }}
      >
        <span className="text-[16px]" aria-hidden>
          {agent.glyph}
        </span>
      </div>

      {/* Name + recent activity */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-0.5">
          <h4 className="text-[14px] font-semibold text-foreground truncate">
            {agent.name}
          </h4>
          <StatusDot status={agent.status} />
        </div>
        <p className="text-[12px] text-fg-3 truncate">{agent.recentActivity}</p>
      </div>

      {/* Stats trio */}
      <div className="hidden sm:flex items-center gap-4 text-[11px] text-fg-4 shrink-0">
        <span>
          <span className="tabular-nums text-fg-2">{agent.memories}</span>{" "}
          memories
        </span>
        <span>
          <span className="tabular-nums text-fg-2">{agent.configs}</span>{" "}
          configs
        </span>
        <span>
          <span className="tabular-nums text-fg-2">{agent.keys}</span> key
        </span>
      </div>

      <ChevronDown className="w-3.5 h-3.5 text-fg-4 shrink-0" strokeWidth={2} />
    </button>
  );
}

function MainViewAgents() {
  return (
    <div className="px-8 py-6 max-w-275 mx-auto">
      <h2
        className="text-[20px] font-semibold text-foreground mb-1"
        style={{ letterSpacing: "-0.015em" }}
      >
        Connected agents
      </h2>
      <p className="text-[13px] text-fg-3 mb-6">
        3 connected · last active 2m ago
      </p>
      <div className="space-y-3">
        {AGENTS_DEMO.map((a) => (
          <AgentDemoRow key={a.id} agent={a} />
        ))}
        {/* Connect-new entry — same row shape as agent rows */}
        <button
          type="button"
          className="w-full flex items-center gap-3 rounded-[20px] px-5 py-4 text-left transition-colors hover:bg-surface-1"
          style={{ border: "1px dashed oklch(0 0 0 / 0.08)" }}
        >
          <div className="flex items-center justify-center h-9 w-9 rounded-md shrink-0 bg-surface-2">
            <Plus className="w-4 h-4 text-fg-3" strokeWidth={2.2} />
          </div>
          <div className="flex-1 min-w-0">
            <h4 className="text-[14px] font-semibold text-foreground mb-0.5">
              Connect a new agent
            </h4>
            <p className="text-[12px] text-fg-3 truncate">
              Claude Code · Cursor · Codex · MCP
            </p>
          </div>
          <ArrowRight
            className="w-3.5 h-3.5 text-fg-4 shrink-0"
            strokeWidth={2}
          />
        </button>
      </div>
    </div>
  );
}

function MainViewInbox() {
  return (
    <div className="flex-1 flex flex-col items-center justify-center text-center px-8">
      <InboxIcon className="w-8 h-8 text-fg-4 mb-3" strokeWidth={1.5} />
      <h2 className="text-[16px] font-semibold text-foreground mb-1">Inbox</h2>
      <p className="text-[13px] text-fg-4 max-w-90">
        Existing kind-dispatched renderer ([inbox-control.tsx]) mounts here
        unchanged. Shell only changes where it lives, not how it renders.
      </p>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   Full desktop shell
   ═══════════════════════════════════════════════ */

function ShellDesktopDemo() {
  const [tab, setTab] = useState<ShellTab>("memories");
  const [collapsed, setCollapsed] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
  // secondaryHidden — user explicitly hid the panel via the close
  // chevron OR by clicking the active tab again (Notion convention).
  // Resets when the user picks a different tab.
  const [secondaryHidden, setSecondaryHidden] = useState(false);

  const tabHasSecondary =
    SHELL_TABS.find((t) => t.id === tab)?.hasSecondaryPanel ?? false;
  const secondaryExpanded = tabHasSecondary && !secondaryHidden;
  const railWidth = collapsed ? 56 : 196;
  const mainPaddingLeft = secondaryExpanded
    ? TREE_PANEL_INSET +
      railWidth +
      TREE_PANEL_INSET +
      TREE_PANEL_WIDTH +
      TREE_PANEL_INSET
    : TREE_PANEL_INSET + railWidth + TREE_PANEL_INSET;
  const handleTabChange = (next: ShellTab) => {
    if (next === tab) {
      // Clicking active tab toggles panel visibility (Notion / Linear pattern).
      if (tabHasSecondary) setSecondaryHidden((h) => !h);
      return;
    }
    setTab(next);
    setSecondaryHidden(false); // new tab → panel back on
  };
  return (
    <div
      className="relative rounded-3xl overflow-hidden bg-background"
      style={{
        // Repurposes the per-hub time-based aurora to the page background
        // layer. Production uses this gradient inside HubHeaderBanner only;
        // plan 19 will wire a CSS var so a "calm" / "off" mode can disable
        // page-level aurora per hub preference. Resolves to current time
        // bucket (deep-night / morning / noon / afternoon / evening / late-night).
        backgroundImage: AURORA_GRADIENTS[getHubHeaderTimeBucket(new Date())],
        backgroundColor: "var(--background)",
        border: "1px solid oklch(0 0 0 / 0.06)",
        boxShadow: "var(--glass-shadow)",
        height: 600,
      }}
    >
      {/* Both panels are absolutely positioned floating glass surfaces */}
      <ShellLeftRail
        active={tab}
        onChange={handleTabChange}
        collapsed={collapsed}
        onToggleCollapse={() => setCollapsed((c) => !c)}
        width={railWidth}
      />
      <ShellSecondaryPanel
        tab={tab}
        railWidth={railWidth}
        hidden={secondaryHidden}
        onClose={() => setSecondaryHidden(true)}
      />

      {/* Main content — padded to clear both floating panels. Snappy: NORMAL + EASE. */}
      <motion.div
        className="absolute inset-y-0 right-0 overflow-y-auto"
        animate={{ paddingLeft: mainPaddingLeft }}
        transition={{ duration: NORMAL, ease: EASE }}
        style={{ left: 0 }}
      >
        {tab === "brain" && <MainViewBrain />}
        {tab === "memories" && (
          <MainViewMemoriesOverview onCompose={() => setComposeOpen(true)} />
        )}
        {tab === "agents" && <MainViewAgents />}
        {tab === "inbox" && <MainViewInbox />}
      </motion.div>

      {/* Compose modal stub */}
      <AnimatePresence>
        {composeOpen && (
          <motion.div
            className="absolute inset-0 z-10 flex items-center justify-center"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: NORMAL, ease: EASE }}
            style={{
              background: "oklch(from var(--background) l c h / 0.55)",
              backdropFilter: "blur(8px)",
            }}
            onClick={() => setComposeOpen(false)}
          >
            <motion.div
              initial={{ y: 12, opacity: 0 }}
              animate={{ y: 0, opacity: 1 }}
              exit={{ y: 12, opacity: 0 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="rounded-[20px] p-5 w-120 bg-card"
              style={{
                border: "1px solid var(--glass-border)",
                boxShadow:
                  "0 24px 64px -20px oklch(from var(--foreground) l c h / 0.32)",
              }}
              onClick={(e) => e.stopPropagation()}
            >
              <div className="flex items-center gap-2 mb-3">
                <FilePlus className="w-4 h-4 text-fg-3" strokeWidth={2} />
                <span className="text-[14px] font-semibold text-foreground">
                  New memory
                </span>
                <button
                  type="button"
                  onClick={() => setComposeOpen(false)}
                  className="ml-auto w-6 h-6 rounded-md flex items-center justify-center text-fg-4 hover:text-fg-2 hover:bg-surface-2"
                >
                  <X className="w-3.5 h-3.5" strokeWidth={2} />
                </button>
              </div>
              <p className="text-[12px] text-fg-3 mb-3">
                Live preview editor (Tiptap, no edit/preview toggle). Markdown
                shortcuts render inline as you type — like Notion. Plan 20.
              </p>
              <div
                className="rounded-lg p-3 min-h-30 text-[13px] text-fg-4 bg-surface-1"
                style={{
                  border: "1px solid var(--glass-border)",
                }}
              >
                Type <span className="font-mono"># </span> for h1,{" "}
                <span className="font-mono">- </span> for a list, paste a link
                to embed…
              </div>
              <div className="flex items-center gap-2 mt-4 text-[11px] text-fg-4">
                <span>◐ Personal hub</span>
                <span>·</span>
                <span>Drag a file to attach</span>
                <button
                  type="button"
                  className="ml-auto h-7 px-3 rounded-lg text-[12px] font-medium"
                  style={{
                    background: "var(--foreground)",
                    color: "var(--background)",
                  }}
                >
                  Save ⌘↵
                </button>
              </div>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>

      {/* Floating scan icon — neutral glass */}
      <div className="absolute bottom-4 right-4 z-5 pointer-events-none">
        <div
          className="w-10 h-10 rounded-full flex items-center justify-center pointer-events-auto bg-card"
          style={{
            border: "1px solid var(--glass-border)",
            boxShadow: "var(--glass-shadow)",
          }}
        >
          <Search className="w-4 h-4 text-fg-3" strokeWidth={2} />
        </div>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   Mobile demo — push-stack, no dock
   Refero: Notion mobile (apps/64), Linear mobile (apps/204).
   ═══════════════════════════════════════════════ */

type MobileScreen = "main" | "drawer" | "topic" | "memory";

function ShellMobileDemo() {
  const [screen, setScreen] = useState<MobileScreen>("main");

  return (
    <div className="flex justify-center items-start gap-6 flex-wrap">
      <div
        className="rounded-[40px] p-2 shrink-0 bg-surface-2"
        style={{ width: 320, border: "1px solid var(--glass-border)" }}
      >
        <div
          className="rounded-[32px] overflow-hidden relative bg-background"
          style={{
            height: 580,
            // Same time-based aurora as desktop demo (no 1-pane vs 3-pane
            // distinction — aurora is a page-background concern).
            backgroundImage:
              AURORA_GRADIENTS[getHubHeaderTimeBucket(new Date())],
          }}
        >
          <div className="h-6 flex items-center justify-center">
            <div className="w-16 h-1 rounded-full bg-surface-3" />
          </div>

          <AnimatePresence initial={false}>
            {screen === "main" && (
              <motion.div
                key="main"
                className="absolute inset-x-0 bottom-0"
                style={{ top: 24 }}
                initial={{ x: -40, opacity: 0.5 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: -40, opacity: 0 }}
                transition={{ duration: NORMAL, ease: EASE }}
              >
                <MobileMain
                  onMenuClick={() => setScreen("drawer")}
                  onTopicClick={() => setScreen("topic")}
                  onMemoryClick={() => setScreen("memory")}
                />
              </motion.div>
            )}
            {screen === "drawer" && (
              <motion.div
                key="drawer"
                className="absolute inset-y-0 left-0"
                style={{ width: "75%", top: 24 }}
                initial={{ x: "-100%" }}
                animate={{ x: 0 }}
                exit={{ x: "-100%" }}
                transition={{ duration: NORMAL, ease: EASE }}
              >
                <MobileDrawer onClose={() => setScreen("main")} />
              </motion.div>
            )}
            {screen === "topic" && (
              <motion.div
                key="topic"
                className="absolute inset-x-0 bottom-0 bg-background"
                style={{ top: 24 }}
                initial={{ x: "100%" }}
                animate={{ x: 0 }}
                exit={{ x: "100%" }}
                transition={{ duration: NORMAL, ease: EASE }}
              >
                <MobilePushed
                  title="Welcome"
                  subtitle="4 memories · seed"
                  onBack={() => setScreen("main")}
                />
              </motion.div>
            )}
            {screen === "memory" && (
              <motion.div
                key="memory"
                className="absolute inset-x-0 bottom-0 bg-background"
                style={{ top: 24 }}
                initial={{ x: "100%" }}
                animate={{ x: 0 }}
                exit={{ x: "100%" }}
                transition={{ duration: NORMAL, ease: EASE }}
              >
                <MobileMemoryDetail onBack={() => setScreen("main")} />
              </motion.div>
            )}
          </AnimatePresence>

          {screen === "drawer" && (
            <button
              type="button"
              className="absolute inset-y-0 right-0"
              style={{
                left: "75%",
                top: 24,
                background: "oklch(from var(--background) l c h / 0.5)",
                backdropFilter: "blur(2px)",
              }}
              onClick={() => setScreen("main")}
              aria-label="Close drawer"
            />
          )}
        </div>
      </div>

      <div className="flex flex-col gap-2 shrink-0">
        {(
          [
            ["main", "Main view", "Hub overview, fresh memory grid"],
            ["drawer", "☰ Drawer open", "Left rail slides in from edge"],
            ["topic", "→ Topic pushed", "Tap topic = full-screen push"],
            ["memory", "→ Memory pushed", "Tap card = detail full-screen"],
          ] as const
        ).map(([id, label, hint]) => {
          const isActive = screen === id;
          return (
            <button
              key={id}
              type="button"
              onClick={() => setScreen(id)}
              className="text-left rounded-lg p-2.5 transition-colors"
              style={{
                background: isActive ? "var(--surface-3)" : "transparent",
                border: isActive
                  ? "1px solid oklch(from var(--foreground) l c h / 0.18)"
                  : "1px solid var(--glass-border)",
                color: isActive ? "var(--foreground)" : "var(--fg-2)",
                minWidth: 220,
              }}
            >
              <div className="text-[12px] font-semibold mb-0.5">{label}</div>
              <div
                className="text-[11px]"
                style={{ color: isActive ? "var(--fg-3)" : "var(--fg-4)" }}
              >
                {hint}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function MobileMain({
  onMenuClick,
  onTopicClick,
  onMemoryClick,
}: {
  onMenuClick: () => void;
  onTopicClick: () => void;
  onMemoryClick: () => void;
}) {
  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="flex items-center gap-2 px-4 h-12 shrink-0">
        <button
          type="button"
          onClick={onMenuClick}
          className="w-8 h-8 rounded-md flex items-center justify-center text-fg-2 hover:bg-surface-2"
          aria-label="Open menu"
        >
          <Menu className="w-4 h-4" strokeWidth={2} />
        </button>
        <HubAvatar label="Personal" kind="personal" />
        <span className="text-[13px] font-semibold text-foreground">
          Personal hub
        </span>
        <Search className="w-4 h-4 text-fg-3 ml-auto" strokeWidth={2} />
      </div>

      <div className="flex-1 overflow-y-auto px-4 pt-2 pb-20 space-y-3">
        <h2
          className="text-[18px] font-semibold text-foreground"
          style={{ letterSpacing: "-0.015em" }}
        >
          Hi, Jiahao.
        </h2>

        <div className="flex items-center gap-1.5 mt-3">
          <span className="text-[12px] font-semibold text-foreground">
            Fresh memory
          </span>
          <span className="text-[10px] text-fg-4 ml-auto">7 days</span>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <button
            type="button"
            className="flex flex-col items-center justify-center rounded-[20px] text-[10px] text-fg-3 gap-1"
            style={{
              border: "1px dashed var(--glass-border)",
              minHeight: 130,
            }}
          >
            <Plus className="w-4 h-4" strokeWidth={2} />
            New
          </button>
          {SEED_MEMORIES.slice(0, 3).map((m) => (
            <button
              key={m.id}
              type="button"
              onClick={onMemoryClick}
              className="text-left rounded-[20px] p-3"
              style={{
                // Same .glass-card recipe as desktop MemoryCard.
                background: "color-mix(in srgb, var(--card) 75%, transparent)",
                backdropFilter: "blur(12px) saturate(120%)",
                WebkitBackdropFilter: "blur(12px) saturate(120%)",
                border: "1px solid oklch(0 0 0 / 0.06)",
                boxShadow: "0 0 0 1px oklch(0 0 0 / 0.03)",
                minHeight: 130,
              }}
            >
              <div className="w-4 h-4 rounded mb-2 flex items-center justify-center bg-surface-2">
                <FileText className="w-2.5 h-2.5 text-fg-3" strokeWidth={2} />
              </div>
              <div className="text-[11px] font-semibold text-foreground line-clamp-1">
                {m.title}
              </div>
              <div className="text-[10px] text-fg-4 line-clamp-2 mt-1">
                {m.preview}
              </div>
              <div className="mt-1.5">
                <MemoryCardAttribution actor={m.actor} />
              </div>
            </button>
          ))}
        </div>

        <button
          type="button"
          onClick={onTopicClick}
          className="flex items-center gap-2 w-full rounded-md p-2 hover:bg-surface-2 mt-3"
        >
          <span className="text-[12px]">✦</span>
          <span className="text-[12px] font-medium text-foreground">
            Welcome
          </span>
          <span className="text-[10px] text-fg-4 tabular-nums ml-auto">4</span>
          <ChevronRight className="w-3 h-3 text-fg-4" strokeWidth={2} />
        </button>
      </div>

      <div className="absolute bottom-4 right-4 pointer-events-none">
        <div
          className="w-10 h-10 rounded-full flex items-center justify-center pointer-events-auto bg-card"
          style={{
            border: "1px solid var(--glass-border)",
            boxShadow: "var(--glass-shadow)",
          }}
        >
          <Search className="w-4 h-4 text-fg-3" strokeWidth={2} />
        </div>
      </div>
    </div>
  );
}

function MobileDrawer({ onClose }: { onClose: () => void }) {
  return (
    <div className="h-full flex flex-col" style={{ ...glassSurfaceStyle }}>
      <div className="flex items-center gap-2 px-3 h-12 shrink-0">
        <div className="w-7 h-7 rounded-lg flex items-center justify-center text-foreground bg-surface-2 shrink-0">
          <MemaxLogo size={16} />
        </div>
        <div
          className="overflow-hidden shrink-0"
          style={{ width: 70, height: 20 }}
        >
          <MemaxWordmark
            height={20}
            className="text-foreground"
            style={{ marginLeft: -22 }}
          />
        </div>
        <button
          type="button"
          onClick={onClose}
          className="ml-auto w-7 h-7 rounded-md flex items-center justify-center text-fg-3 hover:bg-surface-2"
        >
          <X className="w-3.5 h-3.5" strokeWidth={2} />
        </button>
      </div>

      <nav className="flex flex-col gap-0.5 px-2 mt-2">
        {SHELL_TABS.map((tab) => {
          const Icon = tab.icon;
          const isActive = tab.id === "memories";
          return (
            <button
              key={tab.id}
              type="button"
              className="flex items-center gap-2.5 h-9 px-2 rounded-lg text-left transition-colors"
              style={{
                background: isActive ? "var(--surface-3)" : "transparent",
                color: isActive ? "var(--foreground)" : "var(--fg-2)",
              }}
            >
              <Icon
                className="w-3.5 h-3.5"
                strokeWidth={isActive ? 2.2 : 1.8}
              />
              <span className="text-[13px] font-medium flex-1">
                {tab.label}
              </span>
              {tab.badge != null && (
                <span className="text-[10px] font-semibold tabular-nums px-1.5 py-0.5 rounded-full bg-surface-3 text-fg-2">
                  {tab.badge}
                </span>
              )}
            </button>
          );
        })}
      </nav>

      <div className="px-3 pt-5 pb-1.5">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-fg-4">
          Hubs
        </span>
      </div>
      <nav className="flex flex-col px-2 gap-px">
        {HUBS_DEMO.map((h) => (
          <button
            key={h.id}
            type="button"
            className="flex items-center gap-2 h-8 px-2 rounded-md hover:bg-surface-2 transition-colors text-left"
          >
            <HubAvatar
              label={h.name}
              kind={h.kind}
              accent={h.accent}
              size="sm"
            />
            <span className="text-[12px] text-fg-2 flex-1 truncate">
              {h.name}
            </span>
            {h.active && (
              <Check className="w-3 h-3 text-foreground" strokeWidth={2.5} />
            )}
          </button>
        ))}
        <button
          type="button"
          className="flex items-center gap-2 h-8 px-2 rounded-md text-fg-3 hover:bg-surface-2"
        >
          <Plus className="w-3 h-3" strokeWidth={2} />
          <span className="text-[12px]">New hub</span>
        </button>
      </nav>

      <div className="mt-auto px-3 pb-3 pt-3 flex items-center gap-2">
        <div className="w-7 h-7 rounded-full flex items-center justify-center bg-surface-3">
          <User className="w-3 h-3 text-fg-2" strokeWidth={2} />
        </div>
        <span className="text-[12px] font-medium text-foreground flex-1">
          Jiahao
        </span>
        <Settings className="w-3.5 h-3.5 text-fg-4" strokeWidth={2} />
      </div>
    </div>
  );
}

function MobilePushed({
  title,
  subtitle,
  onBack,
}: {
  title: string;
  subtitle: string;
  onBack: () => void;
}) {
  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-2 px-2 h-12 shrink-0">
        <button
          type="button"
          onClick={onBack}
          className="w-8 h-8 rounded-md flex items-center justify-center text-fg-2 hover:bg-surface-2"
        >
          <ChevronLeft className="w-4 h-4" strokeWidth={2.2} />
        </button>
        <span className="text-[13px] font-semibold text-foreground">
          {title}
        </span>
      </div>
      <div className="flex-1 overflow-y-auto px-4 pt-2 pb-20">
        <p className="text-[12px] text-fg-3 mb-3">{subtitle}</p>
        <div className="grid grid-cols-2 gap-2">
          {SEED_MEMORIES.slice(0, 4).map((m) => (
            <div
              key={m.id}
              className="rounded-[20px] p-3"
              style={{
                background: "color-mix(in srgb, var(--card) 75%, transparent)",
                backdropFilter: "blur(12px) saturate(120%)",
                WebkitBackdropFilter: "blur(12px) saturate(120%)",
                border: "1px solid oklch(0 0 0 / 0.06)",
                boxShadow: "0 0 0 1px oklch(0 0 0 / 0.03)",
              }}
            >
              <div className="text-[11px] font-semibold text-foreground line-clamp-1 mb-1">
                {m.title}
              </div>
              <div className="text-[10px] text-fg-4 line-clamp-3">
                {m.preview}
              </div>
              <div className="mt-1.5">
                <MemoryCardAttribution actor={m.actor} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function MobileMemoryDetail({ onBack }: { onBack: () => void }) {
  return (
    <div className="h-full flex flex-col">
      <div className="flex items-center gap-2 px-2 h-12 shrink-0">
        <button
          type="button"
          onClick={onBack}
          className="w-8 h-8 rounded-md flex items-center justify-center text-fg-2 hover:bg-surface-2"
        >
          <ChevronLeft className="w-4 h-4" strokeWidth={2.2} />
        </button>
        <span className="text-[12px] font-mono text-fg-4">
          memory-as-mirror.md
        </span>
        <MoreHorizontal className="w-4 h-4 text-fg-4 ml-auto" strokeWidth={2} />
      </div>
      <div className="flex-1 overflow-y-auto px-5 pt-2 pb-12 space-y-4">
        <h1
          className="text-[18px] font-semibold text-foreground"
          style={{ letterSpacing: "-0.015em" }}
        >
          Memory as Mirror
        </h1>
        <p className="text-[13px] text-fg-2 leading-relaxed italic">
          &quot;We are our memory, we are that chimerical museum of shifting
          shapes, that pile of broken mirrors.&quot;
        </p>
        <p className="text-[12px] text-fg-3">— Jorge Luis Borges</p>

        <div
          className="rounded-lg p-3 bg-surface-1"
          style={{ border: "1px solid var(--glass-border)" }}
        >
          <div className="text-[10px] font-semibold uppercase tracking-wider text-fg-4 mb-1.5">
            memax sees this as
          </div>
          <div className="text-[12px] text-fg-2">
            Kind: <span className="font-medium">rationale</span> · Stability:{" "}
            <span className="font-medium">stable</span>
          </div>
          <div className="text-[12px] text-fg-3 mt-0.5">Topic: Welcome</div>
        </div>

        <div className="text-[11px] text-fg-4">
          <div>Source · seed memory · imported on signup</div>
          <div className="mt-1">Tags: #welcome #brand</div>
        </div>
      </div>
    </div>
  );
}

/* ═══════════════════════════════════════════════
   Section export
   ═══════════════════════════════════════════════ */

export function ShellV2Section() {
  return (
    <Section
      title="43. Shell v2 — three-pane desktop, drawer + push-stack mobile"
      description="Plan 19+ frontend architecture. Left rail (Brain / Memories / Agents / Inbox) + contextual secondary panel (280px, matches existing TopicTreePanel) + main view. Glass surfaces, scarce signature accent, MemaxLogo for brand. Onboarding plan 18 nests as pinned region in Memories Overview."
    >
      <DemoCard label="43-shell. Desktop shell — click tabs, expand/collapse rail, click + to open Compose modal stub">
        <p className="text-[12px] text-fg-2 mb-3">
          Three zones: 56–196px <span className="font-mono">LeftRail</span>{" "}
          (glass) + 280px contextual{" "}
          <span className="font-mono">SecondaryPanel</span> (glass-tinted;
          hidden when tab has none — Brain) + flex main view.{" "}
          <span className="font-medium">Signature is scarce</span>: only the hub
          avatar (◐), the active tab, the Save CTA, and{" "}
          <span className="font-mono">system_notice</span> accent. Brand mark
          uses <span className="font-mono">&lt;MemaxLogo /&gt;</span> in neutral
          foreground per kitchen 11 canonical rule.
        </p>
        <ShellDesktopDemo />
      </DemoCard>

      <DemoCard label="43-cards. Memory card preset — neutral halo, file-type icons, Borges card carries the only signature accent">
        <p className="text-[12px] text-fg-2 mb-3">
          New <span className="font-mono">surface=&quot;card&quot;</span> preset
          on the existing <span className="font-mono">MemoryRow</span> component
          (plan 20). Halo on hover is foreground-tinted, NOT signature — depth
          without color cast (Manus library #e7ebc0ff). Borges (
          <span className="font-mono">isQuote: true</span>) is the only card
          that wears signature. The first cell is the{" "}
          <span className="font-mono">+</span> Compose entry with{" "}
          <kbd className="text-[10px] px-1 rounded bg-surface-2">⌘⇧↵</kbd>.
        </p>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 max-w-225">
          <PlusCard variant="memory" onClick={() => {}} />
          {SEED_MEMORIES.map((m) => (
            <MemoryCard key={m.id} memory={m} />
          ))}
        </div>
      </DemoCard>

      <DemoCard label="43-mobile. Mobile shell — main → drawer → push-topic → push-memory">
        <p className="text-[12px] text-fg-2 mb-3">
          No bottom dock. Hamburger summons the rail (slides in from edge, ~75%
          width, dim backdrop closes). Tapping a topic / memory pushes a
          full-screen route;{" "}
          <kbd className="text-[10px] px-1 rounded bg-surface-2">←</kbd> or
          swipe-back pops. Floating ⌘K scan stays bottom-right thumb-zone.
          Refero: Notion mobile (apps/64), Linear mobile (apps/204).
        </p>
        <ShellMobileDemo />
      </DemoCard>

      <DemoCard label="43-routing. URL scheme — Option B (Notion-style namespace)">
        <p className="text-[12px] text-fg-2 mb-3">
          Hub <span className="font-mono">slug</span> is unique only within{" "}
          <span className="font-mono">(hub_type, owner_id)</span> in
          <span className="font-mono">
            {" "}
            packages/server/internal/model/hub.go
          </span>
          , so URLs need the owner namespace to disambiguate team hubs across
          owners.
        </p>
        <pre
          className="text-[11px] leading-relaxed p-3 rounded-lg overflow-x-auto bg-surface-1 text-fg-2"
          style={{
            border: "1px solid var(--glass-border)",
            fontFamily:
              "JetBrains Mono, ui-monospace, SFMono-Regular, monospace",
          }}
        >
          {`/h/<owner-slug>/<hub-slug>/memories            ← Memories Overview (default)
/h/<owner-slug>/<hub-slug>/topics/<topic-id>   ← Topic detail
/h/<owner-slug>/<hub-slug>/memories/<memory-id> ← Memory detail v2 (route, not modal)
/brain                                          ← BrainView (existing) → agentic v2 later
/brain/sessions/<session-id>                    ← Future agentic conversation thread
/agents                                         ← ConnectedAgent grid
/agents/<id>                                    ← Agent detail / config
/inbox                                          ← InboxControl (existing renderer, new route)`}
        </pre>
        <p className="text-[11px] text-fg-4 mt-2">
          Adds <span className="font-mono">users.slug</span> column (unique,
          indexed). Migration shipped in plan 19 alongside the route shell.
        </p>
      </DemoCard>

      <div className="text-[11px] text-fg-3 mt-2 space-y-1.5">
        <p>
          <span className="text-fg-1 font-medium">Tokens reused.</span> Glass
          surfaces (<span className="font-mono">--glass-start/end/border</span>
          ), surface-1/2/3 (calibrated foreground opacities), fg-2/3/4 (text
          opacities), <span className="font-mono">--signature-muted</span> for
          tinted accents. Brand mark via{" "}
          <span className="font-mono">&lt;MemaxLogo /&gt;</span> from{" "}
          <span className="font-mono">@memaxlabs/ui</span>. Curve scale:
          rounded-3xl outer shells, rounded-[20px] cards, rounded-lg rows.
        </p>
        <p>
          <span className="text-fg-1 font-medium">Migration.</span> Cookie-flag
          hybrid via <span className="font-mono">memax_shell=v2</span>. Route by
          route, flag flips when stable. Old layout removed once all routes
          migrated.
        </p>
        <p>
          <span className="text-fg-1 font-medium">Plans this informs.</span> 19
          (shell foundation), 20 (memory card preset + Compose), 21 (memory
          detail v2 + recall-time bug), 22 (mobile push-stack), 23 (seed
          memories admin), 24 (agents + inbox routes). Each ships in 2–3 codex
          rounds.
        </p>
      </div>
    </Section>
  );
}
