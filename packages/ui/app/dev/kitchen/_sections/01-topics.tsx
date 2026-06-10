// Kitchen 01 — Topics
// Maps to: TopicGrid (/memories), TopicDetail (/topics/[id])
// TopicCard imported from production — kitchen uses real component with mock data
//
// PAGE HIERARCHY: "Recent" and "Your Topics" are SIBLING sections.
//   "Recent" → RecentSection (time-filtered). "Your Topics" + count → topics + inbox.
//   Count covers topics + inbox ONLY (not recent). Both desktop and mobile.
"use client";

import { useState } from "react";
import { Section, DemoCard } from "../_shared";
import {
  Rocket,
  ShieldCheck,
  Code,
  Database,
  Layout,
  FileText,
  ChevronRight,
  ChevronDown,
  Pin,
  Server,
  Zap,
  RotateCcw,
  Image as ImageIcon,
  Link as LinkIcon,
  Clock,
  Inbox,
  Copy,
  Bot,
} from "lucide-react";
import { DREAM_PURPLE } from "@/tokens/dream";
import type { LucideIcon } from "lucide-react";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * UNIFIED MEMORY ROW — page-level usage examples
 *
 * Canonical component rules now live in section 04: Memory Row Variants.
 * This section shows how those row rules compose into page-level topic,
 * recent, and inbox layouts.
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

interface MockMemory {
  title: string;
  summary?: string;
  tags?: string[];
  age: string;
  source?: string;
  sourceAgent?: string;
  authorName?: string;
  authorInitial?: string;
  hubName?: string; // team hub name — shown in personal "all" view
  accessCount?: number;
  contentType?: "text" | "pdf" | "image" | "link";
  dotColor?: string;
  isProcessing?: boolean;
}

type Surface = "recent" | "inbox" | "topic" | "list" | "recall";

/**
 * Unified MemoryRow — page-level mock that mirrors the canonical section 04
 * rules closely enough for layout demos in this section.
 */
function MemoryRow({
  memory: m,
  surface,
  showDivider = false,
  showCopy = false,
}: {
  memory: MockMemory;
  surface: Surface;
  showDivider?: boolean;
  showCopy?: boolean;
}) {
  const dot = m.dotColor ?? NEUTRAL_DOT;

  // Processing state — universal, same on all surfaces
  if (m.isProcessing) {
    return (
      <div
        className={`px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${showDivider ? "border-t border-border/30" : ""}`}
      >
        <div className="flex items-center gap-2">
          <span
            className="h-3 w-3 flex items-center justify-center text-[10px] leading-none state-slow-breathe shrink-0"
            style={{ color: "var(--signature)" }}
          >
            ✦
          </span>
          <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
            {m.title}
          </span>
          <span
            className="text-[13px] shrink-0 state-slow-breathe"
            style={{ color: "var(--signature)" }}
          >
            organizing...
          </span>
        </div>
      </div>
    );
  }

  const showAttributionText =
    surface === "recent" || surface === "recall" || surface === "list";
  const useStackedRecent =
    surface === "recent" && (!!m.sourceAgent || !!m.authorName);

  // Indicator slot — determined by attribution tier
  const indicator = m.sourceAgent ? (
    // Tier 1: Agent capture → Bot icon
    <Bot className="h-3.5 w-3.5 text-fg-3 shrink-0" />
  ) : m.authorName ? (
    // Tier 2: Team human → Avatar initial
    <div
      className="h-4 w-4 rounded-full flex items-center justify-center text-[8px] font-medium text-fg-2 shrink-0"
      style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
    >
      {m.authorInitial ?? m.authorName[0]?.toUpperCase()}
    </div>
  ) : m.contentType === "pdf" ? (
    <FileText className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : m.contentType === "image" ? (
    <ImageIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : m.contentType === "link" ? (
    <LinkIcon className="h-3 w-3 shrink-0" style={{ color: dot }} />
  ) : (
    // Tier 3: Personal default → colored dot
    <span className="h-3 w-3 flex items-center justify-center shrink-0">
      <span
        className="h-1.5 w-1.5 rounded-full"
        style={{ backgroundColor: dot }}
      />
    </span>
  );

  const contextLabel = !showAttributionText ? null : m.authorName ? (
    <span className="text-[13px] text-fg-3 shrink-0">
      <span className="hidden sm:inline">{m.authorName} </span>
      {m.sourceAgent ? `via ${m.sourceAgent}` : "pushed"}
    </span>
  ) : m.sourceAgent ? (
    <span className="text-[13px] text-fg-3 shrink-0">
      <span className="hidden sm:inline">{m.sourceAgent} </span>
      captured
    </span>
  ) : null;

  // Surface-specific: show summary?
  const showSummary =
    !!m.summary &&
    (surface === "topic" ||
      surface === "list" ||
      surface === "recent" ||
      surface === "recall");

  // Tags: NOT shown at row level — they're noise in a list.
  // Tags live in: memory detail page, search matching (backend), copy-for-AI export.

  // Surface-specific: show source/count?
  const showSource = surface === "list";
  const showCount = showSource && (m.accessCount ?? 0) > 0;

  return (
    <div
      className={`px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${showDivider ? "border-t border-border/30" : ""}`}
    >
      {useStackedRecent ? (
        <>
          <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
            {indicator}
            <span className="text-foreground/40">{contextLabel}</span>
            <span className="ml-auto text-foreground/40 tabular-nums">
              {m.age}
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              {m.title}
            </span>
            {showCopy && (
              <Copy className="h-3 w-3 text-fg-4 hover:text-fg-3 cursor-pointer shrink-0" />
            )}
          </div>
        </>
      ) : (
        <div className="flex items-center gap-2">
          {indicator}
          {contextLabel}
          <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
            {m.title}
          </span>
          <div className="flex items-center gap-1.5 ml-auto shrink-0">
            {m.hubName && surface === "recall" && (
              <span className="text-[10px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
                {m.hubName}
              </span>
            )}
            {showCount && (
              <span className="text-[12px] text-fg-3 tabular-nums">
                {m.accessCount}×
              </span>
            )}
            {showSource && m.source && (
              <span className="text-[10px] text-fg-3">{m.source}</span>
            )}
            <span className="text-[13px] text-fg-3 tabular-nums">{m.age}</span>
            {showCopy && (
              <Copy className="h-3 w-3 text-fg-4 hover:text-fg-3 cursor-pointer ml-0.5" />
            )}
          </div>
        </div>
      )}
      {/* Line 2: summary */}
      {showSummary && (
        <p
          className={`text-[13px] text-fg-2 truncate mt-0.5 ${useStackedRecent ? "" : "pl-5"}`}
        >
          {m.summary}
        </p>
      )}
      {/* Tags removed from row level — they're noise in lists.
         Tags live in: memory detail page, search (backend), copy-for-AI. */}
    </div>
  );
}

/* ── Mock data ── */

const MEMORIES: Record<string, MockMemory> = {
  agentCapture: {
    title: "Session: refactored ingest pipeline",
    summary:
      "Moved chunking from handler to dedicated worker, added retry logic...",
    tags: ["refactor", "ingest", "worker"],
    age: "8h",
    source: "hook",
    sourceAgent: "claude-code",
    hubName: "backend-team",
    dotColor: NEUTRAL_DOT,
  },
  teamHuman: {
    title: "API auth flow",
    summary: "OAuth2 refresh token rotation with 30-day expiry...",
    tags: ["auth", "oauth", "security"],
    age: "2h",
    source: "cli",
    authorName: "Derek",
    authorInitial: "D",
    hubName: "backend-team",
    dotColor: NEUTRAL_DOT,
  },
  teamHumanViaAgent: {
    title: "Reranker cost analysis",
    summary: "Cohere rerank v3 at $1/1000 queries, 50ms p95...",
    tags: ["cohere", "cost", "retrieval"],
    age: "5h",
    source: "cli",
    authorName: "Ziyang",
    authorInitial: "Z",
    sourceAgent: "cursor",
    hubName: "backend-team",
    dotColor: NEUTRAL_DOT,
  },
  personalText: {
    title: "Blue-green deployment strategy",
    summary: "Zero-downtime deploys using blue-green with Fly.io machines...",
    tags: ["fly.io", "zero-downtime", "infra"],
    age: "2d",
    source: "cli",
    accessCount: 5,
    dotColor: NEUTRAL_DOT,
  },
  personalPdf: {
    title: "Staging deploy process",
    summary: "Push to staging branch triggers auto-deploy...",
    tags: ["staging", "ci-cd"],
    age: "3d",
    source: "web",
    accessCount: 2,
    contentType: "pdf",
    dotColor: NEUTRAL_DOT,
  },
  personalImage: {
    title: "Architecture whiteboard photo",
    summary: "System diagram from Monday standup...",
    tags: ["architecture", "diagram"],
    age: "1w",
    source: "web",
    accessCount: 3,
    contentType: "image",
    dotColor: NEUTRAL_DOT,
  },
  personalLink: {
    title: "Fly.io multi-region docs",
    summary: "Official guide for multi-region Postgres with read replicas...",
    tags: ["fly.io", "docs", "postgres"],
    age: "5d",
    source: "cli",
    accessCount: 1,
    contentType: "link",
    dotColor: NEUTRAL_DOT,
  },
  processing: {
    title: "CleanShot 2026-04-08",
    age: "",
    isProcessing: true,
  },
  inbox1: {
    title: "Random debugging note",
    age: "6h",
    dotColor: NEUTRAL_DOT,
  },
  inbox2: {
    title: "Meeting with Alex re: pricing",
    age: "1d",
    dotColor: NEUTRAL_DOT,
  },
  inbox3: {
    title: "CLI flag idea for --export",
    age: "2d",
    dotColor: NEUTRAL_DOT,
  },
};

/* ── Topic card data ── */
/* Kitchen mock — defines the northstar visual spec.
 * Production implements: packages/web/src/components/features/topic-card.tsx
 * Flow: kitchen defines → production implements → verify match */

interface MockTopic {
  id: string;
  name: string;
  icon: LucideIcon;
  count: number;
  description?: string;
  subtopics?: string[];
  recentMemory?: string;
  pinned?: boolean;
}

const MOCK_TOPICS: MockTopic[] = [
  {
    id: "1",
    name: "Deployment",
    icon: Rocket,
    count: 23,
    description: "Blue-green deploys, rollback procedures, staging automation",
    subtopics: ["Staging", "Production", "Rollback"],
    recentMemory: "Fly.io machine-based blue-green with health checks",
    pinned: true,
  },
  {
    id: "2",
    name: "Authentication",
    icon: ShieldCheck,
    count: 18,
    description: "OAuth2 refresh flow, token rotation, session management",
    recentMemory: "Token rotation strategy with 1h access + 30d refresh",
  },
  {
    id: "3",
    name: "Go Patterns",
    icon: Code,
    count: 31,
    description: "Error handling, context propagation, interface design",
    recentMemory: "Table-driven tests with testcontainers setup",
  },
  {
    id: "4",
    name: "Database",
    icon: Database,
    count: 15,
    description: "Connection pooling, migration strategy, pgvector setup",
  },
  {
    id: "5",
    name: "Frontend",
    icon: Layout,
    count: 22,
    description: "React Server Components, Tailwind patterns, Radix primitives",
    subtopics: ["Components", "Styling"],
    recentMemory: "TanStack Query infinite scroll with sentinel observer",
  },
  {
    id: "6",
    name: "Team Docs",
    icon: FileText,
    count: 9,
    description: "Onboarding checklist, code review guide, team agreements",
    pinned: true,
  },
];

/* ── TopicCard — kitchen mock matching production spec ── */
/* Maps to: packages/web/src/components/features/topic-card.tsx */

function TopicCard({
  topic,
  onClick,
}: {
  topic: MockTopic;
  onClick?: () => void;
}) {
  const Icon = topic.icon;
  return (
    <button
      onClick={onClick}
      className="w-full h-full text-left rounded-xl p-4 flex flex-col transition-colors cursor-pointer hover:bg-surface-1"
      style={{ border: "1px solid var(--border)", background: "var(--card)" }}
    >
      <div className="flex items-center gap-2 mb-2">
        <Icon className="h-4 w-4 text-fg-3 shrink-0" />
        <span className="text-[15px] font-semibold text-foreground truncate">
          {topic.name}
        </span>
        {topic.pinned && <Pin className="h-3 w-3 text-fg-4 shrink-0 ml-auto" />}
      </div>
      {topic.description && (
        <p className="text-[13px] text-fg-2 line-clamp-1 mb-2">
          {topic.description}
        </p>
      )}
      {topic.subtopics && topic.subtopics.length > 0 && (
        <p className="text-[12px] text-fg-3 truncate mb-2">
          {topic.subtopics.join(" · ")}
        </p>
      )}
      {topic.recentMemory && (
        <div className="flex items-center gap-1.5 mb-2">
          <span className="text-[8px] shrink-0" style={{ color: DREAM_PURPLE }}>
            ✦
          </span>
          <span className="text-[12px] text-fg-3 truncate">
            {topic.recentMemory}
          </span>
        </div>
      )}
      <div className="flex items-center gap-2 mt-auto pt-1">
        <span className="text-[12px] text-fg-3 tabular-nums">
          {topic.count} memories
        </span>
        {topic.subtopics && topic.subtopics.length > 0 && (
          <span className="text-[12px] text-fg-4">
            · {topic.subtopics.length} subtopics
          </span>
        )}
      </div>
    </button>
  );
}

/* ── Section card wrapper (for memory row groups) ── */

function SectionCard({
  icon: Icon,
  label,
  trailing,
  children,
}: {
  icon: LucideIcon;
  label: string;
  trailing?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{ border: "1px solid var(--border)", background: "var(--card)" }}
    >
      <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
        <Icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
          {label}
        </span>
        <div className="flex-1" />
        {trailing}
      </div>
      {children}
    </div>
  );
}

/* ── Breadcrumb ── */

function Breadcrumb({ items }: { items: string[] }) {
  return (
    <div className="flex items-center gap-1.5 mb-3">
      {items.map((item, i) => (
        <span key={i} className="flex items-center gap-1.5">
          {i > 0 && <ChevronRight className="h-3 w-3 text-fg-4 shrink-0" />}
          <span
            className={`text-[13px] ${
              i === items.length - 1
                ? "text-fg-2 font-medium"
                : "text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
            }`}
          >
            {item}
          </span>
        </span>
      ))}
    </div>
  );
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * Progressive disclosure pattern — Show N more + Collapse
 *
 * Recent (and Inbox) are task-oriented list sections. Infinite scroll is
 * wrong for task-oriented surfaces — it surprises users with unbounded
 * growth and traps them in their past expand decisions. Progressive
 * disclosure in fixed batches, with an explicit collapse affordance,
 * gives users complete control over pacing.
 *
 * Core rules:
 *   • Initial = 5 rows, always. Fresh visit = fresh intention.
 *   • "Show 5 more" CTA is explicit and shows remaining count.
 *   • No IntersectionObserver, no scroll-triggered load.
 *   • Collapse lives in section header trailing slot, only when expanded.
 *   • No localStorage persistence of displayLimit.
 *   • When remaining ≤ 5, CTA says "Show last N".
 *   • When all loaded, CTA disappears; Collapse stays if displayLimit > 5.
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

const RECENT_INITIAL = 5;
const RECENT_BATCH = 5;

const PROGRESSIVE_MOCK_MEMORIES: Array<{ title: string; age: string }> = [
  { title: "Auth middleware rewrite notes", age: "3m" },
  { title: "React Server Component boundary rules", age: "12m" },
  { title: "Deployment rollback incident — Apr 9", age: "28m" },
  { title: "Ziyang pushed reranker cost analysis", age: "41m" },
  { title: "Tailwind 4 migration — color token changes", age: "1h" },
  { title: "Fly.io machine health check tuning", age: "1h" },
  { title: "PostgreSQL connection pooling strategy", age: "2h" },
  { title: "OAuth refresh token rotation bug", age: "2h" },
  { title: "API key scope design", age: "3h" },
  { title: "Dream engine clustering algorithm notes", age: "3h" },
  { title: "Memory embedding cache TTL decision", age: "4h" },
  { title: "Redis memory pressure alerts", age: "5h" },
  { title: "Onboarding flow — first-time empty state", age: "6h" },
  { title: "Recall ranking formula — time decay weights", age: "7h" },
  { title: "Topic grid three-tier card design", age: "8h" },
  { title: "CLI config sync deleted-file recovery", age: "9h" },
  { title: "Hub visibility scope filter implementation", age: "10h" },
  { title: "Bar notification morph animation spec", age: "11h" },
  { title: "Agent identity axis — A vs B vs C debate", age: "12h" },
  { title: "Kitchen 29d row-accent pattern decision", age: "13h" },
  { title: "Worker queue backoff tuning", age: "14h" },
  { title: "Neon connection pool size — 20 vs 40", age: "15h" },
  { title: "Retry idempotency key design", age: "16h" },
  { title: "Smart snippet centering in recall results", age: "17h" },
  { title: "Upgrade flow design — soft gate vs hard", age: "18h" },
  { title: "Topic delta auto-expand rule for large topics", age: "19h" },
  { title: "HubHeader aurora time-bucket gradients", age: "20h" },
];

function RecentProgressiveDisclosureDemo({
  initialLimit = RECENT_INITIAL,
  memories = PROGRESSIVE_MOCK_MEMORIES,
}: {
  initialLimit?: number;
  memories?: Array<{ title: string; age: string }>;
}) {
  const [displayLimit, setDisplayLimit] = useState(initialLimit);
  const total = memories.length;
  const visible = memories.slice(0, displayLimit);
  const remaining = Math.max(total - displayLimit, 0);
  const isExpanded = displayLimit > initialLimit;
  const allLoaded = displayLimit >= total;

  const showMoreLabel =
    remaining === 0
      ? null
      : remaining <= RECENT_BATCH
        ? `Show last ${remaining} · 显示剩余 ${remaining} 条`
        : `Show ${RECENT_BATCH} more · 还剩 ${remaining} 条`;

  return (
    <div
      className="rounded-xl overflow-hidden"
      style={{
        border: "1px solid var(--border)",
        background: "var(--card)",
      }}
    >
      {/* Section header */}
      <div className="flex items-center gap-2 px-4 pt-3 pb-1.5">
        <Clock className="h-3.5 w-3.5 text-fg-3 shrink-0" />
        <span className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide">
          Recent
        </span>
        <span className="text-[12px] text-fg-3 bg-surface-1 px-1.5 py-0.5 rounded">
          过去 12h
        </span>
        <div className="flex-1" />
        <span className="text-[12px] text-fg-4 tabular-nums">
          {visible.length} / {total}
        </span>
        <button
          type="button"
          className="ml-2 flex items-center gap-1 text-[12px] text-fg-4 hover:text-fg-3 transition-colors cursor-pointer"
        >
          <Copy className="h-3 w-3" />
          <span>Copy {visible.length}</span>
        </button>
        {isExpanded && (
          <button
            type="button"
            onClick={() => setDisplayLimit(initialLimit)}
            className="ml-2 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
          >
            收起
          </button>
        )}
        <button
          type="button"
          className="ml-2 text-[12px] text-fg-3 hover:text-fg-2 cursor-pointer transition-colors"
        >
          Select
        </button>
      </div>

      {/* Rows */}
      <div>
        {visible.map((row, i) => (
          <div
            key={`${row.title}-${i}`}
            className={`flex items-center gap-2 px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${i > 0 ? "border-t border-border/30" : ""}`}
          >
            <span className="text-[14px] text-fg-1 flex-1 truncate">
              {row.title}
            </span>
            <span className="text-[12px] text-fg-3 tabular-nums shrink-0">
              {row.age}
            </span>
          </div>
        ))}
      </div>

      {/* Show more CTA — only when more remaining */}
      {!allLoaded && showMoreLabel && (
        <button
          type="button"
          onClick={() =>
            setDisplayLimit((prev) => Math.min(prev + RECENT_BATCH, total))
          }
          className="w-full border-t border-border/30 py-2.5 text-center text-[13px] text-fg-3 hover:text-fg-2 hover:bg-surface-1 transition-colors cursor-pointer"
        >
          {showMoreLabel}
        </button>
      )}
      {allLoaded && isExpanded && (
        <div className="w-full border-t border-border/30 py-2 text-center text-[12px] text-fg-4">
          全部 {total} 条已加载
        </div>
      )}
    </div>
  );
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
 * MAIN SECTION
 * ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */

export function TopicsSection() {
  const [selectedTopic, setSelectedTopic] = useState<string | null>(null);

  return (
    <Section
      title="1. Topics"
      description="Topics page composition. Canonical row rules now live in section 04: Memory Row Variants. This section focuses on how recent, topic, and inbox rows fit together at the page level."
    >
      {/* ── 1a. Row anatomy — the universal structure ── */}
      <DemoCard label="1a. Row anatomy &mdash; universal 2-line structure">
        <p className="text-[12px] text-fg-2 mb-4">
          Baseline row anatomy for page-level topic layouts. The full
          attribution and responsive rules live in section 04. Use this section
          to validate page composition, spacing, and section hierarchy.
        </p>

        {/* Annotated anatomy */}
        <div
          className="rounded-xl overflow-hidden mb-4"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="px-4 py-2.5">
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 flex items-center justify-center shrink-0">
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ backgroundColor: NEUTRAL_DOT }}
                />
              </span>
              <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                Blue-green deployment strategy
              </span>
              <div className="flex items-center gap-1.5 ml-auto shrink-0">
                <span className="text-[11px] text-foreground/25">
                  backend-team
                </span>
                <span className="text-[13px] text-fg-3 tabular-nums">2d</span>
                <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
              </div>
            </div>
            <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-5">
              Zero-downtime deploys using blue-green with Fly.io machines...
            </p>
          </div>
        </div>

        {/* Labels */}
        <div className="grid grid-cols-2 gap-2 text-[11px] text-fg-3">
          <div>
            <span className="font-medium text-fg-2">Line 1:</span> [Indicator]
            Title [Meta: hub, age, copy]
          </div>
          <div>
            <span className="font-medium text-fg-2">Line 2:</span> Summary
            (optional, pl-5 indent). No tags at row level.
          </div>
        </div>
      </DemoCard>

      {/* ── 1a-exp. No-kind row exploration ── */}
      <DemoCard label="1a-exp. Row without kind &mdash; 3 options">
        <p className="text-[12px] text-fg-2 mb-4">
          If kind is fully removed, what anchors the left side? Comparing:
          title-only, content-type icon, and tags-inline. Agent/team attribution
          tiers are unaffected (they already don&apos;t use kind).
        </p>

        <div className="space-y-3">
          {/* Option A: Title only — cleanest */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1">
              A. Title only — no indicator
            </p>
            <div
              className="rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              {/* No indicator, just title */}
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors">
                <div className="flex items-center gap-2">
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Blue-green deployment strategy
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    2d
                  </span>
                  <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
                </div>
                <p className="text-[13px] text-fg-2 truncate mt-0.5">
                  Zero-downtime deploys using blue-green with Fly.io machines...
                </p>
                <div className="flex items-center gap-1.5 mt-0.5">
                  <span className="text-[12px] text-fg-3">fly.io</span>
                  <span className="text-[12px] text-fg-3">zero-downtime</span>
                  <span className="text-[12px] text-fg-3">infra</span>
                </div>
              </div>
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-2">
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Staging deploy process
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    3d
                  </span>
                </div>
                <p className="text-[13px] text-fg-2 truncate mt-0.5">
                  Push to staging branch triggers auto-deploy...
                </p>
              </div>
            </div>
          </div>

          {/* Option B: Content-type icon only — visual anchor */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1">
              B. Content-type icon — visual anchor, no label
            </p>
            <div
              className="rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors">
                <div className="flex items-center gap-2">
                  <span className="h-3 w-3 flex items-center justify-center shrink-0">
                    <span className="h-1.5 w-1.5 rounded-full bg-foreground/20" />
                  </span>
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Blue-green deployment strategy
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    2d
                  </span>
                  <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
                </div>
                <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-5">
                  Zero-downtime deploys using blue-green with Fly.io machines...
                </p>
              </div>
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-2">
                  <FileText className="h-3 w-3 shrink-0 text-fg-3" />
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Staging deploy process
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    3d
                  </span>
                </div>
                <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-5">
                  Push to staging branch triggers auto-deploy...
                </p>
              </div>
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-2">
                  <ImageIcon className="h-3 w-3 shrink-0 text-fg-3" />
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Architecture whiteboard photo
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    1w
                  </span>
                </div>
              </div>
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-2">
                  <LinkIcon className="h-3 w-3 shrink-0 text-fg-3" />
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Fly.io multi-region docs
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    5d
                  </span>
                </div>
              </div>
            </div>
            <p className="text-[11px] text-fg-4 mt-1">
              Neutral dot for text, FileText for PDF, ImageIcon for image,
              LinkIcon for link. All foreground/20 with no taxonomy color.
            </p>
          </div>

          {/* Option C: First tag as context — replaces subkind */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1">
              C. First tag as context label — replaces subkind
            </p>
            <div
              className="rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors">
                <div className="flex items-center gap-2">
                  <span className="text-[12px] text-fg-3 shrink-0">fly.io</span>
                  <span className="text-fg-4 text-[12px]">·</span>
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Blue-green deployment strategy
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    2d
                  </span>
                  <Copy className="h-3 w-3 text-fg-4 ml-0.5" />
                </div>
                <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-10">
                  Zero-downtime deploys using blue-green with Fly.io machines...
                </p>
              </div>
              <div className="px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-2">
                  <span className="text-[12px] text-fg-3 shrink-0">
                    staging
                  </span>
                  <span className="text-fg-4 text-[12px]">·</span>
                  <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
                    Staging deploy process
                  </span>
                  <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
                    3d
                  </span>
                </div>
              </div>
            </div>
            <p className="text-[11px] text-fg-4 mt-1">
              Uses first tag as context label. More specific than kind but
              varies wildly. Only works if tags are consistently short.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── 1b. Attribution tiers — the indicator slot ── */}
      <DemoCard label="1b. Attribution tiers &mdash; see section 04 for canonical rules">
        <p className="text-[12px] text-fg-2 mb-4">
          These examples are retained as visual references, but section 04 is
          the source of truth for the row system and attribution behavior.
        </p>

        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          {/* Tier 1: Agent capture */}
          <div className="px-4 pt-2 pb-0.5">
            <span className="text-[10px] text-fg-3 uppercase tracking-wider">
              Tier 1 — Agent auto-capture
            </span>
          </div>
          <MemoryRow memory={MEMORIES.agentCapture} surface="recent" />

          {/* Tier 2: Team human */}
          <div className="px-4 pt-3 pb-0.5 border-t border-border/30">
            <span className="text-[10px] text-fg-3 uppercase tracking-wider">
              Tier 2 — Team human push
            </span>
          </div>
          <MemoryRow memory={MEMORIES.teamHuman} surface="recent" />

          {/* Tier 2b: Team human via agent */}
          <div className="px-4 pt-3 pb-0.5 border-t border-border/30">
            <span className="text-[10px] text-fg-3 uppercase tracking-wider">
              Tier 2b — Team human via agent
            </span>
          </div>
          <MemoryRow memory={MEMORIES.teamHumanViaAgent} surface="recent" />

          {/* Tier 3: Personal default */}
          <div className="px-4 pt-3 pb-0.5 border-t border-border/30">
            <span className="text-[10px] text-fg-3 uppercase tracking-wider">
              Tier 3 — Personal (fallback to content-type dot)
            </span>
          </div>
          <MemoryRow memory={MEMORIES.personalText} surface="recent" showCopy />
        </div>
      </DemoCard>

      {/* ── 1b-hub. Hub attribution — two layout options ── */}
      <DemoCard label="1b-hub. Hub attribution &mdash; clean is the chosen direction">
        <p className="text-[12px] text-foreground/40 mb-4">
          The clean stacked attribution is the chosen direction for `recent`.
          Recall stays compact. Topic rows stay icon-only and quiet.
        </p>

        <div className="grid grid-cols-2 gap-4">
          {/* ── COMPACT (current) ── */}
          <div>
            <p className="text-[10px] text-foreground/25 uppercase tracking-wider mb-1.5">
              Compact — inline context, hub in meta
            </p>
            <div
              className="rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <MemoryRow
                memory={MEMORIES.personalText}
                surface="recent"
                showCopy
              />
              <MemoryRow
                memory={MEMORIES.teamHuman}
                surface="recent"
                showDivider
              />
              <MemoryRow
                memory={MEMORIES.agentCapture}
                surface="recent"
                showDivider
              />
              <MemoryRow
                memory={{
                  title: "Figma token export workflow",
                  age: "1d",
                  authorName: "Alex",
                  authorInitial: "A",
                  hubName: "design-team",
                  dotColor: NEUTRAL_DOT,
                }}
                surface="recent"
                showDivider
              />
            </div>
            <p className="text-[11px] text-foreground/25 mt-1.5">
              Legacy compact comparison only. We keep this here to show why the
              stacked treatment is better, not as an active recommendation.
            </p>
          </div>

          {/* ── CLEAN (Slack-style, with verbs) ── */}
          <div>
            <p className="text-[10px] text-foreground/25 uppercase tracking-wider mb-1.5">
              Clean — attribution above, verbs, titles aligned
            </p>
            <div
              className="rounded-xl overflow-hidden"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              {/* Personal — no attribution line, just title */}
              <div className="px-4 py-2.5 hover:bg-foreground/3 cursor-pointer transition-colors">
                <div className="flex items-center gap-2">
                  <span className="h-3 w-3 flex items-center justify-center shrink-0">
                    <span
                      className="h-1.5 w-1.5 rounded-full"
                      style={{
                        backgroundColor: NEUTRAL_DOT,
                      }}
                    />
                  </span>
                  <span className="text-[14px] font-medium text-foreground truncate flex-1">
                    Blue-green deployment strategy
                  </span>
                  <span className="text-[13px] text-foreground/40 tabular-nums shrink-0">
                    2d
                  </span>
                </div>
              </div>

              {/* Team human — attribution line + title */}
              <div className="px-4 py-2.5 hover:bg-foreground/3 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-1.5 mb-0.5">
                  <div
                    className="h-3.5 w-3.5 rounded-full flex items-center justify-center text-[7px] font-medium text-foreground/55 shrink-0"
                    style={{
                      background: "oklch(from var(--foreground) l c h / 0.08)",
                    }}
                  >
                    D
                  </div>
                  <span className="text-[12px] text-foreground/40">
                    Derek pushed
                  </span>
                  <span className="text-[11px] text-foreground/25">
                    · backend-team
                  </span>
                  <span className="text-[12px] text-foreground/40 ml-auto tabular-nums">
                    2h
                  </span>
                </div>
                <span className="text-[14px] font-medium text-foreground truncate block">
                  API auth flow
                </span>
              </div>

              {/* Agent capture — verb: captured */}
              <div className="px-4 py-2.5 hover:bg-foreground/3 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-1.5 mb-0.5">
                  <Bot className="h-3.5 w-3.5 text-foreground/40 shrink-0" />
                  <span className="text-[12px] text-foreground/40">
                    claude-code captured
                  </span>
                  <span className="text-[11px] text-foreground/25">
                    · backend-team
                  </span>
                  <span className="text-[12px] text-foreground/40 ml-auto tabular-nums">
                    8h
                  </span>
                </div>
                <span className="text-[14px] font-medium text-foreground truncate block">
                  Session: refactored ingest pipeline
                </span>
              </div>

              {/* Different hub */}
              <div className="px-4 py-2.5 hover:bg-foreground/3 cursor-pointer transition-colors border-t border-border/30">
                <div className="flex items-center gap-1.5 mb-0.5">
                  <div
                    className="h-3.5 w-3.5 rounded-full flex items-center justify-center text-[7px] font-medium text-foreground/55 shrink-0"
                    style={{
                      background: "oklch(from var(--foreground) l c h / 0.08)",
                    }}
                  >
                    A
                  </div>
                  <span className="text-[12px] text-foreground/40">
                    Alex pushed
                  </span>
                  <span className="text-[11px] text-foreground/25">
                    · design-team
                  </span>
                  <span className="text-[12px] text-foreground/40 ml-auto tabular-nums">
                    1d
                  </span>
                </div>
                <span className="text-[14px] font-medium text-foreground truncate block">
                  Figma token export workflow
                </span>
              </div>
            </div>
            <p className="text-[11px] text-foreground/25 mt-1.5">
              Chosen direction for `recent`: titles stay aligned, team rows use
              human-first attribution, and personal no-agent rows remain
              single-line.
            </p>
          </div>
        </div>
      </DemoCard>

      {/* ── 1c. Content type indicators ── */}
      <DemoCard label="1c. Content type indicators &mdash; personal tier only">
        <p className="text-[12px] text-fg-2 mb-4">
          Topic rows are intentionally quiet: icon or avatar only, then title
          and summary. No name, no `via`, no `captured` copy.
        </p>

        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <MemoryRow memory={MEMORIES.personalText} surface="topic" />
          <MemoryRow
            memory={MEMORIES.personalPdf}
            surface="topic"
            showDivider
          />
          <MemoryRow
            memory={MEMORIES.personalImage}
            surface="topic"
            showDivider
          />
          <MemoryRow
            memory={MEMORIES.personalLink}
            surface="topic"
            showDivider
          />
        </div>

        <p className="text-[11px] text-fg-4 mt-2">
          Text → neutral dot · PDF → FileText icon · Image → ImageIcon · Link →
          LinkIcon. Personal indicators stay quiet and non-semantic.
        </p>
      </DemoCard>

      {/* ── 1d. Surface presets — same memory, different contexts ── */}
      <DemoCard label="1d. Surface presets &mdash; same memory, 5 contexts">
        <p className="text-[12px] text-fg-2 mb-4">
          Same memory rendered on each surface. The canonical rule matrix lives
          in section 04; this page-level demo keeps the topic layout grounded in
          those rules.
        </p>

        <div className="space-y-3">
          {(
            [
              ["recent", "Recent — full meta + copy"],
              ["inbox", "Inbox — minimal, just title + age"],
              ["topic", "Topic — summary focus, no source"],
              ["list", "List — source + count + age"],
              ["recall", "Recall — summary focus, no source/count"],
            ] as [Surface, string][]
          ).map(([surface, label]) => (
            <div key={surface}>
              <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1">
                {label}
              </p>
              <div
                className="rounded-xl overflow-hidden"
                style={{
                  border: "1px solid var(--border)",
                  background: "var(--card)",
                }}
              >
                <MemoryRow
                  memory={MEMORIES.personalText}
                  surface={surface}
                  showCopy={surface === "recent"}
                />
              </div>
            </div>
          ))}
        </div>
      </DemoCard>

      {/* ── 1e. Processing state ── */}
      <DemoCard label="1e. Processing state &mdash; ✦ organizing...">
        <p className="text-[12px] text-fg-2 mb-4">
          Universal across all surfaces. ✦ breathes with signature color. No
          summary, no tags, no meta — just title + status.
        </p>
        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <MemoryRow memory={MEMORIES.processing} surface="recent" />
          <MemoryRow
            memory={MEMORIES.personalText}
            surface="recent"
            showDivider
            showCopy
          />
        </div>
      </DemoCard>

      {/* ── 1f. Topics page — full page layout ── */}
      <DemoCard label="1f. Topics page &mdash; Recent + Topics + Inbox">
        <p className="text-[12px] text-fg-2 mb-4">
          Full layout: page header → Recent section (Clock, dropdown time
          filter, Select) → topic cards grid → inbox (Inbox icon, count). Gap-3
          consistent spacing.
        </p>

        {/* Page header */}
        <div className="flex items-center justify-between mb-4">
          <div>
            <h2 className="text-[18px] font-semibold text-foreground">
              Your Topics
            </h2>
            <p className="text-[13px] text-fg-3">147 memories · 6 topics</p>
          </div>
        </div>

        <div className="flex flex-col gap-3">
          {/* Recent section */}
          <SectionCard
            icon={Clock}
            label="Recent"
            trailing={
              <div className="flex items-center gap-2">
                <button className="flex items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
                  Past 12h
                  <ChevronDown className="h-2.5 w-2.5" />
                </button>
                <button className="flex items-center gap-1 text-[12px] text-fg-4 hover:text-fg-3 transition-colors cursor-pointer">
                  <Copy className="h-3 w-3" />
                  <span>Copy 5 for AI</span>
                </button>
              </div>
            }
          >
            <div>
              <MemoryRow memory={MEMORIES.processing} surface="recent" />
              <MemoryRow
                memory={MEMORIES.personalText}
                surface="recent"
                showDivider
                showCopy
              />
              <MemoryRow
                memory={MEMORIES.personalPdf}
                surface="recent"
                showDivider
                showCopy
              />
            </div>
            <button className="w-full text-center text-[12px] text-fg-3 hover:text-fg-2 py-2 cursor-pointer transition-colors border-t border-border/30">
              +4 more
            </button>
          </SectionCard>

          {/* Topic grid */}
          <div className="grid grid-cols-3 gap-3">
            {MOCK_TOPICS.map((t) => (
              <TopicCard
                key={t.id}
                topic={t}
                onClick={() =>
                  setSelectedTopic(t.id === selectedTopic ? null : t.id)
                }
              />
            ))}
          </div>

          {/* Inbox */}
          <SectionCard
            icon={Inbox}
            label="Inbox"
            trailing={
              <span className="text-[13px] text-fg-3 tabular-nums">3</span>
            }
          >
            <p className="px-4 pb-2 text-[13px] text-fg-3">
              Will be organized in the next dream cycle
            </p>
            <div>
              <MemoryRow memory={MEMORIES.inbox1} surface="inbox" />
              <MemoryRow memory={MEMORIES.inbox2} surface="inbox" showDivider />
              <MemoryRow memory={MEMORIES.inbox3} surface="inbox" showDivider />
            </div>
          </SectionCard>
        </div>
      </DemoCard>

      {/* ── 1f-team. Recent — team attribution variant ── */}
      <DemoCard label="1f-team. Recent section &mdash; team hub attribution">
        <p className="text-[12px] text-fg-2 mb-4">
          Team-hub recent uses the same canonical `recent` row behavior from
          section 04: stacked attribution, human-first wording, and copy action
          in the row meta.
        </p>
        <SectionCard
          icon={Clock}
          label="Recent"
          trailing={
            <button className="flex items-center gap-1 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer">
              Past 1d
              <ChevronDown className="h-2.5 w-2.5" />
            </button>
          }
        >
          <div>
            <MemoryRow memory={MEMORIES.teamHuman} surface="recent" showCopy />
            <MemoryRow
              memory={MEMORIES.teamHumanViaAgent}
              surface="recent"
              showDivider
            />
            <MemoryRow
              memory={MEMORIES.agentCapture}
              surface="recent"
              showDivider
            />
          </div>
        </SectionCard>
      </DemoCard>

      {/* ── 1f-pagination. Progressive disclosure — Show 5 more + Collapse ── */}
      <DemoCard label="1f-pagination. Recent pagination — progressive disclosure, not infinite scroll">
        <div className="space-y-2 text-[13px] text-fg-2 mb-4">
          <p>
            <span className="text-fg-1 font-medium">Problem:</span> current
            production uses an IntersectionObserver sentinel to auto-load more
            memories on scroll. Once expanded, Recent fills the entire page and
            never stops. Users feel trapped in a continuously growing list with
            no way back.
          </p>
          <p>
            <span className="text-fg-1 font-medium">Principle:</span> infinite
            scroll is for passive consumption feeds (TikTok, Twitter timeline).
            Progressive disclosure is for task-oriented lists (Linear issues,
            Gmail inbox, Slack messages). Recent is task-oriented — users are
            reviewing what got added, deciding if they need to fix anything,
            scanning for something specific.
          </p>
          <p>
            <span className="text-fg-1 font-medium">Pattern:</span> show 5
            initial rows. Explicit &ldquo;Show 5 more&rdquo; button with
            remaining count. &ldquo;Collapse&rdquo; link appears in the section
            header trailing slot the moment the user expands past the initial 5
            — always visible, always one tap away.
          </p>
        </div>

        <div className="space-y-4">
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Interactive — tap &ldquo;Show 5 more&rdquo; to expand,
              &ldquo;收起&rdquo; to reset
            </p>
            <RecentProgressiveDisclosureDemo />
          </div>
        </div>

        <div className="mt-4 text-[11px] text-fg-3 space-y-1">
          <p>
            <span className="text-fg-2">Load batch:</span> 5 rows per tap. The
            constant <span className="font-mono">RECENT_BATCH = 5</span> can be
            tuned later but should stay small enough that each tap is a
            deliberate step, not a lazy expansion.
          </p>
          <p>
            <span className="text-fg-2">Initial limit:</span> 5 rows. Fresh
            visit always starts at 5, regardless of previous session state. No
            localStorage persistence of displayLimit — users don&apos;t want to
            inherit yesterday&apos;s expand state.
          </p>
          <p>
            <span className="text-fg-2">When remaining ≤ batch size:</span> CTA
            copy changes to{" "}
            <span className="font-mono">
              &ldquo;Show last N · 显示剩余 N 条&rdquo;
            </span>{" "}
            so the final tap shows the exact truth.
          </p>
          <p>
            <span className="text-fg-2">When all loaded:</span> CTA disappears
            entirely. Collapse affordance stays in the header as long as{" "}
            <span className="font-mono">displayLimit &gt; 5</span>.
          </p>
          <p>
            <span className="text-fg-2">Server pagination:</span> under the
            hood, cursor pagination still fetches in the background when the
            requested limit exceeds what&apos;s loaded. User sees one coherent
            &ldquo;Show more&rdquo; action regardless.
          </p>
          <p>
            <span className="text-fg-2">Same pattern applies to Inbox.</span>{" "}
            Inbox is also a task-oriented list section — use the identical
            disclosure mechanic. The constants{" "}
            <span className="font-mono">INBOX_INITIAL</span> /{" "}
            <span className="font-mono">INBOX_BATCH</span> can share values with
            Recent unless product signal says otherwise.
          </p>
        </div>
      </DemoCard>

      <DemoCard label="1f-pagination-why. Why infinite scroll is wrong here (industry reference)">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-[12px]">
            <thead className="text-fg-3 border-b border-border/40">
              <tr>
                <th className="py-2 pr-3 text-left font-medium">
                  Surface type
                </th>
                <th className="py-2 pr-3 text-left font-medium">
                  User&apos;s job
                </th>
                <th className="py-2 pr-3 text-left font-medium">Pattern</th>
                <th className="py-2 text-left font-medium">Examples</th>
              </tr>
            </thead>
            <tbody className="text-fg-2">
              <tr className="border-b border-border/20 align-top">
                <td className="py-2 pr-3 text-fg-3">
                  Passive consumption feed
                </td>
                <td className="py-2 pr-3">Keep me entertained</td>
                <td className="py-2 pr-3 text-fg-1">Infinite scroll ✓</td>
                <td className="py-2 text-fg-2">
                  TikTok, Instagram, Twitter timeline, Reddit home
                </td>
              </tr>
              <tr className="border-b border-border/20 align-top">
                <td className="py-2 pr-3 text-fg-3">Task-oriented list</td>
                <td className="py-2 pr-3">Find / review / act</td>
                <td className="py-2 pr-3 text-fg-1">
                  Progressive disclosure or pagination ✓
                </td>
                <td className="py-2 text-fg-2">
                  Linear, GitHub, Gmail, Notion, Slack, Things
                </td>
              </tr>
              <tr className="border-b border-border/20 align-top">
                <td className="py-2 pr-3 text-fg-3">Long-form content</td>
                <td className="py-2 pr-3">Read in order</td>
                <td className="py-2 pr-3 text-fg-1">Single page or TOC</td>
                <td className="py-2 text-fg-2">Medium, Substack, Wikipedia</td>
              </tr>
              <tr className="align-top">
                <td className="py-2 pr-3 text-fg-3">Dashboard / metrics</td>
                <td className="py-2 pr-3">Scan at a glance</td>
                <td className="py-2 pr-3 text-fg-1">
                  Fixed viewport, no scroll
                </td>
                <td className="py-2 text-fg-2">
                  Grafana, Datadog, Linear home
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p className="text-[11px] text-fg-3 mt-3">
          Recent, Inbox, Topic detail memory list, and Recall results are all{" "}
          <span className="text-fg-1">task-oriented lists</span> → progressive
          disclosure wins for all four. The current production infinite-scroll
          fallback in <span className="font-mono">RecentSection</span> is the
          exception to fix.
        </p>
      </DemoCard>

      {/* ── 1f-inbox. Inbox states — free / pro / organizing ── */}
      <DemoCard label="1f-inbox. Inbox section &mdash; 3 states">
        <p className="text-[12px] text-fg-2 mb-4">
          Inbox behavior differs by plan. Free users wait for nightly dream
          cycle. Pro users can trigger organization immediately. During
          organization, ✦ breathes and rows show progress.
        </p>
        <div className="space-y-3">
          {/* Free user — passive wait */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
              Free — wait for dream cycle
            </p>
            <SectionCard
              icon={Inbox}
              label="Inbox"
              trailing={
                <span className="text-[13px] text-fg-3 tabular-nums">3</span>
              }
            >
              <p className="px-4 pb-2 text-[13px] text-fg-3">
                Will be organized in the next dream cycle
              </p>
              <div>
                <MemoryRow memory={MEMORIES.inbox1} surface="inbox" />
                <MemoryRow
                  memory={MEMORIES.inbox2}
                  surface="inbox"
                  showDivider
                />
              </div>
            </SectionCard>
          </div>

          {/* Pro user — organize now CTA */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
              Pro — organize now
            </p>
            <SectionCard
              icon={Inbox}
              label="Inbox"
              trailing={
                <div className="flex items-center gap-2">
                  <span className="text-[13px] text-fg-3 tabular-nums">3</span>
                  <button
                    className="flex items-center gap-1 text-[12px] px-2 py-0.5 rounded-md transition-colors cursor-pointer hover:opacity-80"
                    style={{
                      color: DREAM_PURPLE,
                      background: `oklch(0.55 0.15 290 / 0.08)`,
                    }}
                  >
                    <span className="text-[12px] leading-none">✦</span>
                    <span>Organize</span>
                  </button>
                </div>
              }
            >
              <p className="px-4 pb-2 text-[13px] text-fg-3">
                3 memories waiting — organize now or let the dream cycle handle
                it
              </p>
              <div>
                <MemoryRow memory={MEMORIES.inbox1} surface="inbox" />
                <MemoryRow
                  memory={MEMORIES.inbox2}
                  surface="inbox"
                  showDivider
                />
                <MemoryRow
                  memory={MEMORIES.inbox3}
                  surface="inbox"
                  showDivider
                />
              </div>
            </SectionCard>
          </div>

          {/* Organizing — dream in progress */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-1.5">
              Organizing — dream triggered
            </p>
            <SectionCard
              icon={Inbox}
              label="Inbox"
              trailing={
                <span
                  className="flex items-center gap-1.5 text-[12px] state-slow-breathe"
                  style={{ color: DREAM_PURPLE }}
                >
                  <span className="text-[10px]">✦</span>
                  organizing 3 memories...
                </span>
              }
            >
              <div>
                {/* Rows dim during organization */}
                <div className="opacity-40">
                  <MemoryRow memory={MEMORIES.inbox1} surface="inbox" />
                  <MemoryRow
                    memory={MEMORIES.inbox2}
                    surface="inbox"
                    showDivider
                  />
                  <MemoryRow
                    memory={MEMORIES.inbox3}
                    surface="inbox"
                    showDivider
                  />
                </div>
              </div>
            </SectionCard>
          </div>
        </div>
      </DemoCard>

      {/* ── 1g. Topic detail — subtopics + ungrouped ── */}
      <DemoCard label="1g. Topic detail &mdash; description + subtopics + ungrouped">
        <p className="text-[12px] text-fg-2 mb-4">
          Inside a topic. Breadcrumb → header with AI description → subtopic
          sections → &ldquo;Other&rdquo; section for ungrouped memories. No kind
          tabs (kind deprecated from UI). Rows use surface=&quot;topic&quot;:
          summary visible, edge-to-edge (px-4, no wrapper padding).
        </p>

        <Breadcrumb items={["Your Topics", "Deployment"]} />

        {/* Header with description */}
        <div className="flex items-center gap-3 mb-1.5">
          <Rocket className="h-5 w-5 text-fg-3" />
          <div>
            <h2 className="text-[18px] font-semibold text-foreground">
              Deployment
            </h2>
            <p className="text-[13px] text-fg-3">23 memories · 3 subtopics</p>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <button className="flex items-center gap-1 text-[12px] text-fg-4 hover:text-fg-3 transition-colors cursor-pointer">
              <Copy className="h-3 w-3" />
              <span>Copy 23 for AI</span>
            </button>
            <Pin className="h-3.5 w-3.5 text-fg-4" />
          </div>
        </div>

        {/* AI description — from dream engine */}
        <p className="text-[14px] text-fg-2 mb-4">
          Blue-green deploys, rollback procedures, staging automation, and CI/CD
          pipeline configuration for Fly.io.
        </p>

        <div className="space-y-3">
          {/* Subtopic groups — edge-to-edge rows */}
          <SectionCard
            icon={Server}
            label="Staging"
            trailing={
              <span className="text-[12px] text-fg-4 tabular-nums">8</span>
            }
          >
            <div>
              <MemoryRow memory={MEMORIES.personalText} surface="topic" />
              <MemoryRow
                memory={MEMORIES.personalPdf}
                surface="topic"
                showDivider
              />
            </div>
          </SectionCard>

          <SectionCard
            icon={Zap}
            label="Production"
            trailing={
              <span className="text-[12px] text-fg-4 tabular-nums">11</span>
            }
          >
            <div>
              <MemoryRow memory={MEMORIES.personalLink} surface="topic" />
              <MemoryRow
                memory={MEMORIES.personalImage}
                surface="topic"
                showDivider
              />
            </div>
          </SectionCard>

          {/* Ungrouped — memories in this topic but no subtopic */}
          <SectionCard
            icon={Inbox}
            label="Other"
            trailing={
              <span className="text-[12px] text-fg-4 tabular-nums">4</span>
            }
          >
            <div>
              <MemoryRow
                memory={{
                  ...MEMORIES.inbox1,
                  title: "Fly.io multi-region notes",
                  age: "3d",
                }}
                surface="topic"
              />
              <MemoryRow
                memory={{
                  ...MEMORIES.inbox2,
                  title: "Deploy checklist draft",
                  age: "5d",
                }}
                surface="topic"
                showDivider
              />
            </div>
          </SectionCard>
        </div>
      </DemoCard>

      {/* ── 1g-flat. Topic detail — flat list (no subtopics) ── */}
      <DemoCard label="1g-flat. Topic detail &mdash; leaf topic with description">
        <p className="text-[12px] text-fg-2 mb-4">
          Leaf topic (no subtopics). Description shown under header. Memories in
          a single flat card. No kind tabs.
        </p>

        <Breadcrumb items={["Your Topics", "Go Patterns"]} />
        <div className="flex items-center gap-3 mb-1.5">
          <Code className="h-5 w-5 text-fg-3" />
          <div>
            <h2 className="text-[18px] font-semibold text-foreground">
              Go Patterns
            </h2>
            <p className="text-[13px] text-fg-3">31 memories</p>
          </div>
        </div>
        <p className="text-[14px] text-fg-2 mb-4">
          Error handling conventions, context propagation rules, interface
          design principles, and goroutine lifecycle patterns.
        </p>

        <div
          className="rounded-xl overflow-hidden"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div>
            <MemoryRow
              memory={{
                ...MEMORIES.personalText,
                title: "Error handling conventions",
              }}
              surface="topic"
            />
            <MemoryRow
              memory={{
                ...MEMORIES.personalPdf,
                title: "Context propagation rules",
              }}
              surface="topic"
              showDivider
            />
            <MemoryRow
              memory={{
                ...MEMORIES.personalLink,
                title: "Interface design principles",
              }}
              surface="topic"
              showDivider
            />
            <MemoryRow
              memory={{
                ...MEMORIES.personalImage,
                title: "Goroutine lifecycle patterns",
              }}
              surface="topic"
              showDivider
            />
          </div>
        </div>
      </DemoCard>

      {/* ── 1h. Topic card states ── */}
      <DemoCard label="1h. Topic card states &mdash; 4 variants">
        <p className="text-[12px] text-fg-2 mb-4">
          Cards show AI description for leaf topics and subtopic names for
          parents. No extra taxonomy indicators. Description comes from dream
          engine. h-full + flex-col pins age to bottom for consistent card
          height.
        </p>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Leaf + description
            </p>
            <TopicCard topic={MOCK_TOPICS[2]} />
          </div>
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Parent + subtopics
            </p>
            <TopicCard topic={MOCK_TOPICS[0]} />
          </div>
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Pinned leaf
            </p>
            <TopicCard topic={MOCK_TOPICS[5]} />
          </div>
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Empty (new)
            </p>
            <TopicCard
              topic={{
                id: "empty",
                name: "Q2 Planning",
                icon: FileText,
                count: 0,
              }}
            />
          </div>
        </div>
      </DemoCard>

      {/* ── 1h-next. Topic card redesign — memax identity ── */}
      <DemoCard label="1h-next. Topic cards &mdash; memax identity, not Notion">
        <p className="text-[12px] text-fg-2 mb-4">
          Cards are the right container. Problem: current cards look generic.
          Fix: AI description always visible, ✦ for dream signal, content
          preview for scent. Three options below.
        </p>

        {/* Option A: Content-dense — AI description + recent memory preview */}
        <div className="mb-6">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            A. Content-dense — description + memory preview
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {[
              {
                icon: Rocket,
                name: "Deployment",
                count: 23,
                subs: 3,
                desc: "Blue-green deploys, rollback procedures, staging automation",
                preview: "Fly.io machine-based blue-green with health checks",
              },
              {
                icon: Code,
                name: "Go Patterns",
                count: 31,
                subs: 0,
                desc: "Error handling, context propagation, interface design",
                preview: "Table-driven tests with testcontainers setup",
              },
              {
                icon: Layout,
                name: "Frontend",
                count: 22,
                subs: 2,
                desc: "React Server Components, Tailwind, Radix primitives",
                preview:
                  "TanStack Query infinite scroll with sentinel observer",
              },
            ].map((t) => {
              const Icon = t.icon;
              return (
                <div
                  key={t.name}
                  className="rounded-xl p-4 flex flex-col cursor-pointer transition-colors hover:bg-surface-1"
                  style={{
                    border: "1px solid var(--border)",
                    background: "var(--card)",
                  }}
                >
                  {/* Header: icon + name */}
                  <div className="flex items-center gap-2 mb-2">
                    <Icon className="h-4 w-4 text-fg-3 shrink-0" />
                    <span className="text-[15px] font-semibold text-foreground truncate">
                      {t.name}
                    </span>
                  </div>
                  {/* AI description */}
                  <p className="text-[13px] text-fg-2 line-clamp-1 mb-2">
                    {t.desc}
                  </p>
                  {/* Recent memory — content scent with ✦ */}
                  <div className="flex items-center gap-1.5 mb-2">
                    <span
                      className="text-[8px] shrink-0"
                      style={{ color: DREAM_PURPLE }}
                    >
                      ✦
                    </span>
                    <span className="text-[12px] text-fg-3 truncate">
                      {t.preview}
                    </span>
                  </div>
                  {/* Footer: count + subs */}
                  <div className="flex items-center gap-2 mt-auto pt-1">
                    <span className="text-[12px] text-fg-3 tabular-nums">
                      {t.count} memories
                    </span>
                    {t.subs > 0 && (
                      <span className="text-[12px] text-fg-4">
                        · {t.subs} subtopics
                      </span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Option B: Compact — tighter, icon-led identity */}
        <div className="mb-6">
          <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
            B. Compact — icon-led, description, sub-topics as · text
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {[
              {
                icon: Rocket,
                name: "Deployment",
                count: 23,
                subs: ["Staging", "Production", "Rollback"],
                desc: "Blue-green deploys, rollback procedures",
              },
              {
                icon: ShieldCheck,
                name: "Authentication",
                count: 18,
                subs: [],
                desc: "OAuth2 refresh flow, token rotation",
              },
              {
                icon: Code,
                name: "Go Patterns",
                count: 31,
                subs: [],
                desc: "Error handling, context propagation",
              },
              {
                icon: Database,
                name: "Database",
                count: 15,
                subs: [],
                desc: "Connection pooling, migration strategy",
              },
              {
                icon: Layout,
                name: "Frontend",
                count: 22,
                subs: ["Components", "Styling"],
                desc: "RSC, Tailwind, Radix primitives",
              },
              {
                icon: FileText,
                name: "Team Docs",
                count: 9,
                subs: [],
                desc: "Onboarding, code review guide",
              },
            ].map((t) => {
              const Icon = t.icon;
              return (
                <div
                  key={t.name}
                  className="rounded-xl px-4 py-3 cursor-pointer transition-colors hover:bg-surface-1"
                  style={{
                    border: "1px solid var(--border)",
                    background: "var(--card)",
                  }}
                >
                  <div className="flex items-center gap-2">
                    <Icon className="h-3.5 w-3.5 text-fg-3 shrink-0" />
                    <span className="text-[14px] font-medium text-foreground truncate flex-1">
                      {t.name}
                    </span>
                    <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
                      {t.count}
                    </span>
                  </div>
                  <p className="text-[12px] text-fg-3 truncate mt-1 pl-[calc(0.375rem+0.5rem+0.5rem)]">
                    {t.desc}
                  </p>
                  {t.subs.length > 0 && (
                    <p className="text-[11px] text-fg-4 mt-0.5 pl-[calc(0.375rem+0.5rem+0.5rem)]">
                      {t.subs.join(" · ")}
                    </p>
                  )}
                </div>
              );
            })}
          </div>
        </div>

        {/* Design notes */}
        <div className="mt-4 p-3 rounded-lg bg-surface-1 text-[11px] text-fg-3 space-y-1">
          <p className="font-medium text-fg-2">memax identity signals:</p>
          <p>
            • ✦ + dream-purple = AI organized this, not the user
            <br />
            • Memory preview = content scent (what&apos;s IN the topic)
            <br />
            • Description = dream engine output
            <br />• No timestamps — recent activity via ✦ preview
          </p>
        </div>
      </DemoCard>

      {/* ── 1i. Empty states ── */}
      <DemoCard label="1i. Empty states &mdash; 3 variants">
        <div className="grid grid-cols-3 gap-4">
          {/* Pre-dream */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Pre-dream (waiting)
            </p>
            <div
              className="rounded-xl p-6"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex flex-col items-center text-center gap-2">
                <span
                  className="text-[14px]"
                  style={{ color: "var(--signature)" }}
                >
                  ✦
                </span>
                <p className="text-[14px] text-fg-2">
                  Topics appear after your first dream
                </p>
                <p className="text-[13px] text-fg-3">
                  memax will organize your memories by subject during the
                  nightly dream cycle.
                </p>
              </div>
            </div>
          </div>

          {/* Clean inbox */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Inbox clean
            </p>
            <div
              className="rounded-xl p-6"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex flex-col items-center text-center gap-1.5">
                <span style={{ color: DREAM_PURPLE, fontSize: 15 }}>✦</span>
                <p className="text-[14px] text-fg-2">Your memory is clean</p>
              </div>
            </div>
          </div>

          {/* Dreams disabled */}
          <div>
            <p className="text-[10px] text-fg-3 uppercase tracking-wider mb-2">
              Dreams disabled
            </p>
            <div
              className="rounded-xl p-6"
              style={{
                border: "1px solid var(--border)",
                background: "var(--card)",
              }}
            >
              <div className="flex flex-col items-center text-center gap-1.5">
                <span style={{ color: DREAM_PURPLE, fontSize: 15 }}>✦</span>
                <p className="text-[14px] text-fg-2">Dreams are turned off</p>
                <p className="text-[13px] text-fg-3">
                  Enable dreams to organize your memories.
                </p>
                <button
                  className="text-[13px] cursor-pointer transition-colors hover:opacity-80 underline mt-1"
                  style={{ color: DREAM_PURPLE }}
                >
                  Enable in Settings →
                </button>
              </div>
            </div>
          </div>
        </div>
      </DemoCard>

      {/* ── Design notes ── */}
      <div className="text-[12px] text-fg-4 space-y-1 mt-2">
        <p>
          <strong className="text-fg-3">Unified MemoryRow:</strong> One
          component across all surfaces. Canonical behavior now lives in section
          04. This page-level section demonstrates how those row rules compose
          inside Recent, Topics, and Inbox.
        </p>
        <p>
          <strong className="text-fg-3">Kind deprecation:</strong> Subkind
          labels and color coding removed from rows and cards. Topics + tags
          provide better navigation. Kind tabs removed from topic detail.
        </p>
        <p>
          <strong className="text-fg-3">Topic cards:</strong> Leaf topics show
          AI description (from dream engine). Parent topics show subtopic names
          with chevrons. No extra taxonomy indicators. h-full + flex-col pins
          age to bottom for consistent grid height.
        </p>
        <p>
          <strong className="text-fg-3">Topic detail:</strong> Description under
          header (from dream engine). Subtopic groups + &ldquo;Other&rdquo;
          section for ungrouped memories. No kind tabs. Edge-to-edge rows (px-4,
          no px-1.5 wrapper).
        </p>
        <p>
          <strong className="text-fg-3">Hub attribution:</strong> Hub name lives
          in the canonical row rules in section 04. Current chosen split:
          `recent` uses stacked attribution, `recall` stays compact, `topic`
          stays icon-only, and `inbox` stays minimal.
        </p>
        <p>
          <strong className="text-fg-3">Rows:</strong> Edge-to-edge within card
          (px-4). No rounded-lg. Flat dividers (border-t border-border/30).
          hover:bg-surface-1.
        </p>
        <p>
          <strong className="text-fg-3">Copy:</strong> Per-row copy (markdown)
          on Recent surface only. Group copy is explicit-intent only — enter
          Select mode → batch toolbar Copy (context block format). Section
          headers no longer host a group-level &ldquo;Copy N for AI&rdquo;
          button.
        </p>
      </div>
    </Section>
  );
}
