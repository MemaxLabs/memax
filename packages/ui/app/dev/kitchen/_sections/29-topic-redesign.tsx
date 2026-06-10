// Maps to: topic-card.tsx, topic-grid.tsx, topic-detail.tsx, expand-search-results.tsx
// Topic展示大改 — three-tier cards, "what's new" signal, recall→topic bridge,
// borderless detail, mobile structural changes.
// MECE scenarios: Browse, Find, Review, Organize, Discover, Export.
// Design rule: NO left-border accents on cards. Use shadow lift for "has new" signal.
"use client";

import { useState, useRef, useEffect } from "react";
import {
  AnimatePresence,
  LayoutGroup,
  motion,
  useReducedMotion,
} from "framer-motion";
import { Section, DemoCard } from "../_shared";
import { ContentError } from "@memaxlabs/ui";
import { NORMAL, FAST, EASE } from "@memaxlabs/ui/tokens/motion";
import {
  Rocket,
  Code,
  Layout,
  LayoutGrid,
  Rows3,
  List,
  Shield,
  Database,
  ChevronRight,
  ChevronDown,
  ChevronLeft,
  Zap,
  Search,
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
  Folder,
  Hammer,
  Server,
  Cpu,
  Users,
  Pin,
  Edit3,
  Trash2,
  ArrowUpRight,
} from "lucide-react";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

/* ══════════════════════════════════════════════════════════════════
   Icon resolver — Topic.Icon is a string (Lucide icon name) in prod.
   Kitchen mirrors that shape: mocks declare `icon?: string`, this
   resolver maps to a Lucide component. Unknown names fall back to
   FileText. Render at 28px (focus header), 16px (inline subtopic),
   14px (drill chip). Memory rows do NOT get the topic icon — they
   already carry their own content-type icon from MemoryRow.
   ══════════════════════════════════════════════════════════════════ */

const TOPIC_ICON_MAP: Record<string, React.ElementType> = {
  Rocket,
  Code,
  Layout,
  Shield,
  Database,
  Zap,
  Folder,
  Hammer,
  Server,
  Cpu,
  Users,
  FileText,
};

function TopicIcon({
  name,
  size = 16,
  className = "text-fg-3 shrink-0",
}: {
  name?: string;
  size?: number;
  className?: string;
}) {
  const Icon = (name && TOPIC_ICON_MAP[name]) || FileText;
  return (
    <Icon
      className={className}
      style={{ width: size, height: size }}
      strokeWidth={1.8}
    />
  );
}

/* ══════════════════════════════════════════════════════════════════
   Topics main view — scale-aware A/B/C
   The same logic applies recursively at every drilled level. Mode is
   chosen by direct-children count (not recursive).
   ══════════════════════════════════════════════════════════════════ */

interface MainViewTopic {
  id: string;
  icon: string;
  name: string;
  description: string;
  lastTouched: string; // pre-formatted age, e.g. "2h"
  topSubtopics?: string[]; // for Mode A only — first 3 subtopic names
  pinned?: boolean;
}

const MAIN_VIEW_MOCK: MainViewTopic[] = [
  {
    id: "engineering",
    icon: "Hammer",
    name: "Engineering",
    description:
      "Backend, frontend, infra — Go services, RSC patterns, Fly.io, GitHub Actions.",
    lastTouched: "2h",
    topSubtopics: ["Backend", "Frontend", "Infrastructure"],
    pinned: true,
  },
  {
    id: "auth",
    icon: "Shield",
    name: "Auth & Security",
    description:
      "JWT tokens, OAuth flows, API key rotation, boundary enforcement, secret scanning.",
    lastTouched: "5h",
    topSubtopics: ["OAuth", "API keys", "Sessions"],
  },
  {
    id: "frontend",
    icon: "Layout",
    name: "Frontend",
    description:
      "React Server Components, Tailwind, Radix primitives, design tokens.",
    lastTouched: "8h",
    topSubtopics: ["RSC patterns", "Motion", "State"],
  },
  {
    id: "database",
    icon: "Database",
    name: "Database",
    description:
      "PostgreSQL, pgvector, migrations, connection pooling, replication strategies.",
    lastTouched: "1d",
    topSubtopics: ["Postgres", "Migrations", "Replication"],
  },
  {
    id: "deploy",
    icon: "Rocket",
    name: "Deployment",
    description:
      "Blue-green, rollback procedures, staging automation, Fly.io machine health.",
    lastTouched: "1d",
    topSubtopics: ["Staging", "Production", "Rollback"],
  },
  {
    id: "ingest",
    icon: "Cpu",
    name: "Ingest Pipeline",
    description: "Content ingestion, chunking, embedding, deduplication.",
    lastTouched: "2d",
    topSubtopics: ["Chunking", "Embedding"],
  },
  {
    id: "team-docs",
    icon: "Folder",
    name: "Team Docs",
    description: "Onboarding, code review, release process, incident runbooks.",
    lastTouched: "3d",
    topSubtopics: ["Onboarding", "Releases"],
  },
  {
    id: "go-patterns",
    icon: "Code",
    name: "Go Patterns",
    description:
      "Error handling, context propagation, interface design, testing patterns.",
    lastTouched: "5d",
    topSubtopics: ["Error handling", "Context"],
  },
];

/** PinToggle — corner icon on every topic card / row.
 *  Empty state: outlined Pin at text-fg-4, revealed on hover (desktop) or
 *  always visible (mobile). Filled state: solid Pin at text-fg-1, always
 *  visible. Click stops propagation so the parent card drill doesn't fire.
 *  Used in all three main-view modes (A/B/C) and by the interactive
 *  PinFlowDemo below. */
function PinToggle({
  pinned,
  onToggle,
  size = 14,
}: {
  pinned: boolean;
  onToggle: () => void;
  size?: number;
}) {
  return (
    <button
      type="button"
      aria-label={pinned ? "Unpin topic" : "Pin topic"}
      aria-pressed={pinned}
      onClick={(e) => {
        e.stopPropagation();
        onToggle();
      }}
      className={`shrink-0 p-1 rounded-md transition-all cursor-pointer hover:bg-surface-2 ${
        pinned
          ? "text-fg-1 opacity-100"
          : "text-fg-4 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 md:opacity-0 md:group-hover:opacity-100"
      }`}
      style={{ transitionDuration: `${FAST * 1000}ms` }}
    >
      <Pin
        className=""
        style={{
          width: size,
          height: size,
          fill: pinned ? "currentColor" : "none",
          transform: pinned ? "rotate(-45deg)" : "rotate(0deg)",
          transition: `transform ${FAST}s ${EASE}`,
        }}
      />
    </button>
  );
}

function MainViewCardA({
  topic,
  pinned,
  onTogglePin,
}: {
  topic: MainViewTopic;
  pinned?: boolean;
  onTogglePin?: () => void;
}) {
  // Card body is tappable → drill to topic root (29n morph).
  // Chips are individually tappable → drill straight to that subtopic root
  // using the SAME 29n container morph, skipping the topic root view. Codex
  // should wire both clicks via the same handler that takes a target id.
  return (
    <div
      role="button"
      tabIndex={0}
      className="group rounded-xl p-4 flex flex-col cursor-pointer transition-colors hover:bg-surface-1 focus-visible:bg-surface-1 outline-none"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      <div className="flex items-start gap-2 mb-2">
        <TopicIcon
          name={topic.icon}
          size={18}
          className="text-fg-2 shrink-0 mt-0.5"
        />
        <span className="text-[16px] font-semibold text-foreground truncate flex-1">
          {topic.name}
        </span>
        <PinToggle
          pinned={pinned ?? topic.pinned ?? false}
          onToggle={() => onTogglePin?.()}
          size={14}
        />
        <span className="text-[12px] text-fg-3 tabular-nums shrink-0 mt-0.5">
          {topic.lastTouched}
        </span>
      </div>
      <p className="text-[13px] text-fg-2 line-clamp-2 mb-2">
        {topic.description}
      </p>
      {topic.topSubtopics && topic.topSubtopics.length > 0 && (
        <div className="flex flex-wrap gap-1.5 mt-auto">
          {topic.topSubtopics.slice(0, 3).map((s) => (
            <button
              key={s}
              type="button"
              onClick={(e) => {
                // Prevent the parent card click from also firing.
                e.stopPropagation();
              }}
              className="text-[11px] text-fg-3 bg-surface-1 hover:bg-surface-2 rounded-md px-1.5 py-0.5 cursor-pointer transition-colors"
              title={`Drill into ${topic.name} › ${s}`}
            >
              {s}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function MainViewCardB({
  topic,
  pinned,
  onTogglePin,
}: {
  topic: MainViewTopic;
  pinned?: boolean;
  onTogglePin?: () => void;
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      className="group rounded-lg px-3 py-2.5 flex items-center gap-2 cursor-pointer transition-colors hover:bg-surface-1 focus-visible:bg-surface-1 outline-none"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      <TopicIcon name={topic.icon} size={14} />
      <span className="text-[14px] font-medium text-fg-1 truncate flex-1">
        {topic.name}
      </span>
      <PinToggle
        pinned={pinned ?? topic.pinned ?? false}
        onToggle={() => onTogglePin?.()}
        size={12}
      />
      <span className="text-[12px] text-fg-3 tabular-nums shrink-0">
        {topic.lastTouched}
      </span>
    </div>
  );
}

function MainViewRowC({
  topic,
  pinned,
  onTogglePin,
}: {
  topic: MainViewTopic;
  pinned?: boolean;
  onTogglePin?: () => void;
}) {
  return (
    <div
      role="button"
      tabIndex={0}
      className="group flex items-center gap-3 px-4 py-2 hover:bg-surface-1 focus-visible:bg-surface-1 outline-none cursor-pointer border-t border-border/20"
      style={{ minHeight: 36 }}
    >
      <TopicIcon name={topic.icon} size={14} />
      <span className="text-[14px] text-fg-1 truncate flex-1">
        {topic.name}
      </span>
      <PinToggle
        pinned={pinned ?? topic.pinned ?? false}
        onToggle={() => onTogglePin?.()}
        size={12}
      />
      <span className="text-[12px] text-fg-3 tabular-nums shrink-0">
        {topic.lastTouched}
      </span>
    </div>
  );
}

function TopicMainViewMock({ mode }: { mode: "a" | "b" | "c" }) {
  if (mode === "a") {
    return (
      <div className="rounded-xl border border-border/40 bg-background p-4">
        <div className="flex items-center justify-between mb-3">
          <span className="text-[12px] text-fg-3 uppercase tracking-wider font-medium">
            Your Topics · 8
          </span>
          <span className="text-[11px] text-fg-4">recent activity ▾</span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {MAIN_VIEW_MOCK.slice(0, 8).map((t) => (
            <MainViewCardA key={t.id} topic={t} />
          ))}
        </div>
      </div>
    );
  }
  if (mode === "b") {
    const pinned = MAIN_VIEW_MOCK.filter((t) => t.pinned);
    const others = MAIN_VIEW_MOCK.filter((t) => !t.pinned);
    return (
      <div className="rounded-xl border border-border/40 bg-background p-4">
        {/* Header row — count + quiet ⌘K hint + sort.
            NO local search input: the global command bar (§24m) is the
            filter. Rule 25. */}
        <div className="flex items-center justify-between mb-3 gap-2">
          <span className="text-[12px] text-fg-3 uppercase tracking-wider font-medium">
            {/* i18n: t.topics.count */}
            47 topics
          </span>
          <div className="flex items-center gap-2 text-[11px] text-fg-4">
            <span className="hidden md:inline">
              {/* i18n: t.topics.searchHint */}
              <kbd className="text-[10px] px-1 py-0.5 rounded bg-surface-1 border border-border/40 font-mono">
                ⌘K
              </kbd>{" "}
              to search
            </span>
            <span>recent ▾</span>
          </div>
        </div>
        {/* Pinned */}
        {pinned.length > 0 && (
          <div className="mb-3">
            <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
              {/* i18n: t.topics.pinned */}
              Pinned
            </p>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2">
              {pinned.map((t) => (
                <MainViewCardB key={t.id} topic={t} />
              ))}
            </div>
          </div>
        )}
        {/* All */}
        <div>
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
            {/* i18n: t.topics.all */}
            All Topics
          </p>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2">
            {others.map((t) => (
              <MainViewCardB key={t.id} topic={t} />
            ))}
            {/* Fake more — implies pagination */}
            {Array.from({ length: 8 }).map((_, i) => (
              <MainViewCardB
                key={`ghost-${i}`}
                topic={{
                  id: `g-${i}`,
                  icon: "Folder",
                  name: [
                    "CI/CD",
                    "Testing",
                    "Compliance",
                    "Redis",
                    "Logging",
                    "Monitoring",
                    "Vendor",
                    "Legal",
                  ][i]!,
                  description: "",
                  lastTouched: `${i + 2}d`,
                }}
              />
            ))}
          </div>
        </div>
      </div>
    );
  }
  // mode c
  const pinned = MAIN_VIEW_MOCK.filter((t) => t.pinned);
  const recent = MAIN_VIEW_MOCK.filter((t) => !t.pinned).slice(0, 3);
  return (
    <div className="rounded-xl border border-border/40 bg-background overflow-hidden">
      {/* Header row — count + ⌘K hint + sort.
          NO search input here; the command bar handles topic filtering
          (rule 25). Was previously a text input; removed. */}
      <div className="px-4 py-3 border-b border-border/30 flex items-center justify-between">
        <span className="text-[12px] text-fg-3 uppercase tracking-wider font-medium">
          {/* i18n: t.topics.count */}
          247 topics
        </span>
        <div className="flex items-center gap-2 text-[11px] text-fg-4">
          <span className="hidden md:inline">
            {/* i18n: t.topics.searchHint */}
            <kbd className="text-[10px] px-1 py-0.5 rounded bg-surface-1 border border-border/40 font-mono">
              ⌘K
            </kbd>{" "}
            to search
          </span>
          <span>recent ▾</span>
        </div>
      </div>
      {/* Pinned cards */}
      {pinned.length > 0 && (
        <div className="px-4 pt-3 pb-2">
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
            {/* i18n: t.topics.pinned */}
            Pinned
          </p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
            {pinned.map((t) => (
              <MainViewCardB key={t.id} topic={t} />
            ))}
          </div>
        </div>
      )}
      {/* Recently visited */}
      <div className="px-4 pt-1 pb-2">
        <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
          Recently visited
        </p>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
          {recent.map((t) => (
            <MainViewCardB key={t.id} topic={t} />
          ))}
        </div>
      </div>
      {/* All topics list */}
      <div>
        <p className="text-[10px] uppercase tracking-wider text-fg-3 px-4 pt-2 pb-1.5">
          All Topics · 247
        </p>
        {MAIN_VIEW_MOCK.map((t) => (
          <MainViewRowC key={t.id} topic={t} />
        ))}
        {/* Ghost rows to imply virtualization */}
        {Array.from({ length: 6 }).map((_, i) => (
          <MainViewRowC
            key={`ghost-${i}`}
            topic={{
              id: `g-${i}`,
              icon: "Folder",
              name: [
                "Cache layers",
                "Queue system",
                "Observability",
                "Vendor decisions",
                "Post-mortems",
                "API contracts",
              ][i]!,
              description: "",
              lastTouched: `${i + 3}d`,
            }}
          />
        ))}
        <div className="px-4 py-2 text-center text-[11px] text-fg-4 border-t border-border/20">
          + 233 more (virtualized)
        </div>
      </div>
    </div>
  );
}

/** ViewToggleMock — segmented control for A/B/C view override.
 *  iOS Control Center pattern: glass pill + sliding active indicator via
 *  framer-motion layoutId shared-layout transition. Spring NORMAL + EASE,
 *  gated by useReducedMotion. Icons-only on mobile, icon+label on desktop.
 *  Matches Kitchen 31 Mode Capsule feel and the 29-pin flow animation
 *  technique — one consistent motion language across the app. */
function ViewToggleMock() {
  const [mode, setMode] = useState<"a" | "b" | "c">("a");
  const reduced = useReducedMotion();
  const options: Array<{
    value: "a" | "b" | "c";
    label: string;
    icon: React.ElementType;
    hint: string;
  }> = [
    {
      value: "a",
      label: "Grid",
      icon: LayoutGrid,
      hint: "Rich grid — small topic set (≤20)",
    },
    {
      value: "b",
      label: "Dense",
      icon: Rows3,
      hint: "Dense grid — medium topic set (20–80)",
    },
    {
      value: "c",
      label: "List",
      icon: List,
      hint: "Virtualized list — large topic set (80+)",
    },
  ];

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-end">
        <div
          role="radiogroup"
          aria-label="Topics view density"
          className="relative inline-flex items-center p-1 rounded-full"
          style={{
            background: "oklch(from var(--card) l c h / 0.6)",
            backdropFilter: "blur(12px) saturate(140%)",
            border: "1px solid oklch(from var(--border) l c h / 0.5)",
          }}
        >
          {options.map((opt) => {
            const Icon = opt.icon;
            const active = mode === opt.value;
            return (
              <button
                key={opt.value}
                type="button"
                role="radio"
                aria-checked={active}
                aria-label={opt.label}
                title={opt.hint}
                onClick={() => setMode(opt.value)}
                className={`relative flex items-center gap-1.5 px-3 py-1.5 rounded-full text-[12px] cursor-pointer transition-colors ${
                  active ? "text-fg-1 font-medium" : "text-fg-3 hover:text-fg-2"
                }`}
              >
                {active && (
                  <motion.span
                    layoutId="view-toggle-indicator"
                    className="absolute inset-0 rounded-full"
                    style={{
                      background: "var(--card)",
                      boxShadow:
                        "0 1px 3px oklch(from var(--foreground) l c h / 0.12)",
                      border: "1px solid oklch(from var(--border) l c h / 0.5)",
                    }}
                    transition={{
                      duration: reduced ? 0 : NORMAL,
                      ease: EASE,
                    }}
                  />
                )}
                <Icon
                  className="h-3.5 w-3.5 relative z-10 shrink-0"
                  strokeWidth={2}
                />
                <span className="hidden md:inline relative z-10">
                  {opt.label}
                </span>
              </button>
            );
          })}
        </div>
      </div>
      <TopicMainViewMock mode={mode} />
    </div>
  );
}

/* ── Pin flow + topic context menu ── */

/** TopicContextMenuMock — the right-click / long-press menu for a topic
 *  card. Three actions: Pin (toggles), Rename, Forget (destructive).
 *  Prod will wrap this in Radix DropdownMenu; the kitchen mock is a fixed
 *  absolute-positioned glass panel driven by parent-controlled state. */
function TopicContextMenuMock({
  anchor,
  pinned,
  onPin,
  onRename,
  onForget,
  onClose,
}: {
  anchor: { x: number; y: number };
  pinned: boolean;
  onPin: () => void;
  onRename: () => void;
  onForget: () => void;
  onClose: () => void;
}) {
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onDocClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  return (
    <motion.div
      ref={menuRef}
      initial={{ opacity: 0, scale: 0.96, y: -4 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      exit={{ opacity: 0, scale: 0.96, y: -4 }}
      transition={{ duration: FAST, ease: EASE }}
      className="absolute z-50 min-w-[180px] rounded-xl overflow-hidden py-1"
      style={{
        left: anchor.x,
        top: anchor.y,
        background: "oklch(from var(--card) l c h / 0.95)",
        backdropFilter: "blur(20px) saturate(140%)",
        border: "1px solid oklch(from var(--border) l c h / 0.5)",
        boxShadow:
          "0 10px 32px -12px oklch(from var(--foreground) l c h / 0.25)",
      }}
      role="menu"
    >
      <button
        type="button"
        role="menuitem"
        onClick={() => {
          onPin();
          onClose();
        }}
        className="w-full flex items-center gap-2.5 px-3 py-1.5 text-[13px] text-fg-1 hover:bg-surface-1 cursor-pointer transition-colors text-left"
      >
        <Pin
          className="h-3.5 w-3.5 text-fg-3"
          style={{
            fill: pinned ? "currentColor" : "none",
            transform: pinned ? "rotate(-45deg)" : "rotate(0deg)",
          }}
        />
        {/* i18n: pinned ? t.topics.unpin : t.topics.pin */}
        <span>{pinned ? "Unpin" : "Pin topic"}</span>
      </button>
      <button
        type="button"
        role="menuitem"
        onClick={() => {
          onRename();
          onClose();
        }}
        className="w-full flex items-center gap-2.5 px-3 py-1.5 text-[13px] text-fg-1 hover:bg-surface-1 cursor-pointer transition-colors text-left"
      >
        <Edit3 className="h-3.5 w-3.5 text-fg-3" />
        {/* i18n: t.topics.rename */}
        <span>Rename</span>
      </button>
      <div className="my-1 h-px bg-border/30 mx-2" />
      <button
        type="button"
        role="menuitem"
        onClick={() => {
          onForget();
          onClose();
        }}
        className="w-full flex items-center gap-2.5 px-3 py-1.5 text-[13px] hover:bg-surface-1 cursor-pointer transition-colors text-left"
        style={{ color: "oklch(0.55 0.2 25)" }}
      >
        <Trash2 className="h-3.5 w-3.5" />
        {/* i18n: t.topics.forget */}
        <span>Forget topic…</span>
      </button>
    </motion.div>
  );
}

/** PinFlowDemo — the interactive showpiece for rule 26.
 *  Uses framer-motion LayoutGroup + layoutId to animate a card moving
 *  between the Pinned and All sections when the user toggles its pin.
 *  Spring transition with NORMAL duration + EASE cubic-bezier. This is
 *  the canonical reference for how the real prod implementation should
 *  wire its shared-layout animation. */
function PinFlowDemo() {
  const [pinnedIds, setPinnedIds] = useState<Set<string>>(
    () => new Set(["engineering"]),
  );
  const togglePin = (id: string) => {
    setPinnedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const topics = MAIN_VIEW_MOCK.slice(0, 8);
  const pinned = topics.filter((t) => pinnedIds.has(t.id));
  const others = topics.filter((t) => !pinnedIds.has(t.id));

  return (
    <LayoutGroup>
      <div className="rounded-xl border border-border/40 bg-background p-4 space-y-4">
        {/* Pinned section — collapses when empty */}
        <AnimatePresence initial={false}>
          {pinned.length > 0 && (
            <motion.div
              key="pinned-section"
              layout
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: NORMAL, ease: EASE }}
              className="overflow-hidden"
            >
              <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5 flex items-center gap-1.5">
                <Pin className="h-3 w-3" style={{ fill: "currentColor" }} />
                {/* i18n: t.topics.pinned */}
                Pinned
              </p>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
                {pinned.map((t) => (
                  <motion.div
                    key={t.id}
                    layout
                    layoutId={`pin-card-${t.id}`}
                    transition={{ duration: NORMAL, ease: EASE }}
                  >
                    <MainViewCardB
                      topic={t}
                      pinned
                      onTogglePin={() => togglePin(t.id)}
                    />
                  </motion.div>
                ))}
              </div>
            </motion.div>
          )}
        </AnimatePresence>
        {/* All section */}
        <motion.div layout transition={{ duration: NORMAL, ease: EASE }}>
          <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
            {/* i18n: t.topics.all */}
            All Topics
          </p>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
            {others.map((t) => (
              <motion.div
                key={t.id}
                layout
                layoutId={`pin-card-${t.id}`}
                transition={{ duration: NORMAL, ease: EASE }}
              >
                <MainViewCardB
                  topic={t}
                  pinned={false}
                  onTogglePin={() => togglePin(t.id)}
                />
              </motion.div>
            ))}
          </div>
        </motion.div>
        <p className="text-[11px] text-fg-4">
          Hover a card → the pin icon appears in the top-right. Click it → the
          card animates up to the Pinned section (or back down on unpin). Shared{" "}
          <span className="font-mono">layoutId</span> + the NORMAL spring
          transition from{" "}
          <span className="font-mono">@memaxlabs/ui/tokens/motion</span>.
        </p>
      </div>
    </LayoutGroup>
  );
}

/** ContextMenuDemo — interactive mock of the long-press / right-click
 *  topic menu. Right-click on desktop, long-press (400ms) on mobile
 *  triggers it. Uses TopicContextMenuMock for the actual menu chrome. */
function ContextMenuDemo() {
  const [menuFor, setMenuFor] = useState<{
    id: string;
    x: number;
    y: number;
  } | null>(null);
  const [pinnedIds, setPinnedIds] = useState<Set<string>>(
    () => new Set(["engineering"]),
  );
  const longPressTimer = useRef<number | null>(null);

  const openMenu = (e: React.MouseEvent, id: string) => {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const parentRect =
      (e.currentTarget as HTMLElement).offsetParent?.getBoundingClientRect() ??
      rect;
    setMenuFor({
      id,
      // Position relative to the list container.
      x: e.clientX - parentRect.left,
      y: e.clientY - parentRect.top,
    });
  };

  const togglePin = (id: string) => {
    setPinnedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const topics = MAIN_VIEW_MOCK.slice(0, 5);

  return (
    <div className="rounded-xl border border-border/40 bg-background p-4 relative">
      <p className="text-[11px] text-fg-3 mb-3">
        {/* i18n: t.topics.contextMenuHint */}
        <span className="hidden md:inline">
          Right-click any row to open the context menu.
        </span>
        <span className="md:hidden">
          Long-press (hold 400ms) any row to open the context menu.
        </span>
      </p>
      <div className="space-y-1">
        {topics.map((t) => (
          <div
            key={t.id}
            onContextMenu={(e) => {
              e.preventDefault();
              openMenu(e, t.id);
            }}
            onTouchStart={(e) => {
              const touch = e.touches[0];
              if (!touch) return;
              const clientX = touch.clientX;
              const clientY = touch.clientY;
              const el = e.currentTarget;
              longPressTimer.current = window.setTimeout(() => {
                const parentRect =
                  el.offsetParent?.getBoundingClientRect() ??
                  el.getBoundingClientRect();
                setMenuFor({
                  id: t.id,
                  x: clientX - parentRect.left,
                  y: clientY - parentRect.top,
                });
              }, 400);
            }}
            onTouchEnd={() => {
              if (longPressTimer.current) {
                clearTimeout(longPressTimer.current);
                longPressTimer.current = null;
              }
            }}
            onTouchMove={() => {
              if (longPressTimer.current) {
                clearTimeout(longPressTimer.current);
                longPressTimer.current = null;
              }
            }}
          >
            <MainViewCardB
              topic={t}
              pinned={pinnedIds.has(t.id)}
              onTogglePin={() => togglePin(t.id)}
            />
          </div>
        ))}
      </div>
      <AnimatePresence>
        {menuFor && (
          <TopicContextMenuMock
            anchor={{ x: menuFor.x, y: menuFor.y }}
            pinned={pinnedIds.has(menuFor.id)}
            onPin={() => togglePin(menuFor.id)}
            onRename={() => {
              // Placeholder — prod wires to the topic rename flow.
            }}
            onForget={() => {
              // Placeholder — prod opens the destructive confirm.
            }}
            onClose={() => setMenuFor(null)}
          />
        )}
      </AnimatePresence>
    </div>
  );
}

/** ExpandDrillDemo — interactive showpiece for rule 28 + 29.
 *  Self-contained mock (doesn't pull NAV_MOCK_* globals). Shows the
 *  dual-tap-target row: chevron toggles in place with framer-motion
 *  height + opacity animation, body button fires drill which stages a
 *  banner at the top to stand in for the §29n morph. */
function ExpandDrillDemo() {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({
    backend: true,
  });
  const [drilledInto, setDrilledInto] = useState<string | null>(null);
  const toggle = (id: string) => setExpanded((p) => ({ ...p, [id]: !p[id] }));
  const drill = (id: string) => setDrilledInto(id);

  const subs: NavSubtopic[] = [
    {
      id: "backend",
      name: "Backend",
      icon: "Server",
      description:
        "Go services, database, queues, cache. Owned by platform team.",
      memoryCount: 142,
      newCount: 0,
      memories: [
        { title: "Go error handling patterns (2026-Q1)", age: "2h" },
        { title: "Structured logging with slog", age: "5h" },
        { title: "Context propagation across goroutines", age: "1d" },
      ],
      children: [
        {
          id: "postgres",
          name: "Postgres",
          icon: "Database",
          description: "Primary OLTP store — migrations, pooling, tuning.",
          memoryCount: 38,
          newCount: 0,
          memories: [
            { title: "Connection pool sizing guide", age: "3h" },
            { title: "pgvector HNSW index tuning", age: "6h" },
          ],
        },
      ],
    },
    {
      id: "frontend",
      name: "Frontend",
      icon: "Layout",
      description:
        "React Server Components, Tailwind, Radix primitives, design tokens.",
      memoryCount: 87,
      newCount: 0,
      memories: [
        { title: "RSC cache tag invalidation", age: "4h" },
        { title: "Streaming suspense boundaries", age: "9h" },
      ],
    },
    {
      id: "security",
      name: "Auth & Security",
      icon: "Shield",
      description:
        "OAuth flows, API key rotation, boundary enforcement, secret scanning.",
      memoryCount: 54,
      newCount: 0,
      memories: [
        { title: "PKCE for native apps — S256 challenge", age: "1h" },
        { title: "Refresh token rotation policy", age: "8h" },
      ],
    },
  ];

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      <AnimatePresence initial={false}>
        {drilledInto && (
          <motion.div
            key="drill-banner"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: NORMAL, ease: EASE }}
            style={{ overflow: "hidden" }}
            className="border-b border-border/30"
          >
            <div className="px-4 py-2.5 flex items-center gap-2 bg-surface-1">
              <ChevronRight
                className="h-3 w-3 text-fg-3"
                style={{ transform: "rotate(180deg)" }}
              />
              <span className="text-[12px] text-fg-2">
                Would rebase container to{" "}
                <span className="text-fg-1 font-medium">{drilledInto}</span> via
                §29n morph.
              </span>
              <button
                type="button"
                onClick={() => setDrilledInto(null)}
                className="ml-auto text-[11px] text-fg-3 hover:text-fg-1 cursor-pointer underline underline-offset-2"
              >
                Reset
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
      {subs.map((sub) => (
        <SubtopicGroup
          key={sub.id}
          sub={sub}
          depth={0}
          expanded={expanded}
          onToggle={toggle}
          onDrill={drill}
        />
      ))}
    </div>
  );
}

/* ── Scale concern mocks: nested recursion, mega-orphans, loading ── */

/** Shows Backend as its own Mode-B dense grid because it has 80 direct
 *  subtopics. Renders a mini breadcrumb + search box + 4-col compact card
 *  grid. Same shape as top-level Mode B — the point is that the SAME
 *  component renders at any drill level. */
function NestedScaleMock() {
  const subtopicNames = [
    "Database",
    "Queue",
    "Cache",
    "API gateway",
    "Observability",
    "Monitoring",
    "Logging",
    "Tracing",
    "Rate limiting",
    "Auth middleware",
    "Feature flags",
    "Billing",
    "Webhooks",
    "Email service",
    "Push notifications",
    "File storage",
  ];
  return (
    <div className="rounded-xl border border-border/40 bg-background overflow-hidden">
      <div className="px-4 py-2 flex items-center gap-1.5 text-[12px] text-fg-3 border-b border-border/20">
        <Folder className="h-3 w-3" />
        <span>Engineering</span>
        <ChevronRight className="h-3 w-3 text-fg-4" />
        <Server className="h-3 w-3" />
        <span className="text-fg-1 font-medium">Backend</span>
      </div>
      <div className="p-4">
        <p className="text-[12px] text-fg-2 mb-2">
          Go services, database, queues, cache. Owned by platform team.
        </p>
        <div className="flex items-center gap-2 mb-3">
          <div className="flex items-center gap-1.5 text-[12px] text-fg-3 bg-surface-1 rounded-md px-2.5 py-1.5 flex-1">
            <Search className="h-3.5 w-3.5 text-fg-4" />
            <span className="text-fg-4">Search 80 subtopics in Backend…</span>
          </div>
          <span className="text-[11px] text-fg-4">recent ▾</span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-2">
          {subtopicNames.map((name, i) => (
            <div
              key={name}
              className="rounded-lg px-3 py-2.5 flex items-center gap-2 cursor-pointer transition-colors hover:bg-surface-1"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <Server className="h-3.5 w-3.5 text-fg-3 shrink-0" />
              <span className="text-[13px] font-medium text-fg-1 truncate flex-1">
                {name}
              </span>
              <span className="text-[11px] text-fg-3 tabular-nums shrink-0">
                {i + 1}d
              </span>
            </div>
          ))}
        </div>
        <div className="mt-3 text-center text-[11px] text-fg-4">
          Show 20 more · 80 total
        </div>
      </div>
    </div>
  );
}

/** Topic with 500+ orphan memories. Orphan section becomes its own
 *  paginated block above the subtopic list. Shows L3 boundary at work. */
/** MegaOrphansMock — interactive orphan pagination.
 *  Unlike subtopics, orphans have no drill target (they belong to the
 *  topic itself), so the escape hatch is search-first mode that takes
 *  over the orphan section in place. When the user types, all orphans
 *  are filtered locally; when empty, the list shows the first 20 with
 *  Show-more + total count. See rule 23. */
function MegaOrphansMock() {
  const allOrphans = Array.from({ length: 527 }, (_, i) => ({
    title: [
      "Webhook retry strategy notes",
      "Postgres connection timeout investigation",
      "River worker memory leak (2026-03)",
      "Redis eviction policy audit",
      "OpenTelemetry collector config",
      "Feature flag rollout protocol",
      "pgbouncer vs built-in pooling tradeoff",
      "S3 → R2 migration checklist",
      "Fly.io machine sizing heuristics",
      "Turbopack tsconfig pitfalls",
    ][i % 10]!,
    age: `${(i % 24) + 1}h`,
  }));

  const [visible, setVisible] = useState(DEFAULT_SUBTOPIC_PAGE_SIZE);
  const [query, setQuery] = useState("");
  const filtered = query
    ? allOrphans.filter((o) =>
        o.title.toLowerCase().includes(query.toLowerCase()),
      )
    : allOrphans;
  const slice = query ? filtered : filtered.slice(0, visible);
  const hasMore = !query && filtered.length > visible;
  const nextSize = Math.min(
    visible + DEFAULT_SUBTOPIC_PAGE_SIZE,
    filtered.length,
  );

  return (
    <div className="rounded-xl border border-border/40 bg-background overflow-hidden">
      <div className="px-4 py-3 border-b border-border/20">
        <div className="flex items-center gap-2">
          <Folder className="h-4 w-4 text-fg-2" />
          <span className="text-[14px] font-medium text-fg-1">
            Platform Ops
          </span>
        </div>
        <p className="text-[12px] text-fg-3 mt-0.5">
          All the things that don&apos;t fit anywhere else yet.
        </p>
      </div>
      <div className="px-4 pt-3 pb-1">
        <div className="flex items-center justify-between mb-2">
          <p className="text-[10px] uppercase tracking-wider text-fg-3">
            {/* i18n: t.topics.directMemories */}
            Direct memories
          </p>
          <span className="text-[10px] text-fg-4 tabular-nums">
            {query
              ? `${filtered.length} match${filtered.length === 1 ? "" : "es"}`
              : `${allOrphans.length} unassigned`}
          </span>
        </div>
        {/* Rule 23: > 80 orphans triggers the local filter input. */}
        <div className="flex items-center gap-1.5 text-[12px] bg-surface-1 rounded-md px-2.5 py-1.5 mb-2">
          <Search className="h-3.5 w-3.5 text-fg-4 shrink-0" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Filter these 527 captures…"
            className="flex-1 bg-transparent outline-none text-fg-1 placeholder:text-fg-4"
          />
        </div>
      </div>
      {slice.slice(0, 6).map((m, i) => (
        <div
          key={`${m.title}-${i}`}
          className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer"
        >
          <FileText className="h-3.5 w-3.5 text-fg-3 shrink-0" />
          <span className="text-[14px] text-fg-1 truncate flex-1">
            {m.title}
          </span>
          <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
            {m.age}
          </span>
        </div>
      ))}
      {/* Footer: Show more for unfiltered, results count for filtered. */}
      {!query && hasMore && (
        <div className="flex items-center gap-3 px-4 border-t border-border/20">
          <button
            type="button"
            onClick={() => setVisible(nextSize)}
            className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
          >
            {/* i18n: t.topics.showMoreOrphans */}
            Show{" "}
            {Math.min(
              DEFAULT_SUBTOPIC_PAGE_SIZE,
              filtered.length - visible,
            )}{" "}
            more
          </button>
          <div className="flex-1" />
          <span className="text-[11px] text-fg-4 tabular-nums">
            {visible} of {allOrphans.length}
          </span>
        </div>
      )}
      {query && filtered.length === 0 && (
        <div className="px-4 py-6 text-center text-[12px] text-fg-3 border-t border-border/20">
          {/* i18n: t.topics.noOrphanMatches */}
          No captures match &ldquo;{query}&rdquo;
        </div>
      )}
      <div className="px-4 pt-4 pb-2 border-t border-border/30">
        <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-1.5">
          {/* i18n: t.topics.subtopics */}
          Subtopics
        </p>
      </div>
      {["Incidents", "Post-mortems", "Runbooks"].map((name) => (
        <div
          key={name}
          className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer"
        >
          <ChevronRight className="h-3 w-3 text-fg-3 shrink-0" />
          <Folder className="h-4 w-4 text-fg-3 shrink-0" />
          <span className="text-[14px] font-medium text-fg-1 flex-1">
            {name}
          </span>
        </div>
      ))}
    </div>
  );
}

/** ShowMoreSubtopicsDemo — interactive L2 pagination demo.
 *  30 subtopics, initial 5 visible, "Show 20 more" appends the next batch
 *  in place via AnimatePresence stagger. Demonstrates rule 18 — L2 is an
 *  independent pagination boundary from L1 (topics) and L3 (memories).
 *  Rule 22: if total > 80, the section would auto-switch to Mode B dense
 *  grid with search-first (demoed by §29-scale-nested). */
function ShowMoreSubtopicsDemo() {
  const allSubtopics = [
    { id: "auth-svc", name: "Auth service", icon: "Shield" },
    { id: "api-gw", name: "API gateway", icon: "Server" },
    { id: "db", name: "Database", icon: "Database" },
    { id: "queue", name: "Queue system", icon: "Zap" },
    { id: "cache", name: "Cache layers", icon: "Database" },
    { id: "obs", name: "Observability", icon: "Cpu" },
    { id: "logging", name: "Logging", icon: "FileText" },
    { id: "tracing", name: "Tracing", icon: "Zap" },
    { id: "metrics", name: "Metrics", icon: "Cpu" },
    { id: "alerting", name: "Alerting", icon: "Shield" },
    { id: "on-call", name: "On-call runbooks", icon: "FileText" },
    { id: "incidents", name: "Incident response", icon: "Shield" },
    { id: "postmortems", name: "Post-mortems", icon: "FileText" },
    { id: "sla", name: "SLA tracking", icon: "Cpu" },
    { id: "capacity", name: "Capacity planning", icon: "Server" },
    { id: "migrations", name: "Migrations", icon: "Database" },
    { id: "backup", name: "Backup & restore", icon: "Database" },
    { id: "dr", name: "Disaster recovery", icon: "Shield" },
    { id: "secrets", name: "Secrets management", icon: "Shield" },
    { id: "iam", name: "IAM policies", icon: "Shield" },
    { id: "compliance", name: "Compliance audits", icon: "FileText" },
    { id: "vendors", name: "Vendor contracts", icon: "FileText" },
    { id: "budget", name: "Budget & cost", icon: "Cpu" },
    { id: "tooling", name: "Internal tooling", icon: "Server" },
    { id: "ci", name: "CI pipeline", icon: "Zap" },
    { id: "cd", name: "CD & deploys", icon: "Rocket" },
    { id: "rollback", name: "Rollback playbooks", icon: "FileText" },
    { id: "feature-flags", name: "Feature flags", icon: "Zap" },
    { id: "perf", name: "Performance testing", icon: "Cpu" },
    { id: "security", name: "Security reviews", icon: "Shield" },
  ];

  const INITIAL = 5;
  const PAGE = 20;
  const [visible, setVisible] = useState(INITIAL);
  const reduced = useReducedMotion();
  const slice = allSubtopics.slice(0, visible);
  const hasMore = visible < allSubtopics.length;
  const isExpanded = visible > INITIAL;

  return (
    <div className="rounded-xl border border-border/40 bg-background overflow-hidden">
      <div className="px-4 py-3 border-b border-border/20 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-fg-2" />
          <span className="text-[14px] font-medium text-fg-1">Backend</span>
        </div>
        <span className="text-[11px] text-fg-4 tabular-nums">
          {/* i18n: t.topics.subtopicCount (interpolated) */}
          {allSubtopics.length} subtopics
        </span>
      </div>
      <AnimatePresence initial={false}>
        {slice.map((sub, i) => {
          const Icon = TOPIC_ICON_MAP[sub.icon] ?? FileText;
          return (
            <motion.div
              key={sub.id}
              initial={
                i < INITIAL || reduced ? false : { opacity: 0, height: 0 }
              }
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{
                duration: reduced ? 0 : NORMAL,
                ease: EASE,
                delay: reduced ? 0 : i < INITIAL ? 0 : (i - INITIAL) * 0.02,
              }}
              style={{ overflow: "hidden" }}
            >
              <div className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer">
                <ChevronRight className="h-3 w-3 text-fg-3 shrink-0" />
                <Icon className="h-4 w-4 text-fg-2 shrink-0" />
                <span className="text-[14px] text-fg-1 truncate flex-1">
                  {sub.name}
                </span>
              </div>
            </motion.div>
          );
        })}
      </AnimatePresence>
      {(hasMore || isExpanded) && (
        <div className="flex items-center gap-3 px-4 border-t border-border/20">
          {hasMore && (
            <button
              type="button"
              onClick={() =>
                setVisible(Math.min(visible + PAGE, allSubtopics.length))
              }
              className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
            >
              {/* i18n: t.topics.showMoreSubtopics */}
              Show {Math.min(PAGE, allSubtopics.length - visible)} more
              subtopics
            </button>
          )}
          {isExpanded && !hasMore && (
            <button
              type="button"
              onClick={() => setVisible(INITIAL)}
              className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
            >
              Collapse
            </button>
          )}
          <div className="flex-1" />
          <span className="text-[11px] text-fg-4 tabular-nums">
            {visible} of {allSubtopics.length}
          </span>
        </div>
      )}
    </div>
  );
}

/** Loading skeletons for the 3 pagination boundaries, side-by-side. */
function LoadingSkeletonMock() {
  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
      <div className="rounded-xl border border-border/40 bg-background p-3">
        <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
          L1 · topic card skeletons
        </p>
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <div
              key={i}
              className="rounded-lg p-3 space-y-1.5"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex items-center gap-2">
                <div className="h-4 w-4 rounded bg-surface-2 animate-pulse" />
                <div className="h-3 w-24 rounded bg-surface-2 animate-pulse" />
              </div>
              <div className="h-2.5 w-full rounded bg-surface-2 animate-pulse opacity-70" />
              <div className="h-2.5 w-2/3 rounded bg-surface-2 animate-pulse opacity-70" />
            </div>
          ))}
        </div>
      </div>
      <div className="rounded-xl border border-border/40 bg-background p-3">
        <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
          L2 · subtopic header skeletons
        </p>
        <div className="space-y-0">
          {Array.from({ length: 4 }).map((_, i) => (
            <div
              key={i}
              className="flex items-center gap-2 px-2 py-2.5 border-t border-border/20 first:border-t-0"
            >
              <div className="h-3 w-3 rounded bg-surface-2 animate-pulse" />
              <div className="h-4 w-4 rounded bg-surface-2 animate-pulse" />
              <div className="h-3 flex-1 rounded bg-surface-2 animate-pulse" />
            </div>
          ))}
        </div>
      </div>
      <div className="rounded-xl border border-border/40 bg-background p-3">
        <p className="text-[10px] uppercase tracking-wider text-fg-3 mb-2">
          L3 · memory row skeletons
        </p>
        <div className="space-y-0">
          {Array.from({ length: 5 }).map((_, i) => (
            <div
              key={i}
              className="flex items-center gap-2 px-2 py-2 border-t border-border/20 first:border-t-0"
            >
              <div className="h-3.5 w-3.5 rounded bg-surface-2 animate-pulse" />
              <div
                className="h-3 rounded bg-surface-2 animate-pulse"
                style={{ width: `${60 + (i % 3) * 15}%` }}
              />
              <div className="flex-1" />
              <div className="h-2.5 w-6 rounded bg-surface-2 animate-pulse" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/* ── Recall → Topic Bridge Row ── */

function TopicBridgeRow({
  icon: Icon,
  name,
  matchCount,
}: {
  icon: React.ElementType;
  name: string;
  matchCount: number;
}) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 hover:bg-surface-1 transition-colors cursor-pointer">
      <Icon className="h-3 w-3 text-fg-3 shrink-0" />
      <span className="text-[13px] font-medium text-fg-2">{name}</span>
      <span className="text-[11px] text-fg-4 ml-0.5">
        ({matchCount} matches)
      </span>
      <ChevronRight className="h-3 w-3 text-fg-4 ml-auto shrink-0" />
    </div>
  );
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * 29h — Topic navigation (inline collapsible subtopic groups)
 *
 * Core model:
 *   • One page per topic, one scrolling container
 *   • Subtopics render as inline collapsible groups, not separate pages
 *   • Size class drives default collapse state, not page shape:
 *       small (≤20) / medium (20–100) → open by default
 *       large (100–500) / huge (500+) → closed by default
 *   • Orphan memories render unlabeled at the top of every level
 *   • NO auto-expand, NO delta counts, NO ✦ badges, NO row accents —
 *     topic navigation is silent about dreams (rule 1). The newCount /
 *     isNew fields on the mocks below are kitchen-only legacy; prod does
 *     not carry them and renderers must not read them. When this file
 *     migrates to prod, drop those fields entirely.
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

interface NavMemory {
  title: string;
  age: string;
  isNew?: boolean;
  /** First-initial avatar for team hub attribution. Undefined = personal hub. */
  author?: string;
  /** Content type — rule 33: split is doc vs non-doc. Doc-like types
   *  (text, pdf, markdown, code) render with NO icon — they're reading
   *  content, behaviorally identical from a scanning perspective.
   *  Non-doc types (image, link) render a small trailing icon at
   *  text-fg-4 before the age column. Default is "text" when undefined. */
  content_type?: "text" | "image" | "pdf" | "link";
}

/** MemoryContentTypeBadge — trailing icon for non-doc memory rows.
 *  Rule 33 — the split is doc vs non-doc, not text vs everything:
 *  - Doc-like (no badge): text, pdf, markdown, code — reading content.
 *    PDFs are documents you read, same behavioral category as text.
 *  - Non-doc (trailing badge): image (visual), link (external
 *    reference). These are behaviorally different from reading content
 *    — scanning for "that screenshot" or "that link" is a different
 *    search than scanning for "that decision I wrote down".
 *  Renders small (h-3 w-3) at text-fg-4 so non-doc types stand out
 *  precisely because they're rare. */
function MemoryContentTypeBadge({
  type,
}: {
  type?: NavMemory["content_type"];
}) {
  if (type !== "image" && type !== "link") return null;
  const Icon = type === "image" ? ImageIcon : LinkIcon;
  return (
    <Icon
      className="h-3 w-3 text-fg-4 shrink-0"
      strokeWidth={1.8}
      aria-hidden
    />
  );
}

interface NavSubtopicActivity {
  /** Memories added in the recent window, across all descendants. */
  count: number;
  /** Window size (e.g. "7 days"). */
  days: number;
  /** Contributors in that window, first-initial avatars. */
  authors: string[];
}

interface NavSubtopic {
  id: string;
  name: string;
  /** Lucide icon name. Resolved via TOPIC_ICON_MAP. Maps to Topic.Icon in prod. */
  icon?: string;
  description?: string; // optional — subtopics use the same Topic model and can carry a description
  memoryCount: number; // total in this subtopic (incl. nested)
  newCount: number;
  memories?: NavMemory[];
  children?: NavSubtopic[]; // nested subtopics
  /** Team hub only — "recently touched in this subtree" header context. */
  recentActivity?: NavSubtopicActivity;
}

/** Where to show subtopic descriptions.
 *  - "none": never render (current baseline, fastest scan, loses dream-engine context)
 *  - "always": render under name at every depth, collapsed or expanded (2-line header)
 *  - "expanded": render only when the subtopic is open (collapsed stays compact)
 *  - "expanded-shallow": render only when open AND depth === 0 (recommended default)
 */
type DescriptionStrategy = "none" | "always" | "expanded" | "expanded-shallow";

interface NavTopicMock {
  name: string;
  /** Lucide icon name. Resolved via TOPIC_ICON_MAP. Maps to Topic.Icon in prod. */
  icon?: string;
  description: string;
  memoryCount: number;
  subtopicCount: number;
  directMemories: NavMemory[];
  subtopics: NavSubtopic[];
  /** Hidden memories beyond what's loaded (for virtualization demo) */
  hiddenCount?: number;
}

/** Size class → default expand state.
 *  small/medium: everything open. large/huge: subtopics closed by default,
 *  except subtopics with newCount > 0 which auto-expand on first visit. */
function computeDefaultExpanded(
  topic: NavTopicMock,
  sizeClass: "small" | "medium" | "large" | "huge",
): Record<string, boolean> {
  const result: Record<string, boolean> = {};
  const walk = (subs: NavSubtopic[]) => {
    for (const s of subs) {
      if (sizeClass === "small" || sizeClass === "medium") {
        result[s.id] = true;
      } else {
        result[s.id] = s.newCount > 0;
      }
      if (s.children) walk(s.children);
    }
  };
  walk(topic.subtopics);
  return result;
}

/** SubtopicMemoryList — progressive-disclosure list of memories inside an
 *  expanded subtopic. Default load = 20, "Show N more" expands by 20, cap
 *  at actual list length. If the caller passes a pre-filtered (search)
 *  array, pagination is disabled (search results render all matches).
 *
 *  Maps to prod: cursor-paginated fetch
 *    GET /v1/topics/:id/memories?subtopic_id=X&cursor=Y&limit=20
 *  The kitchen uses a simple client-side slice to demonstrate the UX;
 *  production replaces `memories.slice(0, visible)` with a useQuery
 *  driven cursor list. */
const DEFAULT_SUBTOPIC_PAGE_SIZE = 20;

/** Inline-expand escape hatch threshold — see rule 30. A subtopic with
 *  more memories than this should NOT offer "Show more" inline; the
 *  only paginated affordance is "Open full view →" which drills. */
const INLINE_EXPAND_THRESHOLD = 100;

function SubtopicMemoryList({
  memories,
  indentPx,
  keyPrefix,
  isSearchResult = false,
  onDrill,
  subtopicId,
  subtopicName,
}: {
  memories?: NavMemory[];
  indentPx: number;
  keyPrefix: string;
  isSearchResult?: boolean;
  /** Drill into this subtopic — wired from parent SubtopicGroup. When
   *  provided, the escape-hatch "Open full view →" renders. */
  onDrill?: (id: string) => void;
  subtopicId?: string;
  subtopicName?: string;
}) {
  const [visible, setVisible] = useState(DEFAULT_SUBTOPIC_PAGE_SIZE);
  if (!memories || memories.length === 0) return null;

  // Search results render all matches (no pagination within search).
  const slice = isSearchResult ? memories : memories.slice(0, visible);
  const hasMore = !isSearchResult && memories.length > visible;
  const isExpanded = !isSearchResult && visible > DEFAULT_SUBTOPIC_PAGE_SIZE;
  const nextSize = Math.min(
    visible + DEFAULT_SUBTOPIC_PAGE_SIZE,
    memories.length,
  );
  // Rule 30: over threshold, skip Show-more entirely. Only "Open full view →".
  const overThreshold = memories.length > INLINE_EXPAND_THRESHOLD;
  const canDrill = !!(onDrill && subtopicId);

  return (
    <>
      {slice.map((m, i) => (
        <div
          key={`${keyPrefix}-m-${i}`}
          className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer"
          style={{ paddingLeft: `${indentPx}px` }}
        >
          {/* Rule 32: memory row is text-[14px] text-fg-1 regular —
              same size as topic rows but regular weight. Topic rows
              fade (text-fg-2) when closed or commit (font-medium
              text-fg-1) when open, so memory rows naturally sit
              between those two states. Rule 33: no leading content
              icon for text; rare types get a small trailing badge
              before age. Rule 34: no summary line. */}
          <span className="text-[14px] text-fg-1 flex-1 truncate">
            {m.title}
          </span>
          <MemoryContentTypeBadge type={m.content_type} />
          <span className="text-[12px] text-fg-3 tabular-nums">{m.age}</span>
        </div>
      ))}
      {(hasMore || (isExpanded && !hasMore) || overThreshold) &&
        !isSearchResult && (
          <div
            className="flex items-center gap-3 border-t border-border/20"
            style={{ paddingLeft: `${indentPx}px`, paddingRight: 16 }}
          >
            {/* Show more — hidden when over threshold (rule 30) */}
            {hasMore && !overThreshold && (
              <button
                type="button"
                onClick={() => setVisible(nextSize)}
                className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
              >
                {/* i18n: t.topics.showMore */}
                Show{" "}
                {Math.min(
                  DEFAULT_SUBTOPIC_PAGE_SIZE,
                  memories.length - visible,
                )}{" "}
                more
              </button>
            )}
            {isExpanded && !hasMore && !overThreshold && (
              <button
                type="button"
                onClick={() => setVisible(DEFAULT_SUBTOPIC_PAGE_SIZE)}
                className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
              >
                {/* i18n: t.topics.collapse */}
                Collapse
              </button>
            )}
            <div className="flex-1" />
            {/* Open full view → escape hatch (rule 30) */}
            {canDrill && (hasMore || overThreshold) && (
              <button
                type="button"
                onClick={() => onDrill!(subtopicId!)}
                className="flex items-center gap-1 text-[12px] text-fg-2 hover:text-fg-1 py-2 cursor-pointer transition-colors font-medium"
                title={`Open ${subtopicName ?? "subtopic"} in full view`}
              >
                {/* i18n: overThreshold ? t.topics.openFullViewWithCount : t.topics.openFullView */}
                <span>
                  Open full view
                  {overThreshold && (
                    <span className="text-fg-4 ml-1 tabular-nums font-normal">
                      ({memories.length} memories)
                    </span>
                  )}
                </span>
                <ArrowUpRight className="h-3 w-3" />
              </button>
            )}
          </div>
        )}
    </>
  );
}

/** Recursive subtopic group — renders header + (when expanded) its memories
 *  + nested child subtopics. Indent increases with depth (capped at 3). */
/** SubtopicGroup — dual-tap-target row.
 *  Left: chevron button (expand/collapse in place). Right: body button
 *  (drill into this subtopic, rebasing the container via §29n morph).
 *  Expanded content animates with framer-motion AnimatePresence +
 *  useReducedMotion fallback. See rules 28 + 29. */
function SubtopicGroup({
  sub,
  depth,
  expanded,
  onToggle,
  onDrill,
  search,
  descriptionStrategy = "expanded-shallow",
}: {
  sub: NavSubtopic;
  depth: number;
  expanded: Record<string, boolean>;
  onToggle: (id: string) => void;
  /** Drill into this subtopic (rebase the container). Optional — static
   *  kitchen mocks don't wire drills. Prod topic view always wires it. */
  onDrill?: (id: string) => void;
  search?: string;
  descriptionStrategy?: DescriptionStrategy;
}) {
  const isOpen = expanded[sub.id] ?? false;
  const indentPx = Math.min(depth, 2) * 16;
  const reduced = useReducedMotion();

  // In search mode, auto-expand if any descendant matches
  const hasMatchingDescendant =
    search && sub.memories
      ? sub.memories.some((m) =>
          m.title.toLowerCase().includes(search.toLowerCase()),
        )
      : false;
  const effectivelyOpen = isOpen || hasMatchingDescendant;

  const filteredMemories = search
    ? sub.memories?.filter((m) =>
        m.title.toLowerCase().includes(search.toLowerCase()),
      )
    : sub.memories;

  const showDescription = (() => {
    if (!sub.description) return false;
    if (descriptionStrategy === "always") return true;
    if (descriptionStrategy === "expanded") return effectivelyOpen;
    if (descriptionStrategy === "expanded-shallow")
      return effectivelyOpen && depth === 0;
    return false;
  })();

  return (
    <>
      {/* Subtopic header row — two tap targets.
          items-start so the chevron button keeps its intrinsic height at
          the top of the row even when the body grows (description render).
          Otherwise items-stretch + items-center would re-center the
          chevron and shift it down on expand. */}
      <div
        className="flex items-start border-t border-border/20 hover:bg-surface-1 transition-colors"
        style={{ paddingLeft: `${indentPx}px` }}
      >
        <button
          type="button"
          onClick={() => onToggle(sub.id)}
          aria-expanded={effectivelyOpen}
          aria-label={
            effectivelyOpen ? `Collapse ${sub.name}` : `Expand ${sub.name}`
          }
          className="flex items-center pl-4 pr-2 py-2.5 cursor-pointer text-fg-3 hover:text-fg-1 transition-colors shrink-0"
          style={{
            height: 40 /* matches body row height before description */,
          }}
        >
          <ChevronRight
            className="h-3 w-3 shrink-0"
            style={{
              transform: effectivelyOpen ? "rotate(90deg)" : "rotate(0deg)",
              transformOrigin: "center",
              transition: reduced ? "none" : `transform ${FAST}s ${EASE}`,
            }}
          />
        </button>
        <button
          type="button"
          onClick={() => onDrill?.(sub.id)}
          aria-label={`Open ${sub.name}`}
          className="flex-1 text-left py-2.5 pr-4 cursor-pointer min-w-0"
        >
          <div className="flex items-center gap-2">
            <TopicIcon name={sub.icon} size={16} />
            {/* Rule 32: topic row fades as scaffolding when closed
                (text-fg-2), commits when open (font-medium text-fg-1).
                Memory rows stay text-fg-1 regular always — content is
                king, chrome is background. Hierarchy signal comes from
                open/close contrast + leading TopicIcon presence (rule
                33 drops leading icon on text memories) + indentation. */}
            <span
              className={`text-[14px] ${
                effectivelyOpen ? "font-medium text-fg-1" : "text-fg-2"
              } truncate flex-1`}
            >
              {sub.name}
            </span>
          </div>
          {showDescription && (
            <div
              className="text-[12px] font-normal text-fg-3 truncate mt-0.5"
              style={{ paddingLeft: 24 /* icon (16) + gap-2 (8) */ }}
            >
              {sub.description}
            </div>
          )}
        </button>
      </div>

      {/* Expanded content — animated height + opacity, children compose.
          No vertical rail: indentation + topic-row open/close contrast
          + leading icon rhythm (rule 33) already carry the hierarchy.
          Memax DNA is "no dividers, content-led" — don't add decorative
          chrome when the typography + state contrast already convey
          structure. Rule 5 caps inline at 2 levels desktop / 1 mobile. */}
      <AnimatePresence initial={false}>
        {effectivelyOpen && (
          <motion.div
            key={`${sub.id}-content`}
            initial={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            animate={reduced ? { opacity: 1 } : { height: "auto", opacity: 1 }}
            exit={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: NORMAL, ease: EASE }}
            style={{ overflow: "hidden" }}
          >
            <SubtopicMemoryList
              memories={filteredMemories}
              indentPx={36 + indentPx}
              keyPrefix={sub.id}
              isSearchResult={!!search}
              onDrill={onDrill}
              subtopicId={sub.id}
              subtopicName={sub.name}
            />
            {sub.children?.map((child) => (
              <SubtopicGroup
                key={child.id}
                sub={child}
                depth={depth + 1}
                expanded={expanded}
                onToggle={onToggle}
                onDrill={onDrill}
                search={search}
                descriptionStrategy={descriptionStrategy}
              />
            ))}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

/** Full topic detail page with inline collapsible subtopics.
 *  Renders the full shape: breadcrumb → header → meta → memory list. */
function TopicNavigationMock({
  topic,
  sizeClass,
  interactive = true,
  searchable = false,
  descriptionStrategy = "expanded-shallow",
  onDrill,
}: {
  topic: NavTopicMock;
  sizeClass: "small" | "medium" | "large" | "huge";
  interactive?: boolean;
  searchable?: boolean;
  descriptionStrategy?: DescriptionStrategy;
  /** Drill into a subtopic (rebase the container). Optional — most
   *  static kitchen mocks don't wire drills; the 29-expand interactive
   *  demo does. */
  onDrill?: (id: string) => void;
}) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>(() =>
    computeDefaultExpanded(topic, sizeClass),
  );
  const [search, setSearch] = useState("");

  const toggle = (id: string) => {
    if (!interactive) return;
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{ background: "var(--background)" }}
    >
      <div className="px-6 pt-6 pb-6 max-w-3xl mx-auto">
        {/* Breadcrumb */}
        <div className="flex items-center gap-1.5 text-[12px] text-fg-3 mb-4">
          <span className="hover:text-fg-2 cursor-pointer">Your Topics</span>
          <ChevronRight className="h-3 w-3 text-fg-4" />
          <span className="text-fg-2">{topic.name}</span>
        </div>

        {/* Focus header — icon + name + description. NO memoryCount /
            subtopicCount / "levels deep" stats here; the list below is
            the signal. */}
        <div className="flex items-start gap-3 mb-1">
          <TopicIcon
            name={topic.icon}
            size={28}
            className="text-fg-2 shrink-0 mt-1"
          />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-3">
              <span
                className="text-[21px] font-bold text-foreground"
                style={{ letterSpacing: "-0.02em" }}
              >
                {topic.name}
              </span>
              <div className="flex-1" />
              {searchable && (
                <div
                  className="flex items-center gap-1.5 text-[12px] text-fg-3 bg-surface-1 rounded-md px-2 py-1"
                  style={{ minWidth: 160 }}
                >
                  <Search className="h-3 w-3 text-fg-4" />
                  <input
                    type="text"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="Search in topic"
                    className="bg-transparent outline-none flex-1 text-[12px] text-fg-1 placeholder:text-fg-4"
                  />
                </div>
              )}
            </div>
            <p className="text-[14px] text-fg-2 mt-1">{topic.description}</p>
          </div>
        </div>

        {/* Memory list — one container, inline groups */}
        <div
          className="mt-6 rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          {/* Orphan memories — direct children of the topic, unlabeled
              at top. Rule 32: text-[14px] text-fg-1 regular (content is
              king, chrome is background). Rule 33: trailing content
              badge for non-text. Rule 34: no summary line. */}
          {topic.directMemories
            .filter(
              (m) =>
                !search || m.title.toLowerCase().includes(search.toLowerCase()),
            )
            .map((m, i) => (
              <div
                key={`direct-${i}`}
                className={`flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer ${i > 0 ? "border-t border-border/20" : ""}`}
              >
                <span className="text-[14px] text-fg-1 flex-1 truncate">
                  {m.title}
                </span>
                <MemoryContentTypeBadge type={m.content_type} />
                <span className="text-[12px] text-fg-3 tabular-nums">
                  {m.age}
                </span>
              </div>
            ))}

          {/* Subtopic groups */}
          {topic.subtopics.map((sub) => (
            <SubtopicGroup
              key={sub.id}
              sub={sub}
              depth={0}
              expanded={expanded}
              onToggle={toggle}
              onDrill={onDrill}
              search={searchable && search ? search : undefined}
              descriptionStrategy={descriptionStrategy}
            />
          ))}

          {/* Virtualization fallback — huge topic */}
          {topic.hiddenCount && topic.hiddenCount > 0 && (
            <div className="px-4 py-3 border-t border-border/20 text-center text-[12px] text-fg-4">
              + {topic.hiddenCount} more (virtualized — only visible rows
              render)
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/* ── Mock data for each size class ── */

const NAV_MOCK_SMALL: NavTopicMock = {
  name: "Team Docs",
  icon: "Folder",
  description: "Onboarding, code review, release process.",
  memoryCount: 8,
  subtopicCount: 0,
  directMemories: [
    { title: "Onboarding checklist", age: "2d" },
    { title: "Code review expectations", age: "3d" },
    { title: "Release process — beta", age: "4d" },
    { title: "Incident response runbook", age: "1w" },
    { title: "Architecture decision log template", age: "2w" },
    { title: "Git workflow note", age: "3w" },
    { title: "Pairing session guide", age: "1mo" },
    { title: "Shipping checklist", age: "1mo" },
  ],
  subtopics: [],
};

const NAV_MOCK_MEDIUM: NavTopicMock = {
  name: "Frontend",
  icon: "Layout",
  description: "React Server Components, Tailwind, Radix primitives.",
  memoryCount: 40,
  subtopicCount: 3,
  directMemories: [
    { title: "Tailwind 4 migration notes", age: "3h", isNew: true },
    { title: "Radix vs base-ui primitive comparison", age: "1d" },
  ],
  subtopics: [
    {
      id: "rsc",
      name: "RSC patterns",
      icon: "Code",
      description:
        "Server actions, cache tags, suspense boundaries, and the use-client divide.",
      memoryCount: 14,
      newCount: 3,
      memories: [
        { title: "Server action error handling", age: "4h", isNew: true },
        { title: "RSC cache tag invalidation", age: "6h", isNew: true },
        { title: "Streaming suspense boundaries", age: "9h", isNew: true },
        { title: "Use client vs use server — decision tree", age: "2d" },
        { title: "Form actions without useFormState", age: "3d" },
        { title: "Passing server data to client components", age: "4d" },
      ],
    },
    {
      id: "animation",
      name: "Motion",
      icon: "Zap",
      description:
        "Spring easing tokens, layout animations, and the transform-only perf budget.",
      memoryCount: 12,
      newCount: 0,
      memories: [
        { title: "Spring easing tokens — memax set", age: "1w" },
        { title: "Framer Motion layout shared elements", age: "2w" },
        { title: "Entrance animation patterns", age: "3w" },
        { title: "GPU-friendly transform-only rules", age: "1mo" },
      ],
    },
    {
      id: "state",
      name: "State",
      icon: "Database",
      description:
        "TanStack Query hydration, Zustand slice design, and cross-surface cache invalidation.",
      memoryCount: 12,
      newCount: 0,
      memories: [
        { title: "TanStack Query cache hydration", age: "1w" },
        { title: "Zustand slice patterns", age: "2w" },
      ],
    },
  ],
};

const NAV_MOCK_LARGE: NavTopicMock = {
  name: "Auth & Security",
  icon: "Shield",
  description:
    "JWT tokens, OAuth flows, API key management, boundary enforcement.",
  memoryCount: 150,
  subtopicCount: 6,
  directMemories: [
    {
      title: "Security review Q1 decisions",
      age: "1h",
      isNew: true,
      content_type: "pdf",
    },
    { title: "Threat model draft — new endpoints", age: "4h", isNew: true },
  ],
  subtopics: [
    {
      id: "oauth",
      name: "OAuth",
      description:
        "PKCE, refresh token rotation, state parameter hygiene, and consent flow bugs.",
      memoryCount: 32,
      newCount: 3,
      memories: [
        { title: "PKCE for native apps", age: "3h", isNew: true },
        {
          title: "RFC 7636 — official PKCE spec",
          age: "4h",
          content_type: "link",
        },
        { title: "Refresh token rotation bug", age: "5h", isNew: true },
        {
          title: "OAuth consent flow screenshot",
          age: "6h",
          content_type: "image",
        },
        { title: "State parameter audit", age: "7h", isNew: true },
        { title: "OIDC nonce validation", age: "1d" },
        { title: "Authorization code flow notes", age: "3d" },
      ],
    },
    {
      id: "api-keys",
      name: "API keys",
      description:
        "Key rotation policies, scoped token design, mTLS, and HSM-backed signing.",
      memoryCount: 24,
      newCount: 2,
      memories: [
        { title: "Key rotation policy v3", age: "4h", isNew: true },
        { title: "Scoped token design", age: "6h", isNew: true },
        {
          title: "Auth0 API key rotation guide",
          age: "1d",
          content_type: "link",
        },
        { title: "mTLS client cert rotation", age: "2d" },
        { title: "HSM-backed signing keys", age: "1w" },
      ],
    },
    {
      id: "sessions",
      name: "Session management",
      description:
        "JWT validation middleware, session fixation defense, idle timeout strategy.",
      memoryCount: 18,
      newCount: 0,
      memories: [
        { title: "JWT validation middleware", age: "1w" },
        { title: "Session fixation fix note", age: "2w" },
      ],
    },
    {
      id: "csrf",
      name: "CSRF & CORS",
      description:
        "Double-submit tokens, SameSite cookies, and preflight cache tuning.",
      memoryCount: 15,
      newCount: 0,
    },
    {
      id: "rate-limit",
      name: "Rate limiting",
      description:
        "Token bucket vs leaky bucket, per-user vs per-IP, Redis backoff tuning.",
      memoryCount: 12,
      newCount: 0,
    },
    {
      id: "audit",
      name: "Audit logging",
      description:
        "Immutable audit trail, PII redaction rules, compliance retention windows.",
      memoryCount: 9,
      newCount: 0,
    },
  ],
};

const NAV_MOCK_HUGE: NavTopicMock = {
  name: "Backend Archive",
  icon: "Server",
  description:
    "Historical engineering notes — 3 years of decisions, migrations, and post-mortems.",
  memoryCount: 612,
  subtopicCount: 12,
  directMemories: [],
  subtopics: [
    { id: "db", name: "Database", memoryCount: 142, newCount: 0 },
    { id: "queue", name: "Queue system", memoryCount: 87, newCount: 0 },
    {
      id: "cache",
      name: "Cache layers",
      memoryCount: 64,
      newCount: 2,
      memories: [
        { title: "Redis memory pressure alerts", age: "5h", isNew: true },
        { title: "LRU vs LFU decision", age: "8h", isNew: true },
      ],
    },
    { id: "obs", name: "Observability", memoryCount: 58, newCount: 0 },
    { id: "deploy", name: "Deployment", memoryCount: 54, newCount: 0 },
    { id: "infra", name: "Infrastructure", memoryCount: 47, newCount: 0 },
    {
      id: "sec-archive",
      name: "Security archive",
      memoryCount: 43,
      newCount: 0,
    },
    { id: "migrations", name: "Migrations", memoryCount: 38, newCount: 0 },
    { id: "post-mortems", name: "Post-mortems", memoryCount: 29, newCount: 0 },
    { id: "vendor", name: "Vendor decisions", memoryCount: 24, newCount: 0 },
    { id: "api", name: "API contracts", memoryCount: 15, newCount: 0 },
    { id: "legacy", name: "Legacy systems", memoryCount: 11, newCount: 0 },
  ],
  hiddenCount: 600,
};

const NAV_MOCK_DEEP: NavTopicMock = {
  name: "Ingest Pipeline",
  icon: "Cpu",
  description: "Content ingestion, chunking, embedding, deduplication.",
  memoryCount: 42,
  subtopicCount: 2,
  directMemories: [{ title: "Pipeline architecture diagram", age: "2d" }],
  subtopics: [
    {
      id: "chunking",
      name: "Chunking",
      description:
        "Format-aware splitting across markdown, PDF, and code — paragraph and heading boundaries.",
      memoryCount: 18,
      newCount: 2,
      memories: [
        { title: "Markdown chunker edge case", age: "3h", isNew: true },
        { title: "Semantic paragraph boundaries", age: "5h", isNew: true },
      ],
      children: [
        {
          id: "chunking-markdown",
          name: "Markdown",
          description:
            "Heading-anchored chunks, code block preservation, table strategy.",
          memoryCount: 8,
          newCount: 1,
          memories: [
            { title: "Headings as chunk boundaries", age: "7h", isNew: true },
            { title: "Code block preservation", age: "2d" },
            { title: "Table chunking strategy", age: "1w" },
          ],
        },
        {
          id: "chunking-pdf",
          name: "PDF",
          description:
            "Layout-preserving extraction and figure-to-caption linkage.",
          memoryCount: 6,
          newCount: 0,
          memories: [
            { title: "Layout-preserving extraction", age: "1w" },
            { title: "Figure caption linkage", age: "2w" },
          ],
        },
      ],
    },
    {
      id: "embedding",
      name: "Embedding",
      description:
        "Voyage AI rate limits, batch size tuning, and dimension trade-offs.",
      memoryCount: 23,
      newCount: 0,
      memories: [
        { title: "Voyage AI rate limits", age: "1w" },
        { title: "Batch size tuning", age: "2w" },
      ],
    },
  ],
};

const NAV_MOCK_SEARCH: NavTopicMock = NAV_MOCK_LARGE;

/* ════════════════════════════════════════════════════════════════
   29j — Team hub deep navigation
   ════════════════════════════════════════════════════════════════

   Team hubs are Memax's focus. They accumulate far more memory,
   far more subtopics, and far deeper trees than personal hubs.
   Depth 3-5 is legitimate (real team knowledge structure), not
   a dream-engine mistake.

   This section shows the full e2e drill-in flow: breadcrumb as
   first-class navigation, ⌘P picker for direct subtopic jumps,
   row attribution, recent activity context, and a hub-overview
   flat-2 mode for team onboarding.
   ════════════════════════════════════════════════════════════════ */

const TEAM_HUB_ENGINEERING: NavTopicMock = {
  name: "Engineering",
  icon: "Hammer",
  description:
    "All engineering knowledge — backend, frontend, infra, data, security.",
  memoryCount: 2147,
  subtopicCount: 48,
  directMemories: [
    {
      title: "Engineering principles (2026 refresh)",
      age: "3d",
      author: "D",
    },
    { title: "Weekly eng sync notes", age: "5d", author: "A" },
  ],
  subtopics: [
    {
      id: "backend",
      name: "Backend",
      icon: "Server",
      description:
        "Go services, database, queues, cache. Owned by platform team.",
      memoryCount: 842,
      newCount: 14,
      recentActivity: {
        count: 31,
        days: 7,
        authors: ["A", "B", "C", "D"],
      },
      memories: [
        {
          title: "Service decomposition plan",
          age: "2h",
          isNew: true,
          author: "A",
        },
        {
          title: "Gateway timeout audit",
          age: "6h",
          isNew: true,
          author: "C",
        },
      ],
      children: [
        {
          id: "database",
          name: "Database",
          icon: "Database",
          description:
            "Postgres primary, MySQL legacy, Redis cache — schemas, migrations, replication.",
          memoryCount: 412,
          newCount: 9,
          recentActivity: {
            count: 18,
            days: 7,
            authors: ["A", "B", "C"],
          },
          memories: [
            {
              title: "Schema convention v4",
              age: "4h",
              isNew: true,
              author: "A",
            },
            {
              title: "Index naming policy",
              age: "9h",
              isNew: true,
              author: "B",
            },
          ],
          children: [
            {
              id: "postgres",
              name: "Postgres",
              icon: "Database",
              description:
                "Our primary OLTP store. Migrations, replication, query tuning, HA.",
              memoryCount: 287,
              newCount: 8,
              recentActivity: {
                count: 14,
                days: 7,
                authors: ["A", "B", "C"],
              },
              memories: [
                {
                  title: "Connection pool sizing guide",
                  age: "5h",
                  isNew: true,
                  author: "A",
                },
                {
                  title: "pgvector index tuning",
                  age: "12h",
                  isNew: true,
                  author: "C",
                },
              ],
              children: [
                {
                  id: "migrations",
                  name: "Migrations",
                  icon: "Cpu",
                  description:
                    "Zero-downtime patterns, rollback, version control, backfills.",
                  memoryCount: 142,
                  newCount: 6,
                  recentActivity: {
                    count: 8,
                    days: 7,
                    authors: ["A", "B"],
                  },
                  memories: [
                    {
                      title: "NOT NULL backfill checklist",
                      age: "3h",
                      isNew: true,
                      author: "A",
                    },
                    {
                      title: "Shadow table pattern — full guide",
                      age: "7h",
                      isNew: true,
                      author: "B",
                    },
                  ],
                  children: [
                    {
                      id: "zero-downtime",
                      name: "Zero-downtime patterns",
                      description:
                        "Shadow tables, expand/contract, dual-write, backfill windows.",
                      memoryCount: 58,
                      newCount: 4,
                      recentActivity: {
                        count: 4,
                        days: 7,
                        authors: ["A", "B"],
                      },
                      memories: [
                        {
                          title: "Expand/contract schema migration",
                          age: "4h",
                          isNew: true,
                          author: "A",
                        },
                        {
                          title: "Dual-write invariant checks",
                          age: "8h",
                          isNew: true,
                          author: "B",
                        },
                        {
                          title: "Backfill window sizing",
                          age: "2d",
                          author: "A",
                        },
                      ],
                    },
                    {
                      id: "rollback",
                      name: "Rollback strategies",
                      description:
                        "Immediate rollback windows, snapshot restore, forward-fix policy.",
                      memoryCount: 42,
                      newCount: 2,
                      memories: [
                        {
                          title: "Rollback decision tree",
                          age: "6h",
                          isNew: true,
                          author: "D",
                        },
                      ],
                    },
                    {
                      id: "version-control",
                      name: "Version control",
                      description:
                        "Migration file naming, ordering, sqlc integration.",
                      memoryCount: 42,
                      newCount: 0,
                    },
                  ],
                },
                {
                  id: "replication",
                  name: "Replication",
                  description:
                    "Streaming + logical replication, lag monitoring, failover.",
                  memoryCount: 78,
                  newCount: 2,
                  memories: [
                    {
                      title: "Replication lag alerting",
                      age: "1d",
                      isNew: true,
                      author: "C",
                    },
                  ],
                  children: [
                    {
                      id: "streaming",
                      name: "Streaming replication",
                      memoryCount: 42,
                      newCount: 1,
                    },
                    {
                      id: "logical",
                      name: "Logical replication",
                      memoryCount: 36,
                      newCount: 1,
                    },
                  ],
                },
                {
                  id: "query-opt",
                  name: "Query optimization",
                  description:
                    "EXPLAIN ANALYZE, index selection, query rewrites.",
                  memoryCount: 67,
                  newCount: 0,
                },
              ],
            },
            {
              id: "mysql",
              name: "MySQL (legacy)",
              description:
                "Legacy OLTP — to be deprecated Q3 2026. Freeze new development.",
              memoryCount: 73,
              newCount: 0,
              memories: [
                {
                  title: "InnoDB buffer pool sizing",
                  age: "1mo",
                  author: "B",
                },
              ],
            },
            {
              id: "redis",
              name: "Redis",
              description: "Cache layer, session store, rate-limit counters.",
              memoryCount: 52,
              newCount: 1,
              memories: [
                {
                  title: "Eviction policy trade-offs",
                  age: "8h",
                  isNew: true,
                  author: "C",
                },
              ],
            },
          ],
        },
        {
          id: "queue",
          name: "Queue system",
          description:
            "River (Postgres-backed) — job processors, retry, dead letter.",
          memoryCount: 187,
          newCount: 3,
          memories: [
            {
              title: "River job priority tuning",
              age: "6h",
              isNew: true,
              author: "B",
            },
          ],
          children: [
            {
              id: "river",
              name: "River internals",
              memoryCount: 98,
              newCount: 2,
              children: [
                {
                  id: "job-processors",
                  name: "Job processors",
                  memoryCount: 54,
                  newCount: 1,
                },
                {
                  id: "retry-policies",
                  name: "Retry policies",
                  memoryCount: 44,
                  newCount: 1,
                },
              ],
            },
            {
              id: "dlq",
              name: "Dead letter queue",
              memoryCount: 32,
              newCount: 0,
            },
          ],
        },
        {
          id: "cache-layers",
          name: "Cache layers",
          description:
            "Memcached → Redis migration, invalidation rules, stampede defense.",
          memoryCount: 124,
          newCount: 2,
          memories: [
            {
              title: "Stampede defense — singleflight",
              age: "9h",
              isNew: true,
              author: "A",
            },
          ],
        },
      ],
    },
    {
      id: "frontend",
      name: "Frontend",
      icon: "Layout",
      description:
        "Next.js, React Server Components, Tailwind, design system. Owned by web team.",
      memoryCount: 612,
      newCount: 7,
      recentActivity: {
        count: 12,
        days: 7,
        authors: ["D", "E", "F"],
      },
      children: [
        {
          id: "rsc",
          name: "RSC patterns",
          description:
            "Server actions, cache tags, streaming suspense boundaries.",
          memoryCount: 142,
          newCount: 4,
        },
        {
          id: "design-system",
          name: "Design system",
          description:
            "Liquid glass surfaces, motion tokens, memax brand language.",
          memoryCount: 218,
          newCount: 3,
        },
        {
          id: "state-mgmt",
          name: "State management",
          description: "TanStack Query, Zustand, cache invalidation patterns.",
          memoryCount: 87,
          newCount: 0,
        },
      ],
    },
    {
      id: "infra",
      name: "Infrastructure",
      icon: "Rocket",
      description:
        "Fly.io deployments, Vercel, GitHub Actions, secrets management.",
      memoryCount: 287,
      newCount: 4,
    },
    {
      id: "security",
      name: "Security",
      icon: "Shield",
      description:
        "Auth, OAuth, API keys, boundary enforcement, secret scanning.",
      memoryCount: 198,
      newCount: 5,
      children: [
        {
          id: "oauth",
          name: "OAuth",
          description:
            "PKCE, refresh rotation, consent flow bugs — see also Security > Audit.",
          memoryCount: 78,
          newCount: 2,
          children: [
            {
              id: "pkce",
              name: "PKCE",
              description:
                "PKCE for native + mobile apps, S256 challenge method.",
              memoryCount: 24,
              newCount: 1,
              memories: [
                {
                  title: "PKCE for mobile — Derek's note",
                  age: "4h",
                  isNew: true,
                  author: "D",
                },
              ],
            },
          ],
        },
        {
          id: "api-keys",
          name: "API keys",
          description: "Rotation policy, scoped tokens, mTLS.",
          memoryCount: 45,
          newCount: 1,
        },
      ],
    },
    {
      id: "data",
      name: "Data platform",
      icon: "Database",
      description: "Warehouse, pipelines, analytics — owned by data team.",
      memoryCount: 208,
      newCount: 0,
    },
  ],
};

/* ── Drill-navigation components ── */

/** Walk a NavSubtopic[] tree along a path of IDs, returning the node at the
 *  end of the path plus the display names for each segment. Returns null if
 *  the path breaks (shouldn't happen with valid UI state). */
function walkDrillPath(
  topic: NavTopicMock,
  path: string[],
): {
  currentSubs: NavSubtopic[];
  currentDirectMemories: NavMemory[];
  currentName: string;
  currentIcon?: string;
  currentDescription?: string;
  currentActivity?: NavSubtopicActivity;
  pathNames: string[];
  currentMemoryCount: number;
  currentNewCount: number;
} | null {
  const pathNames: string[] = [topic.name];
  let currentSubs: NavSubtopic[] = topic.subtopics;
  let currentDirectMemories: NavMemory[] = topic.directMemories;
  let currentName = topic.name;
  let currentIcon: string | undefined = topic.icon;
  let currentDescription: string | undefined = topic.description;
  let currentActivity: NavSubtopicActivity | undefined;
  let currentMemoryCount = topic.memoryCount;
  let currentNewCount =
    topic.directMemories.filter((m) => m.isNew).length +
    topic.subtopics.reduce((sum, s) => sum + s.newCount, 0);

  for (const id of path) {
    const next = currentSubs.find((s) => s.id === id);
    if (!next) return null;
    pathNames.push(next.name);
    currentSubs = next.children ?? [];
    currentDirectMemories = next.memories ?? [];
    currentName = next.name;
    currentIcon = next.icon;
    currentDescription = next.description;
    currentActivity = next.recentActivity;
    currentMemoryCount = next.memoryCount;
    currentNewCount = next.newCount;
  }

  return {
    currentSubs,
    currentDirectMemories,
    currentName,
    currentIcon,
    currentDescription,
    currentActivity,
    pathNames,
    currentMemoryCount,
    currentNewCount,
  };
}

/** Sticky breadcrumb. Each segment is clickable and navigates to that drill level. */
function DrillBreadcrumb({
  pathNames,
  onNavigate,
  compact = false,
}: {
  pathNames: string[];
  onNavigate: (newPath: string[]) => void;
  /** Mobile compact mode — shows "← {parent}" + full path collapsed into a menu. */
  compact?: boolean;
}) {
  if (compact && pathNames.length > 2) {
    const parent = pathNames[pathNames.length - 2];
    return (
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 bg-background/80 backdrop-blur-md">
        <button
          type="button"
          onClick={() => onNavigate(pathNames.slice(0, -2).map(() => ""))}
          className="flex items-center gap-1 text-[13px] text-fg-2 hover:text-fg-1 cursor-pointer"
        >
          <ChevronLeft className="h-4 w-4" />
          {parent}
        </button>
        <div className="flex-1" />
        <span className="text-[11px] text-fg-4 truncate max-w-[140px]">
          {pathNames.slice(0, -1).join(" › ")}
        </span>
      </div>
    );
  }

  return (
    <div
      className="sticky top-0 z-[2] flex items-center gap-1.5 text-[12px] text-fg-3 px-4 py-3 flex-wrap bg-background/80 backdrop-blur-md border-b border-border/30"
      style={{ marginBottom: 0 }}
    >
      <span className="text-fg-4">Your Topics</span>
      {pathNames.map((name, i) => {
        const isLast = i === pathNames.length - 1;
        return (
          <span key={i} className="flex items-center gap-1.5">
            <ChevronRight className="h-3 w-3 text-fg-4" />
            <button
              type="button"
              onClick={() => onNavigate(Array(i).fill(""))}
              className={`cursor-pointer hover:text-fg-1 transition-colors ${
                isLast ? "text-fg-1 font-medium" : "text-fg-3"
              } truncate max-w-[180px]`}
            >
              {name}
            </button>
          </span>
        );
      })}
    </div>
  );
}

/** Row that represents a subtopic beyond the inline depth cap. Clicking it
 *  triggers a drill-in: the list rebases to this subtopic as the new root. */
function DrillInRow({
  sub,
  depth,
  onDrill,
}: {
  sub: NavSubtopic;
  depth: number;
  onDrill: () => void;
}) {
  const indentPx = Math.min(depth, 2) * 16;

  return (
    <button
      type="button"
      onClick={onDrill}
      className="w-full text-left px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 transition-colors cursor-pointer group"
      style={{ paddingLeft: `${16 + indentPx}px` }}
    >
      <div className="flex items-center gap-2">
        {/* Spacer where the toggle chevron would be — keeps column alignment with expandable rows. */}
        <div className="w-3 shrink-0" />
        <TopicIcon name={sub.icon} size={14} />
        <span className="text-[14px] text-fg-1 truncate flex-1">
          {sub.name}
        </span>
        <ChevronRight className="h-4 w-4 text-fg-3 group-hover:text-fg-1 transition-colors" />
      </div>
      {sub.description && (
        <div
          className="text-[12px] text-fg-3 truncate mt-0.5"
          style={{
            paddingLeft: 40 /* spacer (12) + gap-2 (8) + icon (14) + gap-2 (8) */,
          }}
        >
          {sub.description}
        </div>
      )}
    </button>
  );
}

/** Tiny avatar cluster for team hub attribution. */
function AuthorCluster({
  authors,
  size = 18,
}: {
  authors: string[];
  size?: number;
}) {
  return (
    <div className="flex items-center" style={{ gap: -6 }}>
      {authors.slice(0, 4).map((a, i) => (
        <div
          key={i}
          className="rounded-full flex items-center justify-center text-[10px] font-medium text-fg-1 border border-background"
          style={{
            width: size,
            height: size,
            background: `oklch(0.75 0.08 ${(a.charCodeAt(0) * 13) % 360})`,
            marginLeft: i === 0 ? 0 : -6,
          }}
          title={a}
        >
          {a}
        </div>
      ))}
      {authors.length > 4 && (
        <span className="text-[11px] text-fg-3 ml-1">
          +{authors.length - 4}
        </span>
      )}
    </div>
  );
}

/** Row rendering for an individual memory in team hub mode — with author avatar. */
function TeamMemoryRow({
  memory,
  indentPx,
}: {
  memory: NavMemory;
  indentPx: number;
}) {
  return (
    <div
      className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer"
      style={{ paddingLeft: `${indentPx}px` }}
    >
      {/* Rule 32: text-[14px] text-fg-1 regular, same as personal
          memory rows. Author avatar is the attribution signal on team
          hubs, appearing trailing (before content badge + age). */}
      <span className="text-[14px] text-fg-1 flex-1 truncate">
        {memory.title}
      </span>
      <MemoryContentTypeBadge type={memory.content_type} />
      {memory.author && (
        <div
          className="shrink-0 rounded-full flex items-center justify-center text-[10px] font-medium text-fg-1"
          style={{
            width: 18,
            height: 18,
            background: `oklch(0.75 0.08 ${(memory.author.charCodeAt(0) * 13) % 360})`,
          }}
          title={`by ${memory.author}`}
        >
          {memory.author}
        </div>
      )}
      <span className="text-[12px] text-fg-3 tabular-nums">{memory.age}</span>
    </div>
  );
}

/** Team hub drill subtopic group — like SubtopicGroup but respects depthCap.
 *  When relative depth reaches cap, children render as drill-in rows instead
 *  of inline expansion. */
/** DrillSubtopicGroup — team-hub variant with drill-in chips beyond cap.
 *  Same dual-tap-target pattern as SubtopicGroup: chevron toggles in
 *  place, body drills. See rules 28 + 29. */
function DrillSubtopicGroup({
  sub,
  depth,
  depthCap,
  onDrill,
  expanded,
  onToggle,
  expandAll,
}: {
  sub: NavSubtopic;
  depth: number;
  depthCap: number; // max depth index that still inlines (0 = only top, 1 = top+1, etc.)
  onDrill: (id: string) => void;
  expanded: Record<string, boolean>;
  onToggle: (id: string) => void;
  expandAll?: boolean;
}) {
  const isOpen = expandAll || (expanded[sub.id] ?? true);
  const indentPx = Math.min(depth, 2) * 16;
  const hasChildren = (sub.children?.length ?? 0) > 0;
  const reduced = useReducedMotion();

  return (
    <>
      <div
        className="flex items-start border-t border-border/20 hover:bg-surface-1 transition-colors"
        style={{ paddingLeft: `${indentPx}px` }}
      >
        <button
          type="button"
          onClick={() => onToggle(sub.id)}
          aria-expanded={isOpen}
          aria-label={isOpen ? `Collapse ${sub.name}` : `Expand ${sub.name}`}
          className="flex items-center pl-4 pr-2 py-2.5 cursor-pointer text-fg-3 hover:text-fg-1 transition-colors shrink-0"
          style={{ height: 40 /* fixed header row height */ }}
        >
          <ChevronRight
            className="h-3 w-3 shrink-0"
            style={{
              transform: isOpen ? "rotate(90deg)" : "rotate(0deg)",
              transformOrigin: "center",
              transition: reduced ? "none" : `transform ${FAST}s ${EASE}`,
            }}
          />
        </button>
        <button
          type="button"
          onClick={() => onDrill(sub.id)}
          aria-label={`Open ${sub.name}`}
          className="flex-1 text-left py-2.5 pr-4 cursor-pointer min-w-0"
        >
          <div className="flex items-center gap-2">
            <TopicIcon name={sub.icon} size={16} />
            {/* Rule 32: same model as SubtopicGroup — fade when closed,
                commit when open. No semibold override. */}
            <span
              className={`text-[14px] ${
                isOpen ? "font-medium text-fg-1" : "text-fg-2"
              } truncate flex-1`}
            >
              {sub.name}
            </span>
          </div>
          {isOpen && sub.description && (
            <div
              className="text-[12px] text-fg-3 truncate mt-0.5"
              style={{ paddingLeft: 24 /* icon 16 + gap-2 8 */ }}
            >
              {sub.description}
            </div>
          )}
        </button>
      </div>

      <AnimatePresence initial={false}>
        {isOpen && (
          <motion.div
            key={`${sub.id}-content`}
            initial={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            animate={reduced ? { opacity: 1 } : { height: "auto", opacity: 1 }}
            exit={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: NORMAL, ease: EASE }}
            style={{ overflow: "hidden" }}
          >
            <PaginatedMemoryList
              memories={sub.memories}
              indentPx={36 + indentPx}
              keyPrefix={sub.id}
              renderRow={(m, key) => (
                <TeamMemoryRow key={key} memory={m} indentPx={36 + indentPx} />
              )}
              onDrill={onDrill}
              subtopicId={sub.id}
              subtopicName={sub.name}
            />
            {hasChildren &&
              sub.children!.map((child) => {
                if (!expandAll && depth + 1 > depthCap) {
                  return (
                    <DrillInRow
                      key={child.id}
                      sub={child}
                      depth={depth + 1}
                      onDrill={() => onDrill(child.id)}
                    />
                  );
                }
                return (
                  <DrillSubtopicGroup
                    key={child.id}
                    sub={child}
                    depth={depth + 1}
                    depthCap={depthCap}
                    onDrill={onDrill}
                    expanded={expanded}
                    onToggle={onToggle}
                    expandAll={expandAll}
                  />
                );
              })}
          </motion.div>
        )}
      </AnimatePresence>
    </>
  );
}

/** PaginatedMemoryList — generic version of SubtopicMemoryList. Takes a
 *  renderRow prop so callers can use TeamMemoryRow (team hub) or plain
 *  rows (personal). Same escape-hatch semantics as SubtopicMemoryList:
 *  Show more up to INLINE_EXPAND_THRESHOLD, then only "Open full view →". */
function PaginatedMemoryList({
  memories,
  indentPx,
  keyPrefix,
  renderRow,
  isSearchResult = false,
  onDrill,
  subtopicId,
  subtopicName,
}: {
  memories?: NavMemory[];
  indentPx: number;
  keyPrefix: string;
  renderRow: (memory: NavMemory, key: string) => React.ReactNode;
  isSearchResult?: boolean;
  onDrill?: (id: string) => void;
  subtopicId?: string;
  subtopicName?: string;
}) {
  const [visible, setVisible] = useState(DEFAULT_SUBTOPIC_PAGE_SIZE);
  if (!memories || memories.length === 0) return null;

  const slice = isSearchResult ? memories : memories.slice(0, visible);
  const hasMore = !isSearchResult && memories.length > visible;
  const isExpanded = !isSearchResult && visible > DEFAULT_SUBTOPIC_PAGE_SIZE;
  const nextSize = Math.min(
    visible + DEFAULT_SUBTOPIC_PAGE_SIZE,
    memories.length,
  );
  const overThreshold = memories.length > INLINE_EXPAND_THRESHOLD;
  const canDrill = !!(onDrill && subtopicId);

  return (
    <>
      {slice.map((m, i) => renderRow(m, `${keyPrefix}-m-${i}`))}
      {(hasMore || (isExpanded && !hasMore) || overThreshold) &&
        !isSearchResult && (
          <div
            className="flex items-center gap-3 border-t border-border/20"
            style={{ paddingLeft: `${indentPx}px`, paddingRight: 16 }}
          >
            {hasMore && !overThreshold && (
              <button
                type="button"
                onClick={() => setVisible(nextSize)}
                className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
              >
                {/* i18n: t.topics.showMore */}
                Show{" "}
                {Math.min(
                  DEFAULT_SUBTOPIC_PAGE_SIZE,
                  memories.length - visible,
                )}{" "}
                more
              </button>
            )}
            {isExpanded && !hasMore && !overThreshold && (
              <button
                type="button"
                onClick={() => setVisible(DEFAULT_SUBTOPIC_PAGE_SIZE)}
                className="text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors"
              >
                {/* i18n: t.topics.collapse */}
                Collapse
              </button>
            )}
            <div className="flex-1" />
            {canDrill && (hasMore || overThreshold) && (
              <button
                type="button"
                onClick={() => onDrill!(subtopicId!)}
                className="flex items-center gap-1 text-[12px] text-fg-2 hover:text-fg-1 py-2 cursor-pointer transition-colors font-medium"
                title={`Open ${subtopicName ?? "subtopic"} in full view`}
              >
                <span>
                  Open full view
                  {overThreshold && (
                    <span className="text-fg-4 ml-1 tabular-nums font-normal">
                      ({memories.length} memories)
                    </span>
                  )}
                </span>
                <ArrowUpRight className="h-3 w-3" />
              </button>
            )}
          </div>
        )}
    </>
  );
}

/** Activity context strip under the header — "Last 7 days: +18 by @A @B @C". */
function RecentActivityStrip({ activity }: { activity: NavSubtopicActivity }) {
  return (
    <div className="flex items-center gap-2 mt-2 text-[12px] text-fg-3">
      <span className="tabular-nums">
        Last {activity.days} days · +{activity.count}
      </span>
      <span className="text-fg-4">·</span>
      <AuthorCluster authors={activity.authors} size={18} />
    </div>
  );
}

/** The full team-hub drill navigation surface. Holds drillPath state,
 *  renders breadcrumb + header + list, and re-renders at the drilled level. */
function DrillNavigationMock({
  topic,
  depthCap = 1,
  initialPath = [],
  interactive = true,
  mobile = false,
  expandAll = false,
}: {
  topic: NavTopicMock;
  /** Max relative depth that inlines. 1 = top+1 (desktop), 0 = top only (mobile). */
  depthCap?: number;
  /** Initial drill path (IDs). */
  initialPath?: string[];
  interactive?: boolean;
  mobile?: boolean;
  expandAll?: boolean;
}) {
  const [drillPath, setDrillPath] = useState<string[]>(initialPath);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const walked = walkDrillPath(topic, drillPath);
  if (!walked) return null;

  const {
    currentSubs,
    currentDirectMemories,
    currentName,
    currentIcon,
    currentDescription,
    currentActivity,
    pathNames,
  } = walked;

  const toggle = (id: string) => {
    if (!interactive) return;
    setExpanded((prev) => ({ ...prev, [id]: !(prev[id] ?? true) }));
  };

  const drill = (id: string) => {
    if (!interactive) return;
    setDrillPath((prev) => [...prev, id]);
    setExpanded({});
  };

  const navigate = (newPath: string[]) => {
    if (!interactive) return;
    // newPath length tells us how many segments to keep (segments beyond
    // topic root). We always keep topic root (pathNames[0]).
    setDrillPath(drillPath.slice(0, newPath.length));
    setExpanded({});
  };

  return (
    <div
      className="rounded-xl overflow-hidden relative"
      style={{ background: "var(--background)" }}
    >
      <DrillBreadcrumb
        pathNames={pathNames}
        onNavigate={navigate}
        compact={mobile}
      />

      <div className={`px-6 ${mobile ? "pt-4" : "pt-6"} pb-6`}>
        {/* Focus header — icon + name + description. No memory/subtopic
            count stats — the list below is the signal. Team-hub recent
            activity strip renders below as collaboration context. */}
        <div className="flex items-start gap-3 mb-1">
          <TopicIcon
            name={currentIcon}
            size={mobile ? 22 : 28}
            className="text-fg-2 shrink-0 mt-1"
          />
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-3">
              <span
                className={`${mobile ? "text-[18px]" : "text-[21px]"} font-bold text-foreground`}
                style={{ letterSpacing: "-0.02em" }}
              >
                {currentName}
              </span>
              <div className="flex-1" />
              {/* ⌘P hint removed — topic-scoped search is the command bar's
                  job now (see §24m). No separate picker hotkey in the
                  topic header. */}
            </div>
            {currentDescription && (
              <p className="text-[13px] text-fg-2 mt-1">{currentDescription}</p>
            )}
          </div>
        </div>

        {currentActivity && <RecentActivityStrip activity={currentActivity} />}

        {/* Memory list */}
        <div
          className="mt-5 rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          {currentDirectMemories.map((m, i) => (
            <div
              key={`direct-${i}`}
              className={`${i > 0 ? "border-t border-border/20" : ""}`}
            >
              <TeamMemoryRow memory={m} indentPx={16} />
            </div>
          ))}

          {currentSubs.map((sub) => {
            if (!expandAll && depthCap < 0) {
              return (
                <DrillInRow
                  key={sub.id}
                  sub={sub}
                  depth={0}
                  onDrill={() => drill(sub.id)}
                />
              );
            }
            return (
              <DrillSubtopicGroup
                key={sub.id}
                sub={sub}
                depth={0}
                depthCap={depthCap}
                onDrill={drill}
                expanded={expanded}
                onToggle={toggle}
                expandAll={expandAll}
              />
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** SubtopicPickerMock REMOVED — moved to §24 (bar-redesign) as topic-scoped
 *  recall northstar. The subtopic picker modal duplicated the global command
 *  bar; the bar's recall mode should handle topic-aware fuzzy search. */

/** PaginationDemo — generates a 50-memory fake subtopic and wires it to
 *  PaginatedMemoryList so the kitchen can demonstrate load-more / collapse
 *  without needing a real backend. */
function PaginationDemo() {
  const memories: NavMemory[] = Array.from({ length: 50 }, (_, i) => ({
    title: [
      "Server action error handling",
      "RSC cache tag invalidation",
      "Streaming suspense boundaries",
      "Use client vs use server — decision tree",
      "Form actions without useFormState",
      "Passing server data to client components",
      "Parallel route caching edges",
      "Revalidate on-demand with tag API",
      "Partial prerender notes",
      "Server components and third-party libs",
    ][i % 10]!,
    age: `${Math.floor(i / 2) + 1}d`,
  }));

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      <div className="flex items-center gap-2 px-4 py-2.5 bg-surface-1/50">
        <ChevronDown className="h-3 w-3 text-fg-3 shrink-0" />
        <TopicIcon name="Code" size={16} />
        <span className="text-[14px] font-medium text-fg-1 truncate">
          RSC patterns (50 memories)
        </span>
        <span className="text-[12px] text-fg-4 tabular-nums ml-auto">50</span>
      </div>
      <PaginatedMemoryList
        memories={memories}
        indentPx={36}
        keyPrefix="pagination-demo"
        renderRow={(m, key) => (
          <div
            key={key}
            className="flex items-center gap-2 px-4 py-2.5 border-t border-border/20 hover:bg-surface-1 cursor-pointer"
            style={{ paddingLeft: 36 }}
          >
            <span className="text-[14px] text-fg-1 flex-1 truncate">
              {m.title}
            </span>
            <MemoryContentTypeBadge type={m.content_type} />
            <span className="text-[12px] text-fg-3 tabular-nums">{m.age}</span>
          </div>
        )}
      />
    </div>
  );
}

/** MobileSubtopicPickerSheet REMOVED — moved to §24 as mobile counterpart of
 *  the bar-redesign topic-scoped recall northstar. */

/** AnimatedDrillDemo — demonstrates the drill navigation animation rule.
 *  Container stays put; the content region inside the container slides
 *  + cross-fades based on direction. Forward = new content enters from
 *  +16px translateX; backward = from -16px. Duration NORMAL (0.2s),
 *  easing EASE (spring cubic-bezier).
 *
 *  This is the canonical drill-in animation reference. When production
 *  wires it up, replace the setState with real drillPath and use the same
 *  motion values. */
function AnimatedDrillDemo() {
  const [level, setLevel] = useState(0);
  const [direction, setDirection] = useState<1 | -1>(1);

  const levels = [
    {
      icon: "Hammer",
      name: "Engineering",
      description:
        "All engineering knowledge — backend, frontend, infra, data, security.",
      crumbs: ["Engineering"],
    },
    {
      icon: "Server",
      name: "Backend",
      description:
        "Go services, database, queues, cache. Owned by platform team.",
      crumbs: ["Engineering", "Backend"],
    },
    {
      icon: "Database",
      name: "Database",
      description:
        "Postgres primary, MySQL legacy, Redis cache — schemas, migrations, replication.",
      crumbs: ["Engineering", "Backend", "Database"],
    },
    {
      icon: "Database",
      name: "Postgres",
      description:
        "Our primary OLTP store. Migrations, replication, query tuning, HA.",
      crumbs: ["Engineering", "Backend", "Database", "Postgres"],
    },
    {
      icon: "Cpu",
      name: "Migrations",
      description:
        "Zero-downtime patterns, rollback, version control, backfills.",
      crumbs: ["Engineering", "Backend", "Database", "Postgres", "Migrations"],
    },
  ];

  const current = levels[level];

  const drill = () => {
    if (level < levels.length - 1) {
      setDirection(1);
      setLevel(level + 1);
    }
  };
  const back = () => {
    if (level > 0) {
      setDirection(-1);
      setLevel(level - 1);
    }
  };

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2 text-[11px] text-fg-3">
        <button
          type="button"
          onClick={back}
          disabled={level === 0}
          className="px-2 py-1 rounded-md bg-surface-1 hover:bg-surface-2 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer transition-colors"
        >
          ← back
        </button>
        <button
          type="button"
          onClick={drill}
          disabled={level === levels.length - 1}
          className="px-2 py-1 rounded-md bg-surface-1 hover:bg-surface-2 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer transition-colors"
        >
          drill →
        </button>
        <span className="text-fg-4 ml-2">
          Level {level + 1} of {levels.length}
        </span>
      </div>

      {/* Container — stays put. Only inner content animates. */}
      <div
        className="rounded-xl overflow-hidden relative"
        style={{
          border: "1px solid var(--border)",
          background: "var(--card)",
          minHeight: 180,
        }}
      >
        {/* Breadcrumb */}
        <div className="flex items-center gap-1.5 text-[12px] text-fg-3 px-5 pt-4 flex-wrap">
          <span className="text-fg-4">Your Topics</span>
          <AnimatePresence mode="popLayout" initial={false}>
            {current.crumbs.map((crumb, i) => {
              const isLast = i === current.crumbs.length - 1;
              return (
                <motion.span
                  key={`${level}-${i}-${crumb}`}
                  layout
                  initial={{ opacity: 0, x: direction * 8 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: -direction * 8 }}
                  transition={{ duration: NORMAL, ease: EASE }}
                  className="flex items-center gap-1.5"
                >
                  <ChevronRight className="h-3 w-3 text-fg-4" />
                  <span
                    className={isLast ? "text-fg-1 font-medium" : "text-fg-3"}
                  >
                    {crumb}
                  </span>
                </motion.span>
              );
            })}
          </AnimatePresence>
        </div>

        {/* Focus header — icon + name + description. Cross-fades + slides
             when level changes. */}
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={level}
            initial={{ opacity: 0, x: direction * 16 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: -direction * 16 }}
            transition={{ duration: NORMAL, ease: EASE }}
            className="px-5 pt-3 pb-5"
          >
            <div className="flex items-start gap-3">
              <TopicIcon
                name={current.icon}
                size={28}
                className="text-fg-2 shrink-0 mt-1"
              />
              <div className="flex-1 min-w-0">
                <p
                  className="text-[21px] font-bold text-foreground"
                  style={{ letterSpacing: "-0.02em" }}
                >
                  {current.name}
                </p>
                <p className="text-[13px] text-fg-2 mt-1">
                  {current.description}
                </p>
              </div>
            </div>
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}

/** MemoryRowOpenDemo REMOVED — 29o now documents existing prod push-in/out
 *  mobile transition + Radix dialog desktop modal. Codex should wire the
 *  existing prod transition into topic navigation, not reimplement. */

export function TopicRedesignSection() {
  return (
    <Section
      title="29. Topic Redesign"
      description="Topics are the spatial browse surface. Three-tier topic cards, recall→topic bridge, 5-level drill navigation with inline subtopic groups, ⌘P picker, and orphan-memory handling at every depth. Dream activity lives in §36 Inbox — topic cards and topic detail are silent (no ✦ badges, no row accents, no delta headers). When the dream engine flags something that needs a decision, it surfaces as a review in Inbox, never here."
    >
      {/* ════════════════════════════════════════════════
          A. Topics main view — scale-aware 3 modes
          ════════════════════════════════════════════════ */}
      <DemoCard label="29a. Topics main view — scale-aware 3 modes">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">One component.</span> The
            Topics page renders a single scale-aware view that picks its own
            density based on <span className="font-mono">topicCount</span>. The
            user never switches manually unless they want to override.
          </p>
          <p>
            <span className="text-fg-1 font-medium">
              Recursive at every drilled level.
            </span>{" "}
            The same A/B/C mode logic applies inside a topic too — if a topic
            has 200 direct subtopics, that level renders in Mode B (dense list),
            not Mode A (rich grid). Whatever scope you&apos;re in (hub root or 5
            levels deep), the renderer asks the same question: how many children
            at this level?
          </p>
          <ul className="text-fg-3 text-[12px] pl-4 space-y-0.5 list-disc">
            <li>
              <span className="text-fg-1 font-medium">Mode A — Personal</span>{" "}
              (≤20 topics) — 3-col rich cards with description + 2-3 subtopic
              chips
            </li>
            <li>
              <span className="text-fg-1 font-medium">Mode B — Team</span>{" "}
              (20-80) — 4-col dense cards, name + last-touched only, search bar
              at top + Pinned section
            </li>
            <li>
              <span className="text-fg-1 font-medium">Mode C — Enterprise</span>{" "}
              (80+) — virtualized list, single row per topic, search-first +
              Pinned + Recently visited at top
            </li>
          </ul>
          <p className="text-fg-3 text-[12px]">
            Thresholds 20 / 80 are starting values — tune from real usage. Each
            demo below is the same component fed a different topic count.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="29a-mode-a. Mode A — Personal grid (≤20 topics)">
        <p className="text-[12px] text-fg-3 mb-3">
          3-col rich cards. Each card carries icon + name + description (2-line
          clamp) + 2-3 subtopic chips + last-touched timestamp. Description and
          chips give &ldquo;what&apos;s in there&rdquo; before you click. No
          memory count. No subtopic count. Just ambient temporal context.
        </p>
        <TopicMainViewMock mode="a" />
      </DemoCard>

      <DemoCard label="29a-mode-b. Mode B — Team dense grid (20-80 topics)">
        <p className="text-[12px] text-fg-3 mb-3">
          4-col compact cards. Description and chips drop. Each card = icon +
          name + last-touched. Top of view: search input + sort dropdown +
          Pinned section. Default sort: by last-touched.
        </p>
        <TopicMainViewMock mode="b" />
      </DemoCard>

      <DemoCard label="29a-mode-c. Mode C — Enterprise list (80+ topics)">
        <p className="text-[12px] text-fg-3 mb-3">
          Single-row virtualized list. No card chrome. Each row = icon + name +
          last-touched (right-aligned). Header row shows count +{" "}
          <span className="font-mono">⌘K</span> hint + sort. No local search
          input — the global command bar handles topic filtering (rule 25).
          Pinned cards + Recently visited cards sit above the virtualized list,
          which mounts only visible rows so 1000+ topics scroll smoothly.
        </p>
        <TopicMainViewMock mode="c" />
      </DemoCard>

      <DemoCard label="29a-toggle. View toggle — user override (grid / dense / list)">
        <p className="text-[12px] text-fg-3 mb-3">
          Top-right of the topics view: a 3-icon toggle that lets the user
          override the auto-mode. Choice persists in localStorage per hub.
          Default = auto. Useful for power users who want dense list even on a
          small personal hub.
        </p>
        <ViewToggleMock />
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Pin flow + topic context menu
          ════════════════════════════════════════════════ */}
      <DemoCard label="29-pin. Pin flow — corner icon + shared-layout morph">
        <p className="text-[12px] text-fg-2 mb-1">
          Every topic card / row carries a{" "}
          <span className="font-mono">Pin</span> icon in its top-right corner.
          Empty state is <span className="font-mono">text-fg-4</span> outlined,
          revealed only on hover / focus (desktop) or always visible (mobile).
          Filled state is <span className="font-mono">text-fg-1</span> with a
          45° rotation — the icon looks &ldquo;pushed in&rdquo; when active.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Clicking the icon toggles pinned state with a{" "}
          <span className="text-fg-1">shared-layout morph</span>: the card
          animates from the All section up to the Pinned section (or back down
          on unpin) using framer-motion{" "}
          <span className="font-mono">layoutId</span>. Duration{" "}
          <span className="font-mono">NORMAL (0.2s)</span>, easing{" "}
          <span className="font-mono">EASE</span> spring from{" "}
          <span className="font-mono">@memaxlabs/ui/tokens/motion</span>. The
          Pinned section height animates with AnimatePresence — when the last
          pin is removed it collapses to 0 cleanly (no empty-section flash).
        </p>
        <PinFlowDemo />
        <p className="text-[11px] text-fg-4 mt-3">
          Industry references: Linear&apos;s favorites, Notion&apos;s star,
          Apple Reminders. All three use a persistent icon affordance + a
          shared-layout animation rather than a dropdown action. We match that
          pattern because it keeps the surface quiet — no menu to open, no
          confirmation to dismiss.
        </p>
      </DemoCard>

      <DemoCard label="29-context-menu. Long-press / right-click — topic actions">
        <p className="text-[12px] text-fg-2 mb-1">
          Pin is fast but limited. For rename + forget, the topic card exposes a{" "}
          <span className="text-fg-1">context menu</span>. Desktop: right-click
          anywhere on the card. Mobile: long-press (hold 400ms). Menu appears as
          a glass panel anchored to the click point. Three items: Pin / Unpin
          (toggles, matches corner icon state), Rename, Forget (destructive,
          red, opens confirm dialog in prod).
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Keyboard: focus a card row and press{" "}
          <span className="font-mono">Space</span> or{" "}
          <span className="font-mono">Enter</span> to drill in;{" "}
          <span className="font-mono">P</span> to toggle pin;{" "}
          <span className="font-mono">Menu key / Shift+F10</span> to open the
          context menu at the focused row. All three map to the same handlers
          behind the scenes.
        </p>
        <ContextMenuDemo />
        <p className="text-[11px] text-fg-4 mt-3">
          Prod wiring: wrap the menu in Radix{" "}
          <span className="font-mono">DropdownMenu</span> with{" "}
          <span className="font-mono">modal=false</span> so it doesn&apos;t trap
          focus (the user is still in the topic list). Right-click uses{" "}
          <span className="font-mono">onContextMenu</span>; long-press uses a
          400ms timer cleared on touch-move / touch-end. Mobile haptic: fire{" "}
          <span className="font-mono">navigator.vibrate(10)</span> when the menu
          opens.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Scale concerns — nested recursion, mega-orphans, loading
          ════════════════════════════════════════════════ */}
      <DemoCard label="29-scale-nested. Mode B/C recurses at every level">
        <p className="text-[12px] text-fg-2 mb-1">
          The scale-aware view is recursive (topic system rule 2). When a topic
          has &gt;20 direct subtopics, its{" "}
          <span className="text-fg-1">detail page</span> switches from
          inline-groups to the same dense-grid / list view that the top-level
          topics page uses. Same component, different root.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Example: <span className="font-mono">Engineering → Backend</span> has
          80 direct subtopics. Instead of rendering 80 inline collapsible groups
          in one scroll, the Backend detail page renders them as a Mode-B dense
          grid with search. The user picks one, drills in, and that
          subtopic&apos;s detail page again decides its own mode based on{" "}
          <span className="font-mono">directChildCount</span>. Scale applies
          locally; never globally.
        </p>
        <NestedScaleMock />
        <div className="mt-3 text-[11px] text-fg-4">
          Gap vs prod: the current{" "}
          <span className="font-mono">DrillNavigationMock</span> always inlines
          nested children regardless of count. Wiring this recursion means the
          drill rebase reads <span className="font-mono">directChildCount</span>{" "}
          at the new root and picks Mode A/B/C for that level.
        </div>
      </DemoCard>

      <DemoCard label="29-loadmore-subtopics. L2 interactive — Show more subtopics">
        <p className="text-[12px] text-fg-2 mb-1">
          The <span className="text-fg-1">L2 pagination boundary</span> (topic
          system rule 18): subtopics within a level. Initial render shows the
          first batch; &ldquo;Show 20 more&rdquo; appends the next group{" "}
          <span className="text-fg-1">in place with a staggered fade-in</span>{" "}
          (framer-motion <span className="font-mono">AnimatePresence</span>,{" "}
          <span className="font-mono">NORMAL (0.2s)</span>{" "}
          <span className="font-mono">EASE</span>, per-item delay 20ms). No
          layout jump — the container grows downward and existing rows
          don&apos;t move.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Independent cursor from L1 (topics) and L3 (memories-within-
          subtopic). Over 80 subtopics at a level, the section auto-switches to
          the Mode-B dense grid with search — see §29-scale-nested for the
          recursed-Mode-B treatment. Reduced-motion: instant, no stagger, no
          height animation.
        </p>
        <ShowMoreSubtopicsDemo />
      </DemoCard>

      <DemoCard label="29-orphans-mega. 500+ orphan memories — interactive">
        <p className="text-[12px] text-fg-2 mb-1">
          Orphan memories (direct children of a topic, not inside any subtopic)
          render unlabeled at the top of the level (rule 4). At scale — e.g. 527
          captures that never got assigned — the orphan section becomes its own
          paginated block with its own{" "}
          <span className="font-mono">Show 20 more</span> boundary, independent
          of the subtopic list below it.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          <span className="text-fg-1">
            Escape hatch for orphans is different from subtopics.
          </span>{" "}
          Orphans have no drill target — they belong to the topic itself, not a
          subtopic. So instead of &ldquo;Open full view →&rdquo;, the orphan
          section uses <span className="text-fg-1">search-first</span>: an
          inline filter input that matches against all 527 titles locally. Type
          in the field below — the list filters in place; clear it to restore
          pagination. Rule 23.
        </p>
        <MegaOrphansMock />
      </DemoCard>

      <DemoCard label="29-loading. Skeleton rows while paginating">
        <p className="text-[12px] text-fg-2 mb-1">
          Lazy-load-by-drill (rule 19) means every boundary has a loading state.
          The kitchen shows the skeletons so Codex doesn&apos;t have to guess
          shapes.
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1">L1</span> — top-level topics page first
            paint: 6 card skeletons that match Mode A card shape (icon circle +
            2 lines + chip row). Mode B/C use row skeletons of the same row
            height.
          </li>
          <li>
            <span className="text-fg-1">L2</span> — subtopics-within-a-level
            Show-more click: 4 new subtopic-header skeletons append to the list
            while the fetch is in flight. No layout jump.
          </li>
          <li>
            <span className="text-fg-1">L3</span> — memories-within-subtopic
            Show-more click: 8 memory-row skeletons append at the end of the
            list. Last-touched and topicLabel chips fade in once real data
            arrives.
          </li>
          <li>
            <span className="text-fg-1">Drill rebase</span> — in-place morph:
            during the 29n cross-fade, the outgoing content fades to skeletons
            of the incoming shape at 0.5 opacity, then the real content replaces
            them. Prevents empty-container flash.
          </li>
        </ul>
        <LoadingSkeletonMock />
      </DemoCard>

      {/* ════════════════════════════════════════════════
          29-hierarchy. Visual hierarchy — topic vs memory row (rules 32–34)
          ════════════════════════════════════════════════ */}
      <DemoCard label="29-hierarchy. Topic vs memory — typography + icon + no-summary">
        <p className="text-[12px] text-fg-2 mb-1">
          The prod screenshot that triggered this pass had three real problems
          compounding: memory rows showed inconsistent summary lines; memory
          titles used <span className="font-mono">font-medium</span> which
          flattened the hierarchy against topic rows; every memory row carried a{" "}
          <span className="font-mono">FileText</span> icon that visually
          competed with the topic <span className="font-mono">Folder</span>{" "}
          icon. Users couldn&apos;t tell what was anchor vs content at a glance.
        </p>
        <p className="text-[12px] text-fg-3 mb-4">
          Three coordinated rules fix it — no added chrome, just removing the
          prod divergences and applying memax DNA (content-led, chrome recedes).
          See rules 32–34 below.
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1 font-medium">Rule 32 — Typography</span>:
            topic rows fade when closed (
            <span className="font-mono">text-[14px] text-fg-2</span>) and commit
            when open (
            <span className="font-mono">text-[14px] font-medium text-fg-1</span>
            ). Memory rows stay{" "}
            <span className="font-mono">text-[14px] text-fg-1</span> regular
            always. Same 14px size for both — content is king, chrome
            (structure) recedes. Industry reference: Linear / Notion / GitHub
            all use same-size list items with weight-via-state hierarchy.
          </li>
          <li>
            <span className="text-fg-1 font-medium">
              Rule 33 — Content icon (doc vs non-doc)
            </span>
            : the split is doc vs non-doc, not text vs everything.{" "}
            <span className="text-fg-1">Doc-like</span> (no badge):{" "}
            <span className="font-mono">text</span>,{" "}
            <span className="font-mono">pdf</span>,{" "}
            <span className="font-mono">markdown</span>,{" "}
            <span className="font-mono">code</span> — all reading content,
            scan-identical. <span className="text-fg-1">Non-doc</span> (trailing
            badge at <span className="font-mono">text-fg-4</span>):{" "}
            <span className="font-mono">image</span>,{" "}
            <span className="font-mono">link</span>. These behave differently
            from reading content. Topic row always has its TopicIcon anchor;
            memory row usually has nothing on the left.{" "}
            <span className="text-fg-1">
              Icon presence is the strongest anchor-vs-content signal in the
              list
            </span>{" "}
            and pairs with rule 32 to carry hierarchy without extra chrome.
          </li>
          <li>
            <span className="text-fg-1 font-medium">
              Rule 34 — No summary on list rows
            </span>
            : memory rows in topic view are single-line (title + optional
            trailing badge + age). Summary lives in memory detail. Topic
            description lives in the topic focus header + expanded subtopic
            header only. Predictable rhythm; no "some rows have description,
            some don&apos;t" jaggedness.
          </li>
        </ul>
        <p className="text-[12px] text-fg-3 mb-3">
          <span className="text-fg-1 font-medium">Why no vertical rail:</span>{" "}
          memax DNA is "no dividers, content-led". Indentation (16px per level,
          rule 5 caps inline at 2 levels desktop / 1 mobile) + rule 32
          open/close contrast + rule 33 icon presence together carry the
          hierarchy. A rail would duplicate signals already in place. Notion
          uses rails because Notion pages nest arbitrarily — memax doesn&apos;t
          (rule 5 cap). If hierarchy still feels unclear after all three rules
          land in prod, revisit as a follow-up; don&apos;t add chrome
          preemptively.
        </p>
        <p className="text-[11px] text-fg-4 mb-3">
          Every §29h demo renders these rules — see 29h-large below for OAuth
          memories with mixed content types. The orphan row is a PDF
          (&quot;Security review Q1 decisions&quot;) — correctly renders{" "}
          <span className="text-fg-1">nothing</span> because PDFs are doc-like.
          Inside the OAuth subtopic, two non-doc rows render trailing badges:
          the RFC 7636 link icon and the OAuth consent flow image icon. Codex
          just needs to drop three prod divergences:{" "}
          <span className="font-mono">memory-row.tsx:548 font-medium</span> on
          memory title,{" "}
          <span className="font-mono">
            memory-row.tsx:340 FileText fallback
          </span>{" "}
          in renderLeadingIdentity, and{" "}
          <span className="font-mono">
            memory-row-presentation.ts:66-71 showSummary
          </span>{" "}
          including &quot;topic&quot; in its surface whitelist.
        </p>
        <TopicNavigationMock topic={NAV_MOCK_LARGE} sizeClass="large" />
      </DemoCard>

      {/* ════════════════════════════════════════════════
          C. Recall → Topic bridge
          ════════════════════════════════════════════════ */}
      <DemoCard label="29c. Recall → topic bridge (grouped results)">
        <p className="text-[12px] text-fg-2 mb-3">
          Recall results grouped by topic. Topics with &gt;1 match get a
          clickable header row. ✦ prefix on dream-discovered topics.
        </p>
        <div
          className="rounded-xl border border-border overflow-hidden"
          style={{ background: "var(--card)" }}
        >
          <TopicBridgeRow icon={Shield} name="Auth & Security" matchCount={3} />
          <div className="border-t border-border/20">
            {[
              "OAuth Token Refresh",
              "JWT Validation Strategy",
              "API Key Rotation",
            ].map((title, i) => (
              <div
                key={title}
                className="flex items-center gap-2 px-6 py-2 border-t border-border/10 first:border-0 hover:bg-surface-1 cursor-pointer"
              >
                <span className="text-[12px] text-fg-3 font-mono w-3 text-right">
                  {i + 1}
                </span>
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ background: NEUTRAL_DOT }}
                />
                <span className="text-[14px] text-fg-1 truncate flex-1">
                  {title}
                </span>
                <span className="text-[12px] text-fg-3 tabular-nums">
                  {[92, 87, 81][i]}%
                </span>
              </div>
            ))}
          </div>
          <div className="border-t border-border/30">
            <TopicBridgeRow icon={Rocket} name="Deployment" matchCount={1} />
          </div>
          <div className="border-t border-border/20">
            <div className="flex items-center gap-2 px-6 py-2 hover:bg-surface-1 cursor-pointer">
              <span className="text-[12px] text-fg-3 font-mono w-3 text-right">
                4
              </span>
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{ background: NEUTRAL_DOT }}
              />
              <span className="text-[14px] text-fg-1 truncate flex-1">
                Staging Deploy Guide
              </span>
              <span className="text-[12px] text-fg-3 tabular-nums">78%</span>
            </div>
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Topic rows: icon + name + match count + → chevron. ✦ prefix for
          dream-discovered. Individual results indented (px-6). Uses topic_id +
          topic_name from RecalledMemory.
        </p>
        {/* ── LLM Agent Rules: Recall → Topic Bridge ── */}
        <div className="mt-3 p-3 rounded-lg bg-surface-1 text-[11px] text-fg-3 font-mono space-y-1">
          <p className="font-semibold text-fg-2">
            LLM RULES — Recall → Topic Bridge
          </p>
          <p>
            • Production file: expand-search-results.tsx. Groups POST-Enter
            semantic results by topic_id.
          </p>
          <p>
            • Only groups when 2+ distinct topics. Single topic = flat list (no
            noise).
          </p>
          <p>
            • Topic icon resolved client-side from useTopics() cache. No server
            changes.
          </p>
          <p>
            • NO ✦ on topic headers in recall. Topics are inherently
            dream-created — marking them adds noise. ✦ is only for the topic
            grid (first-visit discovery signal).
          </p>
          <p>
            • Pre-Enter (keyword/FTS) results stay flat — no topic data
            available.
          </p>
          <p>
            • Indented rows: px-6 (vs px-4). Topic name suppressed in row
            metadata when grouped.
          </p>
          <p>
            • Touch targets: header min-h-11, rows min-h-11 via content +
            padding.
          </p>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          E. Mobile — single column variant of every mode
          ════════════════════════════════════════════════ */}
      <DemoCard label="29e. Mobile — single column for all 3 modes">
        <p className="text-[12px] text-fg-3 mb-3">
          Mobile collapses every grid to 1 column. Mode A keeps the rich card
          (icon + name + description + chips + last-touched). Mode B keeps the
          dense card (icon + name + last-touched). Mode C keeps the list row.
          Same content density, just stacked. The search-first patterns in B and
          C still apply at the top.
        </p>
        <div className="max-w-sm mx-auto">
          <div className="rounded-xl border border-border/40 bg-background p-3 space-y-2">
            {MAIN_VIEW_MOCK.slice(0, 3).map((t) => (
              <MainViewCardA key={t.id} topic={t} />
            ))}
          </div>
        </div>
      </DemoCard>

      <DemoCard label="29g. Topic fetch failures — contextual inline retry">
        <div className="space-y-4">
          <div className="max-w-md">
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Topics page / inbox counts
            </p>
            <ContentError
              plain
              message="Couldn't load your topics right now."
              detail="Topic grouping and inbox counts are temporarily unavailable."
              retryLabel="Try again"
            />
          </div>

          <div className="max-w-md">
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Topic detail shell
            </p>
            <ContentError
              plain
              message="Couldn't open this topic right now."
              detail="The topic header or structure didn't come through. Try again."
              retryLabel="Try again"
            />
          </div>

          <div className="max-w-md">
            <p className="text-[10px] text-fg-3 uppercase tracking-wider font-semibold mb-2">
              Topic memory list / recent section / tree panel
            </p>
            <ContentError
              plain
              message="Couldn't load memories for this topic."
              detail="The topic is here, but its memory list didn't load this time."
              retryLabel="Try again"
            />
          </div>
        </div>
        <p className="text-[10px] text-fg-4 font-mono mt-2">
          Reuses the existing memory-detail error treatment: no border, no
          tinted card, just centered copy + ghost retry on background. Never
          silent empty fallback. Production files: topic-grid.tsx,
          topic-detail.tsx, topic-tree-content.tsx.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          H. Topic navigation — inline collapsible subtopic groups
          ════════════════════════════════════════════════ */}
      <DemoCard label="29h. Topic navigation — one page, one scroll, inline groups">
        <div className="space-y-2 text-[13px] text-fg-2">
          <p>
            <span className="text-fg-1 font-medium">Core model.</span> Inside a
            topic, you see one memory list container. Subtopics render as inline
            collapsible groups — not as separate pages. Drill-down-as-page
            forces users to lose context on every tap; inline groups let them
            skim the whole structure in one pass and collapse what they
            don&apos;t care about.
          </p>
          <p>
            <span className="text-fg-1 font-medium">Size class</span> drives
            default collapse state, not page shape. Small and medium topics open
            all subtopics by default (still scannable). Large and huge topics
            start collapsed — the user expands what they need. There is no
            auto-expand-on-delta and no delta counts on headers. Topic
            navigation is silent about dreams (see rule 1 of the topic system
            rules).
          </p>
          <p className="text-fg-3 text-[12px]">
            One container (no separate &ldquo;since your last visit&rdquo;
            card), no ✦ N new pills, no row accents, indent grows with depth
            (capped at 3 levels). Orphan memories render unlabeled at the top of
            every level.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="29h-small. 8 memories, no subtopics — flat list">
        <p className="text-[12px] text-fg-3 mb-3">
          Trivial case. No grouping chrome. No headers. Just the memory list
          rendered top-to-bottom. Last-touched timestamps on the right of each
          row (rule 17); no delta accents.
        </p>
        <TopicNavigationMock topic={NAV_MOCK_SMALL} sizeClass="small" />
      </DemoCard>

      <DemoCard label="29h-medium. 40 memories, 3 subtopics — open by default">
        <p className="text-[12px] text-fg-3 mb-3">
          Medium topic. All three subtopics render open on arrival because the
          total is still scannable without collapse. The first two memories
          render unlabeled at the top — those belong directly to the topic, not
          to any subtopic. No dream-delta treatment — rows are neutral.
        </p>
        <TopicNavigationMock topic={NAV_MOCK_MEDIUM} sizeClass="medium" />
      </DemoCard>

      <DemoCard label="29h-large. 150 memories, 6 subtopics — closed by default">
        <p className="text-[12px] text-fg-3 mb-3">
          Large topic. All subtopics start collapsed; the user expands what they
          need. Subtopic headers carry just{" "}
          <span className="text-fg-1">icon · name · description</span> — no
          counts, no delta pills, no ✦ badges. The surface is pure spatial
          browse (rule 1 + rule 9).
        </p>
        <TopicNavigationMock topic={NAV_MOCK_LARGE} sizeClass="large" />
      </DemoCard>

      <DemoCard label="29h-huge. 612 memories, 12 subtopics — virtualized">
        <p className="text-[12px] text-fg-3 mb-3">
          Huge topic. All 12 subtopics render collapsed — visually compact, one
          scroll of headers tells the whole structure. The{" "}
          <span className="text-fg-1">+ N more</span> row at the bottom
          represents the virtualized rows — in production, only visible rows
          render; this demo simulates the cap. Mode C of the recursive
          scale-aware view (rule 2) would auto-select here if the count exceeds
          80.
        </p>
        <TopicNavigationMock
          topic={NAV_MOCK_HUGE}
          sizeClass="huge"
          interactive={false}
        />
      </DemoCard>

      <DemoCard label="29h-deep. Nested subtopics — 2 levels deep, indent model">
        <p className="text-[12px] text-fg-3 mb-3">
          Subtopics can themselves contain subtopics. Indent grows with depth
          (16px per level, capped at 3). No delta rollup, no ✦ counts — nested
          groups are just nested groups. Desktop inline cap is 2 (topic + one
          nested), anything deeper becomes a drill-in chip. See §29j-* for the
          drill flow.
        </p>
        <TopicNavigationMock topic={NAV_MOCK_DEEP} sizeClass="medium" />
      </DemoCard>

      <DemoCard label="29h-search. `/` filter — auto-expands matching subtopics">
        <p className="text-[12px] text-fg-3 mb-3">
          Type in the search box (top-right of the topic header). Only memories
          with titles matching the query render. Matching memories inside
          collapsed subtopics force the parent subtopic open so results
          aren&apos;t hidden. Clear the query → prior collapse state restores.
          In production this is triggered by the{" "}
          <span className="text-fg-1">/</span> keyboard shortcut.
        </p>
        <TopicNavigationMock
          topic={NAV_MOCK_SEARCH}
          sizeClass="large"
          searchable
        />
      </DemoCard>

      <DemoCard label="29h-rules. Navigation rules summary">
        <div className="space-y-2 text-[12px] text-fg-2">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p>
              <span className="text-fg-1 font-medium">One page per topic.</span>{" "}
              Subtopics are inline collapsible groups, not separate pages. No
              drill-down-as-route. Breadcrumb stays at{" "}
              <span className="font-mono">Your Topics &gt; Topic Name</span>{" "}
              regardless of which groups are expanded.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Direct memories render first, unlabeled.
              </span>{" "}
              Memories that belong to the topic but not to any subtopic render
              at the top of the list with no group header. Section ends when the
              first subtopic header appears.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Default collapse by size class.
              </span>{" "}
              Small (≤20) / medium (20–100): all subtopics open. Large (100–500)
              / huge (500+): all closed. The user expands what they need. No
              auto-expand-on-delta — topic navigation is silent about dreams
              (see topic system rule 1).
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p>
              <span className="text-fg-1 font-medium">
                No delta, no counts, no accents.
              </span>{" "}
              Subtopic headers carry icon · name · description only. No{" "}
              <span className="font-mono">newCount</span>, no ✦ pills, no row
              accents on &ldquo;new&rdquo; memories, no rollups. If something
              needs the user&apos;s call, it surfaces in §36 Inbox. Topic
              navigation stays pure spatial browse.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Indent capped at 3 levels.
              </span>{" "}
              Deeper nesting flattens — deeper children render at the same
              indent as level 3 with their parent name prefixed in the title if
              disambiguation is needed. Prevents runaway indentation in
              pathological trees.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Expand state persists per topic per user.
              </span>{" "}
              LocalStorage keyed by{" "}
              <span className="font-mono">
                memax-topic-expanded-{"{"}hubId{"}"}-{"{"}topicId{"}"}
              </span>
              . Manual toggles are the only source of truth. No auto-expand on
              any signal.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Search overrides expand state.
              </span>{" "}
              Typing in <span className="font-mono">/</span> search
              force-expands any subtopic containing a match. Clearing the search
              restores the prior expand state.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Virtualization for huge topics.
              </span>{" "}
              Topics over 500 memories use virtualized row rendering (only
              visible rows mount). Subtopic headers always render — they&apos;re
              cheap and they&apos;re navigation. Memory rows inside expanded
              subtopics virtualize.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          29-expand. Dual-tap-target row + expand/collapse animation
          ════════════════════════════════════════════════ */}
      <DemoCard label="29-expand. Expand vs drill — two tap targets on one row">
        <p className="text-[12px] text-fg-2 mb-1">
          An inline subtopic row carries{" "}
          <span className="text-fg-1">two tap targets</span>. The{" "}
          <span className="font-mono">chevron</span> on the left expands /
          collapses the row in place; the{" "}
          <span className="font-mono">row body</span> (icon · name ·
          description) drills into that subtopic, rebasing the container via the
          §29n morph. Same pattern as Notion toggles, Linear nested projects,
          Finder list view.
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1 font-medium">Chevron click</span> →{" "}
            <span className="font-mono">onToggle(sub.id)</span>, stays on
            current page. Chevron rotates 0° → 90°{" "}
            <span className="font-mono">FAST (0.15s)</span>{" "}
            <span className="font-mono">EASE</span>. Content region animates{" "}
            <span className="font-mono">{"{ height: 0, opacity: 0 }"}</span> →{" "}
            <span className="font-mono">
              {"{ height: 'auto', opacity: 1 }"}
            </span>{" "}
            via framer-motion <span className="font-mono">AnimatePresence</span>{" "}
            over <span className="font-mono">NORMAL (0.2s)</span>.{" "}
            <span className="font-mono">overflow: hidden</span> clips during
            animation so child padding doesn&apos;t leak.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Body click</span> →{" "}
            <span className="font-mono">onDrill(sub.id)</span>, rebases the
            container (breadcrumb gains a segment, content region slides +16px +
            cross-fades via §29n). Container stays put; only the content inside
            morphs.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Keyboard</span> —{" "}
            <span className="font-mono">Tab</span> goes chevron → body → next
            row. <span className="font-mono">Space</span> /{" "}
            <span className="font-mono">Enter</span> on chevron toggles;
            <span className="font-mono"> Enter</span> on body drills. Both
            buttons carry <span className="font-mono">aria-label</span>{" "}
            (&ldquo;Expand X&rdquo; / &ldquo;Open X&rdquo;) and the chevron also
            sets <span className="font-mono">aria-expanded</span>.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Reduced motion</span> —
            <span className="font-mono"> useReducedMotion()</span> from
            framer-motion returns true → both the chevron rotation and the
            height animation are skipped. Opacity crossfades stay (they
            don&apos;t trigger vestibular issues). Matches Apple HIG guidance
            for Reduce Motion.
          </li>
        </ul>
        <ExpandDrillDemo />
        <p className="text-[11px] text-fg-4 mt-3">
          Industry references. Notion: same dual-target pattern — chevron
          toggles, title navigates. Linear nested projects: identical. Apple
          Finder list view: click disclosure triangle to expand, tap folder name
          to navigate. Radix <span className="font-mono">Accordion</span>{" "}
          primitive: chevron is the trigger, content is the panel; we&apos;re
          borrowing the animation shape but keeping our own data-layer so the
          row body can act as a second affordance.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          J. Team hub deep navigation — drill-in, breadcrumb, picker
          ════════════════════════════════════════════════ */}
      <DemoCard label="29j. Team hub deep navigation — intro">
        <p className="text-[12px] text-fg-2 mb-1">
          <span className="text-fg-1 font-medium">
            Team hubs are Memax&apos;s focus.
          </span>{" "}
          They accumulate far more memory, far more subtopics, and deeper trees
          than personal hubs. A 50-person engineering team&apos;s
          <span className="font-mono"> Engineering</span> topic legitimately
          grows to depth 3-5 —
          <span className="font-mono">
            {" "}
            Engineering › Backend › Database › Postgres › Migrations ›
            Zero-downtime patterns
          </span>
          .
        </p>
        <p className="text-[12px] text-fg-3 mb-1">
          The inline-2 reading limit still holds (human working memory doesn
          &apos;t scale with team size), but{" "}
          <span className="text-fg-1 font-medium">
            drill-in is now a primary flow
          </span>
          , not an edge case. This section demonstrates the full e2e team
          navigation: sticky breadcrumb, drill-in chips beyond the cap, in-topic
          search, mobile flow, hub overview mode, and row-level author
          attribution. Dream reviews (merge / stale / low-confidence) surface in
          §36 Inbox only — never in the topic view.
        </p>
        <div className="mt-4 p-3 rounded-lg bg-surface-1 text-[11px] text-fg-3">
          <span className="text-fg-1 font-medium">Mock:</span>{" "}
          <span className="font-mono">TEAM_HUB_ENGINEERING</span> — Acme
          Engineering hub, 2,147 memories, 48 subtopics across 5 depth levels,
          with team attribution and 7-day activity rollups.
        </div>
      </DemoCard>

      {/* ─── E2E drill sequence: 5 frames walking through the tree ─── */}
      <DemoCard label="29j-a. Step 1 — Root view (desktop, cap=1)">
        <p className="text-[12px] text-fg-3 mb-3">
          Starting point. Breadcrumb is sticky at top (scroll to confirm). Depth
          0 (Backend, Frontend, …) + depth 1 (Database, Queue, Cache under
          Backend) render inline. Depth 2+ becomes drill-in rows: Postgres,
          MySQL, Redis show as clickable chips with{" "}
          <ChevronRight className="inline h-3 w-3" /> affordance. Fuzzy search
          across all depths lives in the global bar (§24m); no separate picker
          hotkey.
        </p>
        <DrillNavigationMock topic={TEAM_HUB_ENGINEERING} depthCap={1} />
      </DemoCard>

      <DemoCard label="29j-b. Step 2 — Drill into Backend">
        <p className="text-[12px] text-fg-3 mb-3">
          User clicks Backend → depth 2 drill chip, or clicks a drill chip
          directly. Breadcrumb gains a segment. List{" "}
          <span className="text-fg-1 font-medium">rebases in place</span> — no
          page transition. Backend&apos;s depth 0 children (Database, Queue
          system, Cache layers) become the new inline roots.{" "}
          <span className="text-fg-1 font-medium">Recent activity strip</span>{" "}
          under the header shows{" "}
          <span className="font-mono">+31 in 7 days by 4 contributors</span>.
          Click any earlier breadcrumb segment to walk back.
        </p>
        <DrillNavigationMock
          topic={TEAM_HUB_ENGINEERING}
          depthCap={1}
          initialPath={["backend"]}
        />
      </DemoCard>

      <DemoCard label="29j-c. Step 3 — Drill into Database">
        <p className="text-[12px] text-fg-3 mb-3">
          Three segments deep. Postgres / MySQL / Redis are now the new depth 0,
          all inline. Their children (Migrations, Replication, …) stay inline as
          depth 1. Everything deeper is drill-in chips. Activity strip shows
          tighter subtree context (+18, 3 people).
        </p>
        <DrillNavigationMock
          topic={TEAM_HUB_ENGINEERING}
          depthCap={1}
          initialPath={["backend", "database"]}
        />
      </DemoCard>

      <DemoCard label="29j-d. Step 4 — Drill into Postgres">
        <p className="text-[12px] text-fg-3 mb-3">
          Four segments deep. This is still fluid — each rebase is a cheap list
          re-render, not a route push. Migrations, Replication, Query
          optimization are new depth 0. Zero-downtime / Rollback /
          Version-control sit at depth 1, inline. Anything deeper is a drill
          chip again.
        </p>
        <DrillNavigationMock
          topic={TEAM_HUB_ENGINEERING}
          depthCap={1}
          initialPath={["backend", "database", "postgres"]}
        />
      </DemoCard>

      <DemoCard label="29j-e. Step 5 — Drill into Migrations (deepest leaf path)">
        <p className="text-[12px] text-fg-3 mb-3">
          Five segments deep — this is the maximum the{" "}
          <span className="font-mono">Topic</span> model allows. Zero-downtime
          patterns, Rollback strategies, Version control are the new depth 0.
          Notice the list is{" "}
          <span className="text-fg-1 font-medium">still readable</span> — the
          breadcrumb carries the full path, the header carries the narrow
          subject, and the list shows only what matters at this level. No
          IDE-style 5-level indent tower.
        </p>
        <DrillNavigationMock
          topic={TEAM_HUB_ENGINEERING}
          depthCap={1}
          initialPath={["backend", "database", "postgres", "migrations"]}
        />
      </DemoCard>

      {/* 29j-f / 29j-g REMOVED — the subtopic picker (⌘P) duplicates the
          global command bar (§24). Topic-aware fuzzy search for subtopics
          belongs to the bar's recall mode, not a separate modal. Visuals moved
          to §24 as "bar conceptual northstar — topic-scoped recall" for future
          iteration. See 24-bar-redesign.tsx. */}

      <DemoCard label="29j-h. Mobile — cap=0, drill at depth 1">
        <p className="text-[12px] text-fg-3 mb-3">
          Mobile has 32px less horizontal space per indent level, so the cap
          tightens to 0 (only top level inlines). Breadcrumb collapses to{" "}
          <span className="font-mono">← Backend</span> with the full path
          compressed to the right. Every depth-1 subtopic becomes a drill chip.
          Activity strip still shows under the header.
        </p>
        <div className="max-w-sm mx-auto">
          <DrillNavigationMock
            topic={TEAM_HUB_ENGINEERING}
            depthCap={0}
            initialPath={["backend"]}
            mobile
          />
        </div>
      </DemoCard>

      <DemoCard label="29j-i. Mobile — after one drill step">
        <p className="text-[12px] text-fg-3 mb-3">
          User tapped Database → now at Backend › Database. Breadcrumb compact
          shows <span className="font-mono">← Database</span> for fast
          single-tap back. Depth 1 children (Postgres, MySQL, Redis) again
          become drill chips.
        </p>
        <div className="max-w-sm mx-auto">
          <DrillNavigationMock
            topic={TEAM_HUB_ENGINEERING}
            depthCap={0}
            initialPath={["backend", "database"]}
            mobile
          />
        </div>
      </DemoCard>

      <DemoCard label="29j-rules. Team hub navigation rules">
        <div className="space-y-2 text-[12px] text-fg-2">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Inline cap stays at 2 (desktop) / 1 (mobile).
              </span>{" "}
              Team hub depth grows, reading capacity does not. Subtopics beyond
              the cap render as{" "}
              <span className="text-fg-1">drill-in chips</span> — single-row, no
              toggle chevron, right-side <span className="font-mono">→</span>{" "}
              affordance.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Drill-in is in-place rebase, not route push.
              </span>{" "}
              Clicking a drill chip re-parents the list without a page
              transition. The container, header, and list animate as one content
              replace (container morph principle). Back/forward is via the
              breadcrumb; browser history pushes as a secondary effect so
              deep-links still work.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Breadcrumb is first-class.
              </span>{" "}
              Sticky at top on desktop,{" "}
              <span className="font-mono">← parent</span> compact on mobile.
              Every segment is a click target.{" "}
              <span className="font-mono">⌘←</span> keyboard shortcut navigates
              up one level. Typing <span className="font-mono">Esc</span> resets
              to topic root.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p>
              <span className="text-fg-1 font-medium">
                The global bar is the depth escape hatch.
              </span>{" "}
              100+ subtopic team hubs need fuzzy search across all depths.
              That&apos;s the command bar&apos;s job (§24), not a separate
              picker modal. When the bar opens from inside a topic view it
              should bias toward in-topic matches first, then widen to global.
              See §24m &ldquo;Topic-scoped recall — conceptual northstar&rdquo;.
              Mobile uses the prod{" "}
              <span className="font-mono">MobileTreeSheet</span> (
              topic-tree-panel.tsx) for hierarchical jump + the bar for search;
              no extra picker sheet.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p>
              <span className="text-fg-1 font-medium">
                / search crosses all depth.
              </span>{" "}
              In-topic search always queries every descendant, not just the
              currently-visible level. Matching memories force-expand their
              containing subtopic. Clearing the query restores prior drill and
              expand state.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Row-level author attribution.
              </span>{" "}
              Every memory row in a team hub shows an 18px avatar of the pusher
              to the right of the title. Hover = full name. Personal hubs keep
              that slot empty for <span className="text-fg-1">You</span>, but
              reserve it for agent actors so the row still stays content-led.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Recent activity strip under every header.
              </span>{" "}
              Team-hub-only.{" "}
              <span className="font-mono">Last 7 days · +18 · @A @B @C</span>{" "}
              below the subtopic header. Anchors &ldquo;where&apos;s the team
              working right now&rdquo; intuition.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Hub overview is a side mode.
              </span>{" "}
              Separate view for onboarding / orientation. Flat-2 grid of depth 0
              cards with depth 1 children as preview chips. Does{" "}
              <span className="text-fg-1">not</span> replace drill-in — drill is
              for working, overview is for orienting.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">9.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Dream merge suggestions live in §36 Inbox, not here.
              </span>{" "}
              The dream engine may detect redundant sibling subtopics and
              propose a merge, but that surfaces as a Review in the Inbox (see
              §36 &ldquo;Low-confidence / structural proposal&rdquo; row
              pattern), never as a banner or card inside the topic detail view.
              Topic navigation stays silent.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">10.</span>
            <p>
              <span className="text-fg-1 font-medium">
                Drill state persists per user per topic.
              </span>{" "}
              LocalStorage key:{" "}
              <span className="font-mono">
                memax-drill-{"{"}hubId{"}"}-{"{"}topicId{"}"}
              </span>
              . A team member&apos;s drill state is their own — other members
              seeing the same topic start at their own last-visited subtopic,
              not yours.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          K. Icons at every depth
          ════════════════════════════════════════════════ */}
      <DemoCard label="29k. Icons at every depth — 28 / 16 / 14 px">
        <p className="text-[12px] text-fg-2 mb-1">
          Every <span className="font-mono">Topic</span> row in the data model
          carries an <span className="font-mono">icon</span> field (Lucide icon
          name). Render size varies by context:
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1 font-medium">28px</span> — focus header
            (the topic / subtopic currently being viewed)
          </li>
          <li>
            <span className="text-fg-1 font-medium">16px</span> — inline
            subtopic header row
          </li>
          <li>
            <span className="text-fg-1 font-medium">14px</span> — drill-in chip
            (subtopic beyond inline cap)
          </li>
          <li>
            Memory rows do <span className="text-fg-1">not</span> get the topic
            icon — they already carry their own content-type icon via MemoryRow.
          </li>
          <li>
            Unknown icon name → fallback to{" "}
            <span className="font-mono">FileText</span>. Handled by{" "}
            <span className="font-mono">TopicIcon</span> helper.
          </li>
        </ul>
        <p className="text-[11px] text-fg-3 mb-3">
          See <span className="font-mono">NAV_MOCK_LARGE</span> (Auth &amp;
          Security with <span className="font-mono">Shield</span>) — every
          inline subtopic header, drill chip, and focus title picks up its own
          icon. Unknown values fall through to FileText silently.
        </p>
        <TopicNavigationMock
          topic={NAV_MOCK_LARGE}
          sizeClass="large"
          descriptionStrategy="expanded-shallow"
        />
      </DemoCard>

      {/* ════════════════════════════════════════════════
          L. Pagination inside a subtopic
          ════════════════════════════════════════════════ */}
      <DemoCard label="29l. Pagination inside a subtopic — 'Show N more'">
        <p className="text-[12px] text-fg-2 mb-1">
          A subtopic can hold hundreds of memories. Rendering all of them inline
          breaks scroll and DOM weight. The canonical pattern:
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            Initial load <span className="font-mono">20</span> memories
            (DEFAULT_SUBTOPIC_PAGE_SIZE)
          </li>
          <li>
            &ldquo;Show N more&rdquo; at the bottom of the list loads the next
            20
          </li>
          <li>
            Once expanded, show &ldquo;Collapse&rdquo; to return to the first 20
          </li>
          <li>Search results render all matches (pagination disabled)</li>
          <li>
            Production endpoint (gap):{" "}
            <span className="font-mono">
              GET
              /v1/topics/:id/memories?subtopic_id=X&amp;cursor=Y&amp;limit=20
            </span>
          </li>
        </ul>
        <PaginationDemo />
      </DemoCard>

      {/* 29m + 29m-path REMOVED.
          • 29m (mobile subtopic picker sheet): duplicates the global command bar
            (§24) — topic-scoped fuzzy search belongs to the bar's recall mode.
            Visuals moved to §24 as bar conceptual northstar.
          • 29m-path (mobile breadcrumb sheet): duplicates prod MobileTreeSheet
            in topic-tree-panel.tsx which already lets any-level jumps via
            drill-down hierarchy. Compact ← parent breadcrumb stays for
            single-step back. */}

      {/* ════════════════════════════════════════════════
          N. Drill animation — container morph
          ════════════════════════════════════════════════ */}
      <DemoCard label="29n. Drill animation — container morph, not route push">
        <p className="text-[12px] text-fg-2 mb-1">
          Drilling into a subtopic (or popping back) animates the{" "}
          <span className="text-fg-1">content</span> inside the container, not
          the container itself. Forward = new content slides in from +16px with
          cross-fade. Backward = from −16px. Breadcrumb segments animate with
          layout; focus header cross-fades.
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            Duration: <span className="font-mono">NORMAL (0.2s)</span> from{" "}
            <span className="font-mono">@memaxlabs/ui/tokens/motion</span>
          </li>
          <li>
            Easing: <span className="font-mono">EASE</span> — cubic-bezier
            spring <span className="font-mono">[0.16, 1, 0.3, 1]</span>
          </li>
          <li>
            Transform-only: <span className="font-mono">translateX</span> +{" "}
            <span className="font-mono">opacity</span>. No layout properties
            animate.
          </li>
          <li>
            <span className="text-fg-1">Reduced-motion fallback:</span> wrap
            motion values in{" "}
            <span className="font-mono">useReducedMotion()</span> → skip
            translate, keep instant opacity swap.
          </li>
          <li>
            Same rules on desktop and mobile. Mobile does NOT use iOS-style
            route stack — it&apos;s still a container morph.
          </li>
        </ul>
        <AnimatedDrillDemo />
      </DemoCard>

      {/* ════════════════════════════════════════════════
          O. Memory row → detail animation
          ════════════════════════════════════════════════ */}
      <DemoCard label="29o. Memory row → detail — prod reference (NOT a proposal)">
        <p className="text-[12px] text-fg-2 mb-1">
          This card documents what production{" "}
          <span className="text-fg-1">already does</span>. Desktop and mobile
          use different metaphors because opening a memory is a{" "}
          <span className="text-fg-1">new surface</span>, not a container morph
          (29n covers the morph case — same topic, different subtopic).
        </p>
        <ul className="text-[12px] text-fg-3 mb-4 pl-4 space-y-0.5 list-disc">
          <li>
            <span className="text-fg-1 font-medium">Desktop</span> — centered
            glass modal on top of the topic view. Topic stays visible behind at
            reduced opacity so the user keeps their place. Close = Esc / click
            outside / ✕. This already works; no kitchen animation spec needed.
          </li>
          <li>
            <span className="text-fg-1 font-medium">Mobile</span> — push-in /
            push-out. The incoming surface pushes from the right edge, the
            outgoing topic view is pushed off to the left as a single
            coordinated motion (not a slide-over on top). Back = iOS edge swipe
            or back button, which runs the same motion in reverse. This is the
            transition Memax mobile already uses for topic→memory, hub→hub, and
            settings drill-down.
          </li>
          <li>
            Why document instead of design: the animation is already shipped and
            tuned. Earlier drafts of this card proposed a
            &ldquo;slide-from-right&rdquo; modal which conflicted with the
            existing push-in/out metaphor. Codex should{" "}
            <span className="text-fg-1">not</span> reimplement it — wire the
            existing prod transition into the new topic navigation surface.
          </li>
        </ul>
        <p className="text-[11px] text-fg-3">
          Production references: the mobile page transition lives in the app
          shell (see <span className="font-mono">packages/web/src/app/</span>{" "}
          layout + its page wrapper), and the desktop memory modal uses
          Radix&apos;s dialog primitives with our glass tokens.
        </p>
      </DemoCard>

      {/* ════════════════════════════════════════════════
          Design rules — topic system (consolidated)
          ════════════════════════════════════════════════ */}
      <DemoCard label="Design rules — topic system">
        <div className="space-y-2 text-[12px]">
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">1.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Topics are silent about dreams.
              </span>{" "}
              No &ldquo;✦ N new&rdquo; badges on cards, no row accents in
              detail, no delta headers, no &ldquo;since your last visit&rdquo;
              banners. Dream activity and decisions live in §36 Inbox. The topic
              surface is pure spatial browse.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">2.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Scale-aware main view, recursive at every level.
              </span>{" "}
              One component renders the topics list. Three modes by{" "}
              <span className="font-mono">directChildCount</span>: A (≤20, rich
              grid), B (20-80, dense grid + search + Pinned), C (80+,
              virtualized list + search-first + Pinned/Recent). Same logic
              recurses inside any topic — a subtopic with 200 children renders
              Mode B at THAT level. Thresholds 20/80 are starting values; tune
              with usage. See §29a.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">3.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Detail is a focused layer, not a page.
              </span>{" "}
              Breadcrumb → icon + name + full description → meta → memory list
              in one scrolling container. When the user drills into a subtopic,
              the header rebases to show THAT subtopic&apos;s own icon +
              description (see §29j focus rebase).
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">4.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Orphan memories render at every depth.
              </span>{" "}
              A topic can have direct memories not assigned to any subtopic.
              Same for subtopics — they can have direct memories AND children.
              Orphans render unlabeled at the top of each level, above the first
              subtopic group. The subtopic header row is the visual boundary —
              no &ldquo;General&rdquo; divider.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">5.</span>
            <p className="text-fg-2">
              <span className="font-medium">Inline cap at 2, drill past.</span>{" "}
              Desktop shows 2 depth levels inline (top + one nested); anything
              deeper becomes a drill-in chip. Mobile caps at 1. Max depth is 5
              (Topic model constraint) — all 5 levels are reachable through
              drill, not inline expansion.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">6.</span>
            <p className="text-fg-2">
              <span className="font-medium">Recall → topic bridge.</span> Recall
              results group by topic_id when there are 2+ distinct topics.
              Clickable header row with match count. ✦ prefix only in the topic
              grid for first-visit discovery — NOT on the recall bridge row
              (topics are inherently dream-created, the marker is noise).
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">7.</span>
            <p className="text-fg-2">
              <span className="font-medium">Borderless detail surface.</span>{" "}
              Topic detail content sits on{" "}
              <span className="font-mono">--background</span> without a framing
              card. Matches memory detail treatment.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">8.</span>
            <p className="text-fg-2">
              <span className="font-medium">Mobile is structural.</span> Single
              column grid. Large = full card. Medium = compact. Small =
              horizontal scroll pills. Tree trigger as discoverable row at top.
              Drill cap=1.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">9.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                No counts on topic cards / subtopic headers / drill chips /
                picker results.
              </span>{" "}
              No &ldquo;47 memories&rdquo;, no &ldquo;3 subtopics&rdquo;, no
              &ldquo;5 subs · 142&rdquo; on chips. The only number a topic
              surface carries is the{" "}
              <span className="text-fg-1">last-touched timestamp</span> —
              ambient temporal context, not metadata repetition. Sort, picker
              filtering, and pagination still use counts at the data layer; the
              UI just doesn&apos;t render them.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">10.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                The global bar is the depth escape hatch.
              </span>{" "}
              100+ subtopic team hubs need fuzzy search across all depths. That
              belongs to the command bar&apos;s recall mode (§24m), not a
              separate picker modal. Inside a topic view, the bar biases toward
              topic-local matches first, then widens to global. No ⌘P subtopic
              picker modal; no mobile picker bottom sheet.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">11.</span>
            <p className="text-fg-2">
              <span className="font-medium">Icons at every depth.</span>{" "}
              <span className="font-mono">Topic.Icon</span> (Lucide icon name)
              renders at 28px in the focus header, 16px in inline subtopic
              headers, 14px in drill-in chips. Unknown names fall back to{" "}
              <span className="font-mono">FileText</span>. Memory rows do not
              carry the topic icon — they have their own content-type icon. See
              §29k.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">12.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Memory lists inside subtopics paginate.
              </span>{" "}
              Initial load 20 (
              <span className="font-mono">DEFAULT_SUBTOPIC_PAGE_SIZE</span>
              ), &ldquo;Show N more&rdquo; loads next 20, &ldquo;Collapse
              &rdquo; returns to 20. Search results render all matches.
              Production:{" "}
              <span className="font-mono">
                GET
                /v1/topics/:id/memories?subtopic_id=X&amp;cursor=Y&amp;limit=20
              </span>
              . See §29l.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">13.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                No stats noise in focus headers.
              </span>{" "}
              Do NOT render &ldquo;{"{"}memoryCount{"}"} memories &middot; {"{"}
              subtopicCount{"}"} direct subtopics &middot; {"{"}
              levels{"}"} levels deep&rdquo;. The list below IS the signal. The
              only header meta allowed is the team-hub recent activity strip
              (see §29j rule 7), because that&apos;s collaboration context, not
              metadata repetition.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">14.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Mobile hierarchy = prod MobileTreeSheet.
              </span>{" "}
              Mobile has no ⌘P. The existing{" "}
              <span className="font-mono">MobileTreeSheet</span> in{" "}
              <span className="font-mono">topic-tree-panel.tsx</span> handles
              drill-down tree + any-level jump. Topic-aware fuzzy search lives
              in the command bar (§24m). The compact{" "}
              <span className="font-mono">← parent</span> breadcrumb stays for
              single-step back. No separate picker or path sheet.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">15.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Drill animation is container morph.
              </span>{" "}
              Content inside the container slides{" "}
              <span className="font-mono">±16px</span> + cross-fades. Duration{" "}
              <span className="font-mono">NORMAL (0.2s)</span>, easing{" "}
              <span className="font-mono">EASE</span> spring. Transform-only —
              no layout properties. Same rules on desktop and mobile. NOT an
              iOS-style route stack. Breadcrumb segments animate with layout.
              Reduced-motion: skip translate, keep instant opacity. See §29n.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">16.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Memory row → detail uses the existing prod transitions.
              </span>{" "}
              Desktop: Radix dialog in a centered glass container; topic view
              stays behind at reduced opacity. Mobile: push-in / push-out
              (incoming surface pushes from the right, topic view shifts left as
              one coordinated motion). Both are already shipped in prod —
              kitchen 29 does NOT spec a new animation, it just documents which
              existing transition to wire. See §29o.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">17.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Last-touched timestamp on every topic card / row.
              </span>{" "}
              Neutral fg-3 small text, NOT a badge. Encompasses both human push
              and dream organization — no source attribution. The default sort
              key. Source-agnostic by design: users don&apos;t care who touched
              it, they care it&apos;s recent.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">18.</span>
            <p className="text-fg-2">
              <span className="font-medium">Three pagination boundaries.</span>{" "}
              <span className="text-fg-1">L1: top-level topics</span> — cursor
              pagination on /topics page; Mode C virtualizes.
              <span className="text-fg-1"> L2: subtopics within a level</span> —
              inline render first 20, &ldquo;Show more subtopics&rdquo;
              progressive disclosure; auto-switch to search-first when count
              &gt; 80.
              <span className="text-fg-1">
                {" "}
                L3: memories within a subtopic
              </span>{" "}
              — DEFAULT_SUBTOPIC_PAGE_SIZE = 20, &ldquo;Show N more&rdquo; loads
              next 20, &ldquo;Collapse&rdquo; returns. Each boundary is
              independent and uses its own cursor.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">19.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Lazy load by drill — never recurse.
              </span>{" "}
              Loading a topic detail fetches that topic&apos;s direct subtopics
              + first 20 orphan memories. Drilling into a subtopic fetches THAT
              subtopic&apos;s direct children + 20 orphans. Never preload nested
              grandchildren. A 5-level tree with 100 things at each level = 5
              fetches as the user drills, not 10 billion items upfront. Counts
              come from denormalized columns on{" "}
              <span className="font-mono">Topic</span>, never from counting
              child rows.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">20.</span>
            <p className="text-fg-2">
              <span className="font-medium">View toggle override.</span>{" "}
              Top-right of the topics view: 3-icon toggle (grid / dense / list)
              that lets the user override the auto-mode. Choice persists in
              localStorage per hub. Default = auto (mode selected by{" "}
              <span className="font-mono">directChildCount</span>). For power
              users who want dense list on a small personal hub. See
              §29a-toggle.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">21.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Topic card chips drill directly.
              </span>{" "}
              In Mode A (rich grid), each{" "}
              <span className="font-mono">topSubtopics</span> chip is its own
              tap target. Card body tap → drill to the topic root. Chip tap →
              drill straight to that subtopic, skipping the topic root view.
              Both use the SAME §29n container morph. Chip click must{" "}
              <span className="font-mono">stopPropagation</span> so the parent
              card click doesn&apos;t also fire. See §29a-mode-a.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">22.</span>
            <p className="text-fg-2">
              <span className="font-medium">Scale is recursive, locally.</span>{" "}
              Drilling into a subtopic doesn&apos;t just swap content — the new
              root re-computes its own Mode A/B/C from its own{" "}
              <span className="font-mono">directChildCount</span>. A topic with
              12 children renders Mode A at the root AND Mode B inside a
              subtopic that has 80 grandchildren. See §29-scale-nested.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">23.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Orphans paginate independently.
              </span>{" "}
              Orphan memories (rule 4) get their own L3 boundary with its own
              Show-more cursor. When orphan count &gt; 80, the orphan section
              switches to dense-list-with-search — a local Mode C independent of
              the subtopics list below. See §29-orphans-mega.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">24.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Every boundary has a skeleton.
              </span>{" "}
              L1 first paint renders topic card skeletons matching Mode A/B/C
              row shapes. L2 Show-more appends 4 subtopic-header skeletons in
              place. L3 Show-more appends 8 memory-row skeletons. The §29n drill
              morph cross-fades outgoing content into skeletons of the incoming
              shape at 0.5 opacity, then swaps in real data — prevents
              empty-container flash. See §29-loading.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">25.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Search lives in the command bar, not in the topics view.
              </span>{" "}
              Mode B and Mode C do NOT render a local search input. Filtering
              the topic list is the §24 command bar&apos;s job — when the user
              is on <span className="font-mono">/topics</span> and hits{" "}
              <span className="font-mono">⌘K</span>, the bar&apos;s recall mode
              is scoped to topic matches across the current hub. Mode B/C header
              rows show a quiet &ldquo;⌘K to search&rdquo; hint (desktop) beside
              the sort dropdown; mobile relies on the already-visible bar. One
              surface per job, no duplicate search affordances. Two search boxes
              on one screen force the user to guess which one filters which
              scope — don&apos;t do it.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">26.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Pin = corner icon + shared-layout morph.
              </span>{" "}
              Every topic card / row carries a{" "}
              <span className="font-mono">Pin</span> icon top-right. Empty:{" "}
              <span className="font-mono">text-fg-4</span> outlined, revealed on
              hover/focus (desktop), always visible (mobile). Filled:{" "}
              <span className="font-mono">text-fg-1</span>, rotated 45°. Toggle
              click → <span className="text-fg-1">stopPropagation</span> +
              framer-motion <span className="font-mono">layoutId</span> morph
              from the All section to the Pinned section (or back),
              <span className="font-mono"> NORMAL (0.2s)</span>{" "}
              <span className="font-mono">EASE</span> spring. Pinned section
              collapses via AnimatePresence when empty. Per-hub localStorage
              persistence. Matches Linear/Notion/Apple Reminders pattern. See
              §29-pin.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">27.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Long-press / right-click → topic context menu.
              </span>{" "}
              Three items: Pin/Unpin (toggles, matches corner icon state),
              Rename, Forget (destructive,{" "}
              <span className="font-mono">oklch(0.55 0.2 25)</span>, opens
              confirm dialog in prod). Desktop trigger:{" "}
              <span className="font-mono">onContextMenu</span>. Mobile trigger:{" "}
              <span className="font-mono">touchstart</span> + 400ms timer,
              cleared on <span className="font-mono">touchmove / touchend</span>
              . Mobile fires{" "}
              <span className="font-mono">navigator.vibrate(10)</span> on menu
              open. Keyboard: focus row + <span className="font-mono">P</span>{" "}
              for pin, <span className="font-mono">Shift+F10 / Menu key</span>{" "}
              for context menu. Prod wraps in Radix{" "}
              <span className="font-mono">DropdownMenu</span> with{" "}
              <span className="font-mono">modal=false</span> so focus stays on
              the topic list. See §29-context-menu.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">28.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Subtopic row has two tap targets: chevron and body.
              </span>{" "}
              <span className="font-mono">Chevron</span> click →{" "}
              <span className="font-mono">onToggle(id)</span>, expand / collapse
              in place, stays on current page. Row{" "}
              <span className="font-mono">body</span> click (icon + name +
              description) → <span className="font-mono">onDrill(id)</span>,
              rebases the container to that subtopic via the §29n morph. Two
              separate <span className="font-mono">&lt;button&gt;</span>s in one
              flex row with <span className="font-mono">items-start</span> so
              the chevron stays top-aligned when the body grows (otherwise{" "}
              <span className="font-mono">items-stretch</span> +
              <span className="font-mono"> items-center</span> shifts the
              chevron down on expand). Both buttons carry{" "}
              <span className="font-mono">aria-label</span>; chevron sets{" "}
              <span className="font-mono">aria-expanded</span>.{" "}
              <span className="font-mono">Tab</span> order: chevron → body →
              next row. Matches Notion toggles, Linear nested projects, Finder
              list view, Radix Accordion. See §29-expand.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">29.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Expand / collapse animates chevron + content height.
              </span>{" "}
              Chevron rotates{" "}
              <span className="font-mono">rotate(0deg → 90deg)</span> over{" "}
              <span className="font-mono">FAST (0.15s)</span>{" "}
              <span className="font-mono">EASE</span>. Content region is wrapped
              in framer-motion{" "}
              <span className="font-mono">&lt;AnimatePresence&gt;</span> +{" "}
              <span className="font-mono">&lt;motion.div&gt;</span> with{" "}
              <span className="font-mono">{"{ height: 0, opacity: 0 }"}</span> →{" "}
              <span className="font-mono">
                {"{ height: 'auto', opacity: 1 }"}
              </span>
              , duration <span className="font-mono">NORMAL (0.2s)</span>,
              easing <span className="font-mono">EASE</span>.{" "}
              <span className="font-mono">overflow: hidden</span> on the wrapper
              clips child padding during animation. Nested subtopics compose
              their own AnimatePresence inside — no special handling needed.{" "}
              <span className="font-mono">useReducedMotion()</span> from
              framer-motion gates both the chevron rotation and the height
              animation: when true, both are instant — opacity crossfade stays
              (doesn&apos;t trigger vestibular issues, matches Apple HIG Reduce
              Motion). See §29-expand.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">30.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Inline expand has an escape hatch at{" "}
                <span className="font-mono">INLINE_EXPAND_THRESHOLD = 100</span>
                .
              </span>{" "}
              Expanding a subtopic inline is for quick peek, not sustained
              reading. While its memory count is &le; 100,{" "}
              <span className="font-mono">SubtopicMemoryList</span> renders
              &ldquo;Show 20 more&rdquo; + &ldquo;Open full view →&rdquo;
              side-by-side at the bottom of the list (L3 pagination). Past 100,
              &ldquo;Show more&rdquo; disappears and{" "}
              <span className="font-mono">
                &ldquo;Open full view → (N memories)&rdquo;
              </span>{" "}
              is the only footer action — nudging the user to drill via §29n
              instead of paginating endlessly in place. The escape hatch wires
              to the same <span className="font-mono">onDrill(subtopicId)</span>{" "}
              handler as the row body button (rule 28), so chevron-expand +
              body- drill + escape-hatch-drill all compose cleanly. Orphans
              don&apos;t get this hatch — they have no drill target; instead
              they switch to search-first local filter when count &gt; 80 (rule
              23). See §29l + §29-expand.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">31.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                LEGACY — delete{" "}
                <span className="font-mono">topic-tree-panel.tsx</span> during
                migration.
              </span>{" "}
              The desktop sidebar tree (
              <span className="font-mono">
                packages/web/src/components/features/topic-tree-panel.tsx
              </span>
              ) and its <span className="font-mono">MobileTreeSheet</span>{" "}
              duplicate what the new kitchen model already gives us: hierarchy
              rendering → chevron expand (rule 28); any-level jump → command bar
              §24m; breadcrumb anchor → topic detail header. Codex should delete
              both files after the migration,{" "}
              <span className="text-fg-1">but not before</span> §24m is wired
              (the bar must be topic-aware with{" "}
              <span className="font-mono">activeTopicId</span> /{" "}
              <span className="font-mono">scope=&quot;hub&quot;</span> scopes —
              otherwise users lose any-level search with no replacement).
              Ordering constraint:{" "}
              <span className="font-mono">1) wire §24m</span> →{" "}
              <span className="font-mono">
                2) delete topic-tree-panel.tsx + MobileTreeSheet
              </span>{" "}
              →{" "}
              <span className="font-mono">
                3) update all call sites (hub page, topic page, mobile dock)
              </span>
              . Until then, keep the legacy tree running in parallel — it still
              works, it&apos;s just redundant with the new model.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">32.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Topic fades as scaffolding, memory stays content.
              </span>{" "}
              Topic / subtopic header rows render as{" "}
              <span className="font-mono">text-[14px]</span> with{" "}
              <span className="text-fg-1">state-driven contrast</span>:{" "}
              <span className="font-mono">text-fg-2</span> regular when
              collapsed (fades as scaffolding — "this is a signpost, keep
              scanning"),{" "}
              <span className="font-mono">font-medium text-fg-1</span> when open
              ("you've committed to this branch"). Memory rows stay{" "}
              <span className="font-mono">text-[14px] text-fg-1</span> regular
              always — content is king, chrome (structure) is background. Memax
              design DNA: the reading target should always be prominent; the
              scaffolding should recede until you ask it to commit. Same font
              size for both (14px) — no size delta, industry reference is Linear
              / Notion / GitHub where list items share the same size and
              differentiate via weight + state. Prod{" "}
              <span className="font-mono">memory-row.tsx:548</span> has{" "}
              <span className="font-mono">font-medium</span> on memory titles —
              that&apos;s the divergence to fix (drop the
              <span className="font-mono"> font-medium</span>, match the
              kitchen). Differentiation between topic-open and memory comes from
              the open-state icon + weight combination, plus rule 33 (icon
              presence). See §29-hierarchy.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">33.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Content icon: doc-like types render nothing, non-doc types
                render a trailing badge.
              </span>{" "}
              The split is <span className="text-fg-1">doc vs non-doc</span>,
              not text vs everything else.{" "}
              <span className="text-fg-1">Doc-like</span> (no badge):{" "}
              <span className="font-mono">text</span>,{" "}
              <span className="font-mono">pdf</span>,{" "}
              <span className="font-mono">markdown</span>,{" "}
              <span className="font-mono">code</span> — all reading content,
              behaviorally the same when scanning.{" "}
              <span className="text-fg-1">Non-doc</span> (trailing{" "}
              <span className="font-mono">h-3 w-3 text-fg-4</span> badge via{" "}
              <span className="font-mono">MemoryContentTypeBadge</span>
              ): <span className="font-mono">image</span>,{" "}
              <span className="font-mono">link</span>. These are behaviorally
              different — scanning for "that screenshot" or "that URL I saved"
              is a different task than scanning for "that decision I wrote
              down". The rarity of non-doc rows in a typical topic is exactly
              what makes the badge valuable — it marks "this is not reading
              content" without shouting. Topic row always has its leading{" "}
              <span className="font-mono">TopicIcon</span>; memory row usually
              has nothing on the left. The presence/absence of leading icon is
              the strongest anchor-vs-content signal in the list — pairs with
              rule 32 to carry hierarchy without extra chrome. For team hubs,
              author avatar / agent icon still occupies the leading slot
              (attribution &gt; content type in team context). Matches GitHub
              issues, Notion pages, Linear tasks — all of which drop "default
              type" icons. Prod divergences to fix:{" "}
              <span className="font-mono">renderLeadingIdentity()</span>{" "}
              fallback at <span className="font-mono">memory-row.tsx:340</span>{" "}
              should return <span className="font-mono">null</span> for doc-like
              types, and the trailing{" "}
              <span className="font-mono">isRichContentType</span> check at{" "}
              <span className="font-mono">memory-row-presentation.ts:144</span>{" "}
              should drop <span className="font-mono">pdf</span> from its
              whitelist (keep only image + link). See §29-hierarchy.
            </p>
          </div>
          <div className="flex items-start gap-2">
            <span className="text-fg-3 shrink-0 w-4">34.</span>
            <p className="text-fg-2">
              <span className="font-medium">
                Memory rows never show summary in list view.
              </span>{" "}
              Descriptions are a <span className="text-fg-1">topic-header</span>{" "}
              affordance (focus header shows full description; expanded subtopic
              header shows description via{" "}
              <span className="font-mono">descriptionStrategy</span>). Memory
              rows in the topic surface are single-line: title + optional
              trailing content badge + age. No summary, no second line, no
              truncated preview. Summary content lives in the memory detail view
              where the user has opted into reading it. Prod{" "}
              <span className="font-mono">showSummary</span> currently includes{" "}
              <span className="font-mono">&quot;topic&quot;</span> in its
              surface whitelist at{" "}
              <span className="font-mono">
                memory-row-presentation.ts:66-71
              </span>{" "}
              — that&apos;s the divergence to fix. Recent feed keeps the summary
              (different rhythm, different surface).
            </p>
          </div>
          {/* Rule formerly-34 (vertical rail) intentionally dropped —
              indentation + rule 32 typography state + rule 33 icon
              presence carry the hierarchy without decorative chrome.
              Matches memax DNA (no dividers, content-led). If hierarchy
              feels unclear after the three rules above are wired,
              revisit as a follow-up — don't add chrome preemptively. */}
        </div>
      </DemoCard>
    </Section>
  );
}
