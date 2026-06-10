// ═══════════════════════════════════════════════
// 35. Data Section Card — universal state machine for data-bound cards
//
// THE PROBLEM
// Every "list inside a card" surface in memax reinvents state handling:
// Recent, Inbox, Topic subtopics, Agent configs, Hub members, Dream review.
// Results drift: some `return null` on refetch (card vanishes),
// some show generic "暂无结果", some skeleton-flash on filter change,
// some collapse multi-query errors into one red page.
//
// THE RULE
// A mounted data card has ONE state machine with FIVE phases, and never
// unmounts while the query is alive. `<DataSectionCard>` is the primitive.
//
// It COMPOSES existing primitives — does not reinvent them:
//   • Loading state   → <Skeleton> from @memaxlabs/ui
//   • Error state     → <ContentError plain> from @memaxlabs/ui
//   • Row dividers    → border-border/30 (design law 8)
//
// REFETCH IS SILENT (see 35c). Background refetch does NOT pulse, dim, or
// skeleton — rows on screen are already accurate. The only async visual
// signal on a loaded card is `isPlaceholderData` (35d) which dims the body
// to 0.72 because the rows are about to be REPLACED (hub/filter pivot).
//
// CONTENT CHANGE IS VISIBLE (see 35e). When a new row arrives (SSE push
// from an agent, mutation optimistic insert, etc.) the ROW animates in
// via `state-memory-arrive`. Card chrome stays still; content moves.
//
// Maps to production files that should migrate to this pattern:
//   packages/web/src/components/features/topic-grid.tsx   (RecentSection, InboxSection)
//   packages/web/src/components/features/topic-detail.tsx (subtopic groups, ungrouped)
//   packages/web/src/components/features/agent-configs-section.tsx
//   packages/web/src/components/features/hub-management-view.tsx (members, invites)
//   packages/web/src/components/features/related-memories.tsx
//   packages/web/src/components/features/topic-siblings.tsx
//   packages/web/src/components/features/topic-pills.tsx
//
// Codex handoff: extract `DataSectionCard` below into
// `packages/ui/src/components/data-section-card.tsx` and wire each
// production callsite to it. This file is the north star.
// ═══════════════════════════════════════════════
"use client";

import { useEffect, useState } from "react";
import {
  Clock,
  Inbox,
  ChevronDown,
  SlidersHorizontal,
  Bot,
} from "lucide-react";
import { Skeleton, DataCardSkeleton, DataSectionCard } from "@memaxlabs/ui";
import { Section, DemoCard, SIGNATURE } from "../_shared";

const NEUTRAL_DOT = "oklch(from var(--foreground) l c h / 0.2)";

// ─── Mock row (mirror of section 01's canonical MemoryRow — recent surface) ───
//
// Section 01 defines the full MemoryRow with 5 surfaces. Here we only need
// the `recent` variant (stacked: attribution line 1, then title, then
// summary). Keep in sync with section 01 if the shape changes.

interface MockMemory {
  id: string;
  title: string;
  summary?: string;
  agent?: string;
  age: string;
  dotColor?: string;
}

const MOCK: MockMemory[] = [
  {
    id: "1",
    title: "Fly.io blue-green with health checks",
    summary:
      "Zero-downtime deploys using blue-green on Fly.io machines, automatic rollback on health check fail.",
    agent: "claude-code",
    age: "14m",
  },
  {
    id: "2",
    title: "OAuth refresh token rotation strategy",
    summary:
      "Rotate refresh tokens on every use with a 30-day expiry window and revoke on suspicious activity.",
    agent: "cursor",
    age: "1h",
  },
  {
    id: "3",
    title: "pgvector HNSW index tuning",
    summary:
      "m=16, ef_construction=64 for 100k+ vectors. Recall ~96% at query ef=40.",
    age: "3h",
    dotColor: NEUTRAL_DOT,
  },
];

const MOCK_ALT: MockMemory[] = [
  {
    id: "b1",
    title: "Design: mode capsule morph transition",
    summary:
      "Translucent pill in bar that morphs upward into a glass card — replaces floating pills.",
    agent: "derek",
    age: "8m",
  },
  {
    id: "b2",
    title: "Kitchen section registry conventions",
    summary:
      "Every section registers in NAV_TREE and SECTION_COMPONENTS. Page re-renders on hot reload.",
    agent: "claude-code",
    age: "2h",
  },
  {
    id: "b3",
    title: "Inbox empty state — brand voice decision",
    summary:
      "Left-aligned warm voice inside the card body. Never centered, never generic.",
    age: "5h",
    dotColor: NEUTRAL_DOT,
  },
];

const ARRIVAL_MEMORIES: MockMemory[] = [
  {
    id: "new-1",
    title: "Ship it Friday — bar redesign lands",
    summary:
      "Mode capsule + silent refetch + data section card all merged to main.",
    agent: "claude-code",
    age: "just now",
  },
  {
    id: "new-2",
    title: "Kitchen section 35 finalized",
    summary:
      "DataSectionCard composes Skeleton + ContentError. Silent refetch rule documented.",
    agent: "cursor",
    age: "just now",
  },
  {
    id: "new-3",
    title: "Recall error UI — never fall through to no-results",
    summary:
      "Bar recall gets <ContentError compact> branch with specific retry copy.",
    agent: "claude-code",
    age: "just now",
  },
  {
    id: "new-4",
    title: "Hub identity chip — you are here",
    summary:
      "Persistent pill above topics h2 when user has 2+ hubs. Click opens switcher.",
    agent: "derek",
    age: "just now",
  },
];

function MockRow({
  m,
  showDivider = false,
  className = "",
}: {
  m: MockMemory;
  showDivider?: boolean;
  className?: string;
}) {
  const stacked = !!m.agent;
  return (
    <div
      className={`px-4 py-2.5 hover:bg-surface-1 cursor-pointer transition-colors ${showDivider ? "border-t border-border/30" : ""} ${className}`}
    >
      {stacked ? (
        <>
          <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
            <Bot className="h-3.5 w-3.5 text-fg-3 shrink-0" />
            <span className="text-foreground/40">{m.agent} captured</span>
            <span className="ml-auto text-foreground/40 tabular-nums">
              {m.age}
            </span>
          </div>
          <div className="text-[14px] font-medium text-foreground truncate">
            {m.title}
          </div>
          {m.summary && (
            <p className="text-[13px] text-fg-2 truncate mt-0.5">{m.summary}</p>
          )}
        </>
      ) : (
        <>
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 flex items-center justify-center shrink-0">
              <span
                className="h-1.5 w-1.5 rounded-full"
                style={{
                  backgroundColor: m.dotColor ?? NEUTRAL_DOT,
                }}
              />
            </span>
            <span className="text-[14px] font-medium text-foreground truncate flex-1 min-w-0">
              {m.title}
            </span>
            <span className="text-[13px] text-fg-3 tabular-nums shrink-0">
              {m.age}
            </span>
          </div>
          {m.summary && (
            <p className="text-[13px] text-fg-2 truncate mt-0.5 pl-5">
              {m.summary}
            </p>
          )}
        </>
      )}
    </div>
  );
}

function MockRows({ data = MOCK }: { data?: MockMemory[] }) {
  return (
    <>
      {data.map((m, i) => (
        <MockRow key={m.id} m={m} showDivider={i > 0} />
      ))}
    </>
  );
}

type CardPhase = Parameters<typeof DataSectionCard>[0]["phase"];

// ─── Shared trailing chip (matches production Recent filter trigger shape) ───

function FilterChipMock({ label = "Past 12h" }: { label?: string }) {
  return (
    <button
      type="button"
      className="flex items-center gap-1.5 text-[12px] text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
    >
      <SlidersHorizontal className="h-3 w-3" />
      <span>{label}</span>
      <ChevronDown className="h-2.5 w-2.5" />
    </button>
  );
}

// ─── Memory arrival animation — keyframe injected locally for kitchen demo ───
//
// Production will add this as a global class in globals.css:
//   .state-memory-arrive { animation: memaxMemoryArrive 820ms var(--ease-spring) both; }
// plus the keyframe. For kitchen, the <style> child below scopes it.

function MemoryArriveKeyframes() {
  return (
    <style>{`
      @keyframes memaxMemoryArrive {
        0% {
          opacity: 0;
          transform: translateY(-4px);
          box-shadow:
            inset 1.5px 0 0 oklch(from var(--signature) l c h / 0.12),
            0 10px 24px color-mix(in oklch, var(--foreground) 6%, transparent);
        }
        18% {
          opacity: 1;
          transform: translateY(0);
          box-shadow:
            inset 1.5px 0 0 oklch(from var(--signature) l c h / 0.1),
            0 6px 16px color-mix(in oklch, var(--foreground) 4%, transparent);
        }
        60% {
          box-shadow:
            inset 1px 0 0 oklch(from var(--signature) l c h / 0.05),
            0 2px 8px color-mix(in oklch, var(--foreground) 2%, transparent);
        }
        100% {
          opacity: 1;
          transform: translateY(0);
          box-shadow: inset 0 0 0 transparent, 0 0 0 transparent;
        }
      }
      .state-memory-arrive {
        animation: memaxMemoryArrive 820ms cubic-bezier(0.16, 1, 0.3, 1) both;
      }
    `}</style>
  );
}

// ─── Demo: phase cycler (interactive) ───

const PHASE_SEQUENCE: CardPhase[] = [
  "loading",
  "loaded",
  "filtered-empty",
  "empty",
  "error",
];

function PhaseCyclerDemo() {
  const [phase, setPhase] = useState<CardPhase>("loaded");

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-1.5 flex-wrap">
        {PHASE_SEQUENCE.map((p) => (
          <button
            key={p}
            onClick={() => setPhase(p)}
            className={`px-2.5 py-1 rounded-md text-[11px] transition-all cursor-pointer ${
              phase === p
                ? "bg-foreground text-background font-medium"
                : "bg-surface-2 text-fg-2 hover:bg-surface-3"
            }`}
          >
            {p}
          </button>
        ))}
      </div>
      <DataSectionCard
        icon={Clock}
        label="Recent"
        trailing={<FilterChipMock />}
        phase={phase}
        emptyCopy={{
          title: "Nothing new yet",
          hint: "Capture something and it'll show up here",
        }}
        filteredEmptyCopy={{
          title: "No memories in the past 12 hours",
          hint: "Try widening the window or switching actor",
          clearLabel: "Clear filters",
          onClear: () => setPhase("loaded"),
        }}
        errorCopy={{
          title: "Couldn't reach your recent memories",
          detail: "Connection timed out. Your memories are safe.",
          retryLabel: "Reload recent",
          onRetry: () => setPhase("loading"),
        }}
      >
        <MockRows />
      </DataSectionCard>
    </div>
  );
}

// ─── Demo: silent refetch (documentation card) ───

function SilentRefetchDemo() {
  return (
    <div className="space-y-3">
      <DataSectionCard
        icon={Clock}
        label="Recent"
        trailing={<FilterChipMock />}
        phase="loaded"
      >
        <MockRows />
      </DataSectionCard>
      <div className="text-[12px] text-fg-2 space-y-1.5">
        <p>
          React Query will re-fetch this card in the background on window focus,
          after mutations, on interval, etc. The card{" "}
          <strong className="text-fg-1">does not react visually</strong> to any
          of it — the rows on screen are already accurate.
        </p>
        <p className="text-fg-3">
          Reserve motion for moments that change what the user sees. Background
          sync does not. For the case where data IS about to change (hub /
          filter pivot) see 35d. For the case where a NEW row arrives (SSE push,
          optimistic insert) see 35e — the row animates in, the card chrome
          stays still.
        </p>
      </div>
    </div>
  );
}

// ─── Demo: cache pivot (isPlaceholderData — previous data held, body dims) ───

function CachePivotDemo() {
  const [hub, setHub] = useState<"personal" | "team">("personal");
  const [isPlaceholder, setIsPlaceholder] = useState(false);
  const [displayData, setDisplayData] = useState(MOCK);

  useEffect(() => {
    setIsPlaceholder(true);
    const t = setTimeout(() => {
      setDisplayData(hub === "personal" ? MOCK : MOCK_ALT);
      setIsPlaceholder(false);
    }, 700);
    return () => clearTimeout(t);
  }, [hub]);

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-1.5">
        {(["personal", "team"] as const).map((h) => (
          <button
            key={h}
            onClick={() => setHub(h)}
            className={`px-2.5 py-1 rounded-md text-[11px] transition-all cursor-pointer ${
              hub === h
                ? "bg-foreground text-background font-medium"
                : "bg-surface-2 text-fg-2 hover:bg-surface-3"
            }`}
          >
            {h === "personal" ? "Personal hub" : "Team hub"}
          </button>
        ))}
      </div>
      <DataSectionCard
        icon={Clock}
        label="Recent"
        trailing={<FilterChipMock />}
        phase="loaded"
        isPlaceholderData={isPlaceholder}
      >
        <MockRows data={displayData} />
      </DataSectionCard>
      <p className="text-[10px] text-fg-4">
        Data is PLACEHOLDER — previous hub&apos;s rows held visible while the
        new hub&apos;s query resolves (
        <span className="font-mono">placeholderData: (prev) =&gt; prev</span>
        ). Body dims to 0.72 signaling &ldquo;outgoing,&rdquo; rows swap in
        place when new data arrives. Never drops to a skeleton. The dim is the
        ONLY visual cue — no pulse, no star, no spinner.
      </p>
    </div>
  );
}

// ─── Demo: memory arrival animation (SSE / optimistic insert) ───

function MemoryArrivalDemo() {
  const [rows, setRows] = useState<MockMemory[]>(MOCK);
  const [arrivedIds, setArrivedIds] = useState<Set<string>>(new Set());
  const [nextIdx, setNextIdx] = useState(0);

  const pushOne = () => {
    const nextMemory = ARRIVAL_MEMORIES[nextIdx % ARRIVAL_MEMORIES.length];
    const newRow: MockMemory = {
      ...nextMemory,
      id: `${nextMemory.id}-${Date.now()}`,
    };
    setRows((prev) => [newRow, ...prev]);
    setArrivedIds((prev) => new Set(prev).add(newRow.id));
    setNextIdx((i) => i + 1);
    // Clear the "new" flag after the animation settles so the row becomes
    // visually identical to the rest of the list.
    setTimeout(() => {
      setArrivedIds((prev) => {
        const next = new Set(prev);
        next.delete(newRow.id);
        return next;
      });
    }, 2400);
  };

  const reset = () => {
    setRows(MOCK);
    setArrivedIds(new Set());
    setNextIdx(0);
  };

  return (
    <div className="space-y-3">
      <MemoryArriveKeyframes />
      <div className="flex items-center gap-1.5">
        <button
          onClick={pushOne}
          className="px-2.5 py-1 rounded-md text-[11px] font-medium bg-foreground text-background hover:opacity-90 transition-opacity cursor-pointer"
        >
          ↓ Simulate agent pushing a new memory
        </button>
        <button
          onClick={reset}
          className="px-2.5 py-1 rounded-md text-[11px] text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
        >
          Reset
        </button>
      </div>
      <DataSectionCard
        icon={Clock}
        label="Recent"
        trailing={<FilterChipMock />}
        phase="loaded"
      >
        <div>
          {rows.map((m, i) => (
            <MockRow
              key={m.id}
              m={m}
              showDivider={i > 0}
              className={arrivedIds.has(m.id) ? "state-memory-arrive" : ""}
            />
          ))}
        </div>
      </DataSectionCard>
      <div className="text-[12px] text-fg-2 space-y-1.5">
        <p>
          A new row lands softly with a brief left-edge signature trace and a
          smoky neutral lift, then settles into a normal row in under a second.
          The <strong className="text-fg-1">card chrome does not react</strong>{" "}
          — no pulsing header, no card dim, no skeleton. Only the new content
          moves.
        </p>
        <p className="text-fg-3">
          This IS a legitimate motion moment: an agent just captured something,
          the user&apos;s surface gained content, memax signals &ldquo;memax AI
          produced this&rdquo; with a restrained signature trace instead of a
          tinted block (design law 4 — signature = intelligence working), while
          the rest of the effect stays in quiet neutral grays. The accent
          disappears quickly so the row becomes indistinguishable from existing
          rows once the eye registers the arrival.
        </p>
        <p className="text-fg-4">
          Production: <span className="font-mono">state-memory-arrive</span>{" "}
          class in <span className="font-mono">globals.css</span>. Applied by
          the row component when the caller marks the memory as freshly inserted
          (SSE event handler in{" "}
          <span className="font-mono">memax-event-bridge.tsx</span> sets a flag
          on cache-inserted rows, row reads the flag, applies class, clears flag
          after 2.4s).
        </p>
      </div>
    </div>
  );
}

// ─── Demo: 5 phases side-by-side (static snapshot) ───

function StaticPhaseRow({
  phase,
  card,
}: {
  phase: string;
  card: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
        {phase}
      </p>
      {card}
    </div>
  );
}

function FiveStatesGrid() {
  const noop = () => {};
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <StaticPhaseRow
        phase="loading (first load)"
        card={
          <DataSectionCard
            icon={Clock}
            label="Recent"
            trailing={<FilterChipMock />}
            phase="loading"
          />
        }
      />
      <StaticPhaseRow
        phase="loaded"
        card={
          <DataSectionCard
            icon={Clock}
            label="Recent"
            trailing={<FilterChipMock />}
            phase="loaded"
          >
            <MockRows />
          </DataSectionCard>
        }
      />
      <StaticPhaseRow
        phase="loaded + isPlaceholderData (body dims)"
        card={
          <DataSectionCard
            icon={Clock}
            label="Recent"
            trailing={<FilterChipMock />}
            phase="loaded"
            isPlaceholderData
          >
            <MockRows />
          </DataSectionCard>
        }
      />
      <StaticPhaseRow
        phase="filtered-empty"
        card={
          <DataSectionCard
            icon={Clock}
            label="Recent"
            trailing={<FilterChipMock />}
            phase="filtered-empty"
            filteredEmptyCopy={{
              title: "No memories in the past 12 hours",
              hint: "Try widening the window or switching actor",
              clearLabel: "Clear filters",
              onClear: noop,
            }}
          />
        }
      />
      <StaticPhaseRow
        phase="empty (true — no filter)"
        card={
          <DataSectionCard
            icon={Inbox}
            label="Inbox"
            phase="empty"
            emptyCopy={{
              title: "Inbox is clear",
              hint: "New captures land here before the next dream cycle",
            }}
          />
        }
      />
      <StaticPhaseRow
        phase="error"
        card={
          <DataSectionCard
            icon={Clock}
            label="Recent"
            trailing={<FilterChipMock />}
            phase="error"
            errorCopy={{
              title: "Couldn't reach your recent memories",
              detail: "Connection timed out. Your memories are safe.",
              retryLabel: "Reload recent",
              onRetry: noop,
            }}
          />
        }
      />
    </div>
  );
}

// ─── Demo: anti-patterns vs correct ───

function AntiPatternsDemo() {
  const noop = () => {};
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {/* Row 1 — return null vs mounted shell */}
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ❌ return null on refetch / empty
        </p>
        <div
          className="rounded-xl border border-dashed border-destructive/40 p-6 flex items-center justify-center h-28"
          style={{
            background: "oklch(from var(--destructive) l c h / 0.04)",
          }}
        >
          <p className="text-[12px] text-fg-3 italic text-center">
            (component returns null —<br />
            section vanishes — layout jumps)
          </p>
        </div>
        <p className="text-[10px] text-fg-4 font-mono">
          if (!data?.length) return null;
        </p>
      </div>
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ✅ mounted shell, rows stay, refetch is silent
        </p>
        <DataSectionCard
          icon={Clock}
          label="Recent"
          trailing={<FilterChipMock />}
          phase="loaded"
        >
          <MockRows />
        </DataSectionCard>
        <p className="text-[10px] text-fg-4">
          Card stays mounted during background refetch. No pulsing ✦, no body
          dim. Data on screen is accurate — motion would only distract.
        </p>
      </div>

      {/* Row 2 — generic fallback copy vs surface-specific */}
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ❌ generic fallback copy
        </p>
        <div
          className="rounded-xl p-4"
          style={{
            border: "1px solid oklch(from var(--destructive) l c h / 0.25)",
            background: "var(--card)",
          }}
        >
          <div className="text-[14px] font-semibold text-fg-2 uppercase tracking-wide mb-2">
            Recent
          </div>
          <p className="text-[14px] text-fg-3">暂无结果</p>
          <button className="text-[12px] text-fg-3 mt-2">清除筛选</button>
        </div>
        <p className="text-[10px] text-fg-4">
          Reuses <span className="font-mono">t.states.empty.transient</span> —
          no surface context, no brand voice, no echo of the active filter.
        </p>
      </div>
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ✅ surface-specific + filter echo
        </p>
        <DataSectionCard
          icon={Clock}
          label="Recent"
          trailing={<FilterChipMock />}
          phase="filtered-empty"
          filteredEmptyCopy={{
            title: "No memories in the past 12 hours",
            hint: "Try widening the window or switching actor",
            clearLabel: "Clear filters",
            onClear: noop,
          }}
        />
        <p className="text-[10px] text-fg-4">
          Title echoes the filter. Clear action names what will change.
        </p>
      </div>

      {/* Row 3 — reinvented error vs ContentError primitive */}
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ❌ reinvented error visual
        </p>
        <div
          className="rounded-xl p-4 flex items-start gap-3"
          style={{
            border: "1px solid var(--border)",
            background: "var(--card)",
          }}
        >
          <div className="h-4 w-4 rounded-full bg-fg-3 shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0">
            <p className="text-[14px] text-fg-2 font-medium">
              Something went wrong
            </p>
            <button className="mt-2 text-[12px]" style={{ color: SIGNATURE }}>
              ↻ Try again
            </button>
          </div>
        </div>
        <p className="text-[10px] text-fg-4">
          Signature color on a retry button{" "}
          <span className="font-mono">violates Law 4</span> (Dream Violet = AI
          working, NEVER decorative). Generic &ldquo;Something went wrong&rdquo;
          also violates the no-fallback-copy rule.
        </p>
      </div>
      <div className="space-y-2">
        <p className="text-[10px] text-fg-3 uppercase tracking-wider font-medium">
          ✅ ContentError primitive + specific copy
        </p>
        <DataSectionCard
          icon={Clock}
          label="Recent"
          trailing={<FilterChipMock />}
          phase="error"
          errorCopy={{
            title: "Couldn't reach your recent memories",
            detail: "Connection timed out. Your memories are safe.",
            retryLabel: "Reload recent",
            onRetry: noop,
          }}
        />
        <p className="text-[10px] text-fg-4">
          Uses <span className="font-mono">&lt;ContentError plain&gt;</span> —
          red pulsing dot, neutral retry link, specific action label.
        </p>
      </div>
    </div>
  );
}

// ─── API contract table ───

const CONTRACT_ROWS: Array<[string, string, string]> = [
  [
    "phase",
    "'loading' | 'loaded' | 'empty' | 'filtered-empty' | 'error'",
    "Caller derives from React Query flags. See 35h for the derivation.",
  ],
  [
    "isPlaceholderData",
    "boolean",
    "Previous query's data held while the new one runs (hub / filter pivot). Dims body to 0.72. Only visual signal for any async state on a loaded card.",
  ],
  [
    "icon",
    "LucideIcon (optional)",
    "Header icon. Clock for Recent, Inbox for Inbox, etc.",
  ],
  [
    "label",
    "string",
    "Header label. MUST differ from any wrapping h2 to avoid repetition.",
  ],
  ["trailing", "ReactNode", "Filter chip, count badge, copy / select actions."],
  [
    "skeleton",
    "ReactNode (optional)",
    "Custom shape for non-row surfaces. Defaults to row-shaped <Skeleton> mirror.",
  ],
  [
    "emptyCopy",
    "{ title, hint? }",
    "Required when phase = 'empty'. No filter active; surface is truly empty.",
  ],
  [
    "filteredEmptyCopy",
    "{ title, hint?, clearLabel, onClear }",
    "Required when phase = 'filtered-empty'. Title MUST echo the active filter.",
  ],
  [
    "errorCopy",
    "{ title, detail?, retryLabel, onRetry }",
    "Required when phase = 'error'. retryLabel is specific (e.g. 'Reload recent'), never 'Try again'.",
  ],
  [
    "children",
    "ReactNode",
    "Rows or custom body. Only rendered when phase = 'loaded'.",
  ],
];

function ContractTable() {
  return (
    <div className="text-[12px]">
      <div className="grid grid-cols-[180px_1fr_1fr] gap-x-4 gap-y-2">
        <div className="text-fg-3 uppercase tracking-wider text-[10px] font-medium">
          prop
        </div>
        <div className="text-fg-3 uppercase tracking-wider text-[10px] font-medium">
          type
        </div>
        <div className="text-fg-3 uppercase tracking-wider text-[10px] font-medium">
          contract
        </div>
        {CONTRACT_ROWS.map(([prop, type, contract]) => (
          <div key={prop} style={{ display: "contents" }}>
            <div className="text-fg-1 font-mono">{prop}</div>
            <div className="text-fg-2 font-mono text-[11px]">{type}</div>
            <div className="text-fg-2">{contract}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Caller template ───

function CallerTemplate() {
  return (
    <pre className="text-[11px] text-fg-2 font-mono leading-[1.55] overflow-x-auto bg-surface-1 rounded-lg p-3">
      {`// Recent section — production wiring target
const RECENT_PREVIEW_LIMIT = 5;
const {
  data, isPending, isError, isPlaceholderData, refetch, fetchNextPage,
  hasNextPage, isFetchingNextPage,
} = useRecentMemories({
  hubId,
  window,
  actor,
  expanded: visibleCount > RECENT_PREVIEW_LIMIT,
});

const memories = flattenRecentMemories(data);
const visible = memories.slice(0, visibleCount);
const expanded = visibleCount > RECENT_PREVIEW_LIMIT;
const hasMore = visible.length < memories.length || hasNextPage;
const canLoadMore = visible.length < memories.length || hasNextPage;
const nextIncrement = Math.min(5, Math.max(memories.length - visible.length, 5));
const isFilterActive = window !== "7d" || actor !== "all";

const phase: CardPhase =
  isPending && !data          ? "loading"
  : isError && !data          ? "error"
  : memories.length === 0 && isFilterActive ? "filtered-empty"
  : memories.length === 0     ? "empty"
  :                             "loaded";

// Note: we do NOT pass isFetching. Background refetch is silent by
// design — the rows on screen are already accurate (see 35c).
return (
  <DataSectionCard
    icon={Clock}
    label={t.memoryView.freshMemory.other}
    trailing={<RecentFilterTrigger window={window} actor={actor} />}
    phase={phase}
    isPlaceholderData={phase === "loaded" && isPlaceholderData}
    emptyCopy={{
      title: t.memoryView.recentEmptyTitle,
      hint:  t.memoryView.recentEmptyHint,
    }}
    filteredEmptyCopy={{
      title: t.memoryView.recentFilteredTitle,
      hint:  t.memoryView.recentFilteredHint,
      clearLabel: t.memoryView.clearFilters,
      onClear: resetFilters,
    }}
    errorCopy={{
      title:      t.memoryView.recentErrorTitle,
      detail:     t.memoryView.recentErrorDetail,
      retryLabel: t.memoryView.recentErrorRetry,
      onRetry:    () => refetch(),
    }}
  >
    {visible.map((m, i) => (
      <MemoryRow
        key={getRecentRenderKey(m.id)}
        memory={m}
        surface="recent"
        showDivider={i > 0}
        isNew={recentlyArrived.has(m.id)}
      />
    ))}
    {(hasMore || expanded) && (
      <footer className="border-t border-border/30 px-4 py-2.5 flex items-center justify-center gap-4">
        {expanded && (
          <button onClick={() => setVisibleCount(RECENT_PREVIEW_LIMIT)}>
            {t.memoryView.collapseRecent}
          </button>
        )}
        {canLoadMore && (
          <button
            disabled={isFetchingNextPage}
            onClick={async () => {
              const target = visibleCount + 5;
              if (target > memories.length && hasNextPage && !isFetchingNextPage) {
                await fetchNextPage();
              }
              setVisibleCount((n) => n + 5);
            }}
          >
            {isFetchingNextPage
              ? t.memoryView.loadingMore
              : interpolate(t.memoryView.loadMoreRecent, { n: String(nextIncrement) })}
          </button>
        )}
      </footer>
    )}
  </DataSectionCard>
);`}
    </pre>
  );
}

// ─── Section export ───

export function DataSectionCardSection() {
  return (
    <Section
      title="35. Data Section Card"
      description="Universal 5-phase state machine for any list-inside-a-card surface. Never unmounts mid-fetch. Composes existing primitives (Skeleton, ContentError) — does not reinvent them. Refetch is silent; content changes animate."
    >
      {/* 35a — the 5 phases at a glance */}
      <DemoCard label="35a. The 5 phases + 1 modifier">
        <p className="text-[12px] text-fg-2 mb-4">
          Every data-bound card in memax has exactly these 5 phases. Header
          chrome stays identical across all of them — only the body changes.{" "}
          <span className="font-mono">isPlaceholderData</span> is a modifier on{" "}
          <span className="font-mono">loaded</span>, not a separate phase. There
          is no <span className="font-mono">isFetching</span> prop — background
          refetch is silent (see 35c).
        </p>
        <FiveStatesGrid />
      </DemoCard>

      <DemoCard label="35a-i. Progressive Disclosure — explicit, reversible">
        <p className="text-[12px] text-fg-2">
          Recent-style cards do not auto-grow forever. Start with 5 rows, let
          users reveal 5 more at a time, and always allow the section to
          collapse back. This keeps the card bounded, preserves page hierarchy,
          and matches the product rule that data sections stay controllable
          instead of turning into infinite feeds. The footer only appears when
          there is more to reveal or when the section is already expanded.
        </p>
      </DemoCard>

      {/* 35b — interactive cycler */}
      <DemoCard label="35b. Interactive phase cycler">
        <p className="text-[12px] text-fg-2 mb-4">
          Click a phase to switch the card below. Notice the header stays
          identical across every phase — only the body morphs. No layout shift.
        </p>
        <PhaseCyclerDemo />
      </DemoCard>

      {/* 35c — silent refetch rule */}
      <DemoCard label="35c. Background refetch — silent by design">
        <p className="text-[12px] text-fg-2 mb-4">
          When React Query re-fetches a card in the background
          (stale-while-revalidate, post-mutation invalidation, focus refetch),
          memax shows <strong>no visual signal</strong>. The rows on screen are
          already accurate — a pulsing indicator would only interrupt the user.
          This is a deliberate rule, not an omission.
        </p>
        <SilentRefetchDemo />
      </DemoCard>

      {/* 35d — cache pivot */}
      <DemoCard label="35d. Cache pivot — isPlaceholderData, body dims for outgoing data">
        <p className="text-[12px] text-fg-2 mb-4">
          This IS a moment where the user should see something change: the user
          just switched hubs or filters, and the rows on screen are about to be
          replaced. React Query&apos;s{" "}
          <span className="font-mono">placeholderData: (prev) =&gt; prev</span>{" "}
          keeps the previous data visible while the new query resolves, and the
          card dims its body to 0.72 to signal &ldquo;outgoing.&rdquo; Never
          drops to a skeleton. The dim is the ONLY visual cue — no pulse, no
          star, no spinner.
        </p>
        <CachePivotDemo />
      </DemoCard>

      {/* 35e — memory arrival animation */}
      <DemoCard label="35e. Memory arrival — row entrance animation (SSE / optimistic)">
        <p className="text-[12px] text-fg-2 mb-4">
          The other legitimate motion moment. When an agent pushes a new memory
          (SSE event) or the user just captured something, the new row lands
          with a soft upward settle and a fleeting signature trace on its left
          edge. Card chrome does nothing — the motion lives entirely on the new
          row. This is memax&apos;s &ldquo;memory appearing&rdquo; signature:
          quiet, expensive-feeling, and still legible during silent refetch.
        </p>
        <MemoryArrivalDemo />
      </DemoCard>

      {/* 35f — anti-patterns */}
      <DemoCard label="35f. Anti-patterns vs correct">
        <p className="text-[12px] text-fg-2 mb-4">
          Left side: patterns we currently ship in production. Right side: the{" "}
          <span className="font-mono">DataSectionCard</span> equivalent. Scan
          production for these anti-patterns — they&apos;re the biggest source
          of UI state bugs in the audit.
        </p>
        <AntiPatternsDemo />
      </DemoCard>

      {/* 35g — API contract */}
      <DemoCard label="35g. API contract">
        <ContractTable />
        <p className="text-[11px] text-fg-3 mt-4">
          The caller is responsible for deriving{" "}
          <span className="font-mono">phase</span> from React Query flags (see
          35h). The component is dumb — it just renders what it&apos;s told.
          Row-level states (updating / deleting / processing) live in the row
          component, NOT in the card phase — a card can hold rows in mixed
          mutation states simultaneously.
        </p>
      </DemoCard>

      {/* 35h — caller template */}
      <DemoCard label="35h. Caller template — Recent section migration">
        <p className="text-[12px] text-fg-2 mb-3">
          Drop this shape into{" "}
          <span className="font-mono">topic-grid.tsx RecentSection</span>.
          Delete the existing{" "}
          <span className="font-mono">if (!recentPages) return null</span>{" "}
          (L548) and the hand-rolled card wrapper. All state branches flow
          through the primitive.
        </p>
        <CallerTemplate />
      </DemoCard>

      {/* 35i — production migration targets */}
      <DemoCard label="35i. Production migration targets">
        <div className="text-[12px] text-fg-2 space-y-2">
          <p className="font-medium text-fg-1">
            Files that should migrate to{" "}
            <span className="font-mono">DataSectionCard</span>:
          </p>
          <ul className="list-disc pl-5 space-y-1 text-[12px] text-fg-2">
            <li>
              <span className="font-mono">
                packages/web/src/components/features/topic-grid.tsx
              </span>{" "}
              — RecentSection (L369+), InboxSection (L810+)
            </li>
            <li>
              <span className="font-mono">
                packages/web/src/components/features/topic-detail.tsx
              </span>{" "}
              — subtopic groups, ungrouped section
            </li>
            <li>
              <span className="font-mono">
                packages/web/src/components/features/agent-configs-section.tsx
              </span>{" "}
              — agents list, API keys list
            </li>
            <li>
              <span className="font-mono">
                packages/web/src/components/features/hub-management-view.tsx
              </span>{" "}
              — members, invites
            </li>
            <li>
              <span className="font-mono">
                packages/web/src/components/features/related-memories.tsx
              </span>
              , <span className="font-mono">topic-siblings.tsx</span>,{" "}
              <span className="font-mono">topic-pills.tsx</span> — delete their{" "}
              <span className="font-mono">return null</span> branches
            </li>
            <li>
              <span className="font-mono">
                packages/web/src/components/features/dream-dialog.tsx
              </span>{" "}
              — review list body
            </li>
          </ul>
          <p className="mt-3 text-fg-3">
            Non-migrations (intentional): the bar (different shell), full-page
            routes (use <span className="font-mono">&lt;MemaxLoader&gt;</span>{" "}
            at route level), and the memory detail page (single-record surface,
            not a list).
          </p>
          <p className="mt-3 text-fg-3">
            Also required in globals.css (memory arrival animation):
          </p>
          <pre className="text-[10px] text-fg-2 font-mono leading-[1.55] bg-surface-1 rounded-lg p-3 overflow-x-auto">
            {`@keyframes memaxMemoryArrive {
  0%   { opacity: 0; transform: translateY(-4px);
         box-shadow: inset 1.5px 0 0 oklch(from var(--signature) l c h / 0.12),
                     0 10px 24px color-mix(in oklch, var(--foreground) 6%, transparent); }
  18%  { opacity: 1; transform: translateY(0);
         box-shadow: inset 1.5px 0 0 oklch(from var(--signature) l c h / 0.1),
                     0 6px 16px color-mix(in oklch, var(--foreground) 4%, transparent); }
  60%  { box-shadow: inset 1px 0 0 oklch(from var(--signature) l c h / 0.05),
                     0 2px 8px color-mix(in oklch, var(--foreground) 2%, transparent); }
  100% { opacity: 1; transform: translateY(0);
         box-shadow: inset 0 0 0 transparent, 0 0 0 transparent; }
}
.state-memory-arrive {
  animation: memaxMemoryArrive 820ms var(--ease-spring) both;
}`}
          </pre>
        </div>
      </DemoCard>
    </Section>
  );
}
