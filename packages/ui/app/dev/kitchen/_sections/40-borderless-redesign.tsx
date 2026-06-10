"use client";

// ═══════════════════════════════════════════════
// 40. Borderless Redesign — Active Proposal
//
// Visual before → after for the redesign spec:
//   1. Content-list shells go borderless (card chrome → dividers-only)
//   2. Mobile topic mode C full-bleed
//   3. Memax lifecycle signals (unseen halo + dream delta chip)
//   4. Skeletons mirror row geometry
//
// Row internals (padding, typography, gestures) are NOT changed.
// This section is a proposal visualization, not shipped behavior.
// ═══════════════════════════════════════════════

import {
  Clock,
  Hash,
  Folder,
  Pin,
  ChevronRight,
  Copy,
  SlidersHorizontal,
} from "lucide-react";
import { Section, SIGNATURE } from "../_shared";

// ─── Shared row primitives for this section ──────────────────────────────

function AvatarCircle({ hue, letter }: { hue: number; letter: string }) {
  return (
    <div
      className="h-5 w-5 rounded-full flex items-center justify-center text-[10px] font-medium text-white shrink-0"
      style={{ background: `oklch(0.7 0.12 ${hue})` }}
    >
      {letter}
    </div>
  );
}

function AgentIconShell({ letter }: { letter: string }) {
  return (
    <div
      className="h-5 w-5 rounded-md flex items-center justify-center text-[11px] text-fg-2 shrink-0"
      style={{ background: "oklch(from var(--foreground) l c h / 0.08)" }}
    >
      {letter}
    </div>
  );
}

function Halo({
  breathing,
  children,
}: {
  breathing?: boolean;
  children: React.ReactNode;
}) {
  return (
    <span className="relative inline-flex items-center justify-center h-5 w-5 shrink-0">
      <span
        aria-hidden
        className={`absolute pointer-events-none rounded-full ${
          breathing ? "state-slow-breathe" : ""
        }`}
        style={{
          inset: -8,
          background:
            "radial-gradient(circle, oklch(from var(--signature) l c h / 0.4) 0%, transparent 70%)",
        }}
      />
      <span className="relative z-10 inline-flex">{children}</span>
    </span>
  );
}

function ProcessingStar() {
  return (
    <span
      className="inline-flex h-5 w-5 items-center justify-center text-[13px] state-slow-breathe shrink-0"
      style={{ color: SIGNATURE }}
    >
      ✦
    </span>
  );
}

function DeltaChip({ label }: { label: string }) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium shrink-0"
      style={{
        background: "oklch(from var(--signature) l c h / 0.1)",
        color: "var(--signature)",
      }}
    >
      <span className="text-[10px] leading-none">✦</span>
      {label}
    </span>
  );
}

function TopicChip({
  label,
  tone,
}: {
  label: string;
  tone?: "neutral" | "dream";
}) {
  const isDream = tone === "dream";
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] shrink-0"
      style={
        isDream
          ? {
              background: "oklch(from var(--signature) l c h / 0.1)",
              color: "var(--signature)",
              fontWeight: 500,
            }
          : undefined
      }
    >
      {isDream ? (
        <span className="text-[10px] leading-none">✦</span>
      ) : (
        <Folder className="h-2.5 w-2.5 shrink-0 text-fg-4" />
      )}
      <span className={isDream ? "" : "text-fg-3"}>{label}</span>
    </span>
  );
}

function AgeMeta({ age }: { age: string }) {
  return (
    <span className="text-[13px] text-fg-3 tabular-nums shrink-0">{age}</span>
  );
}

// ─── Section header (identical grammar across surfaces) ──────────────────

function SectionHeader({
  icon: Icon,
  label,
  trailing,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  trailing?: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-2 px-1 mb-2">
      <Icon className="h-3.5 w-3.5 shrink-0 text-fg-3" />
      <span className="text-[14px] font-semibold text-fg-2">{label}</span>
      <div className="flex-1" />
      {trailing}
    </div>
  );
}

// ─── Memory rows (stacked "recent" layout) ───────────────────────────────

function StackedRow({
  indicator,
  attribution,
  title,
  summary,
  age,
  trailing,
  topTrailing,
}: {
  indicator: React.ReactNode;
  attribution: string;
  title: string;
  summary?: string;
  age: string;
  trailing?: React.ReactNode;
  topTrailing?: React.ReactNode;
}) {
  return (
    <div className="px-4 py-2.5 hover:bg-surface-1 transition-colors cursor-pointer">
      <div className="mb-0.5 flex items-center gap-1.5 text-[12px]">
        {indicator}
        <span className="text-foreground/40">{attribution}</span>
        <span className="ml-auto flex items-center gap-1.5 text-foreground/40">
          {topTrailing}
          <span className="tabular-nums">{age}</span>
        </span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="text-[14px] text-fg-1 truncate flex-1 min-w-0">
          {title}
        </span>
        {trailing}
      </div>
      {summary && (
        <p className="text-[13px] text-fg-3 truncate mt-0.5">{summary}</p>
      )}
    </div>
  );
}

// ─── Single-line row (topic/list/recall layout) ──────────────────────────

function SingleRow({
  indicator,
  title,
  trailing,
}: {
  indicator: React.ReactNode;
  title: string;
  trailing: React.ReactNode;
}) {
  return (
    <div className="px-4 py-2.5 hover:bg-surface-1 transition-colors cursor-pointer flex items-center gap-2">
      {indicator}
      <span className="text-[14px] text-fg-1 truncate flex-1 min-w-0">
        {title}
      </span>
      <div className="flex items-center gap-1.5 ml-auto shrink-0">
        {trailing}
      </div>
    </div>
  );
}

// ─── Divider ─────────────────────────────────────────────────────────────

function RowDivider() {
  return (
    <div
      style={{
        borderTop: "1px solid oklch(from var(--foreground) l c h / 0.08)",
      }}
    />
  );
}

// ═════════════════════════════════════════════════════════════════════════
// 1. Recent feed — carded (BEFORE) vs borderless (AFTER)
// ═════════════════════════════════════════════════════════════════════════

function RecentFeedCompare() {
  const trailing = (
    <div className="flex items-center gap-2 text-[12px] text-fg-3">
      <SlidersHorizontal className="h-3 w-3" />
      <span>过去 7 天</span>
      <ChevronRight className="h-2.5 w-2.5 rotate-90" />
      <Copy className="h-3 w-3 ml-1" />
      <span>5</span>
    </div>
  );

  const rows = [
    {
      indicator: <AvatarCircle hue={60} letter="J" />,
      attribution: "Jiahao pushed",
      title: "Retrieval eval runs on 59 queries per commit",
      summary: "Server-side eval harness stays green on the recall threshold…",
      age: "5m",
    },
    {
      indicator: <AgentIconShell letter="C" />,
      attribution: "Claude Code captured",
      title: "Hub email invites plan v4 approved",
      summary:
        "Memax email invites + admin notifications, auth deferred to Ziyang…",
      age: "2h",
    },
    {
      indicator: <AvatarCircle hue={200} letter="Z" />,
      attribution: "Ziyang pushed",
      title: "Fly.io cache warmer needs GCS mount",
      summary: "Worker cache eviction on redeploy caused 800ms cold starts…",
      age: "1d",
    },
  ];

  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
      {/* BEFORE — carded */}
      <div>
        <BeforeAfterLabel kind="before" text="carded" />
        <div
          className="rounded-xl overflow-hidden"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
          }}
        >
          <div
            className="px-4 py-2.5"
            style={{ borderBottom: "1px solid var(--border)" }}
          >
            <div className="flex items-center gap-2">
              <Clock className="h-3.5 w-3.5 shrink-0 text-fg-3" />
              <span className="text-[14px] font-semibold text-fg-2">
                新鲜记忆
              </span>
              <div className="flex-1" />
              {trailing}
            </div>
          </div>
          {rows.map((r, i) => (
            <div key={i}>
              {i > 0 && (
                <div
                  style={{
                    borderTop:
                      "1px solid oklch(from var(--border) l c h / 0.6)",
                  }}
                />
              )}
              <StackedRow
                indicator={r.indicator}
                attribution={r.attribution}
                title={r.title}
                summary={r.summary}
                age={r.age}
              />
            </div>
          ))}
        </div>
      </div>

      {/* AFTER — borderless */}
      <div>
        <BeforeAfterLabel kind="after" text="borderless" />
        <div style={{ background: "var(--background)" }}>
          <SectionHeader icon={Clock} label="新鲜记忆" trailing={trailing} />
          <div>
            {rows.map((r, i) => (
              <div key={i}>
                {i > 0 && <RowDivider />}
                <StackedRow
                  indicator={r.indicator}
                  attribution={r.attribution}
                  title={r.title}
                  summary={r.summary}
                  age={r.age}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// 2. Mobile topic list — mode C
// ═════════════════════════════════════════════════════════════════════════

function MobilePhone({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="rounded-[28px] p-3 shrink-0"
      style={{
        width: 360,
        background: "oklch(0.35 0.01 270)",
        boxShadow: "0 20px 60px -20px rgba(0,0,0,0.25)",
      }}
    >
      <div
        className="rounded-surface overflow-hidden"
        style={{ background: "var(--background)", minHeight: 440 }}
      >
        <div className="pt-4 pb-2">{children}</div>
      </div>
    </div>
  );
}

function TopicRowBefore({
  icon,
  name,
  age,
  pinned,
}: {
  icon: string;
  name: string;
  age: string;
  pinned?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-3 py-3 hover:bg-surface-1 transition-colors cursor-pointer">
      <Folder className="h-4 w-4 shrink-0 text-fg-3" />
      <span className="flex-1 truncate text-[14px] text-fg-1">{name}</span>
      <span className="text-[12px] text-fg-3 tabular-nums shrink-0">{age}</span>
      <Pin
        className="h-3.5 w-3.5 shrink-0"
        style={{
          color: pinned ? "var(--fg-2)" : "var(--fg-4)",
          fill: pinned ? "currentColor" : "none",
          transform: pinned ? "rotate(-45deg)" : "rotate(0deg)",
        }}
      />
    </div>
  );
}

function TopicRowAfter({
  name,
  age,
  pinned,
}: {
  name: string;
  age: string;
  pinned?: boolean;
}) {
  return (
    <div className="flex items-center gap-3 px-5 py-3 hover:bg-surface-1 transition-colors cursor-pointer">
      {pinned && (
        <Pin
          className="h-3.5 w-3.5 shrink-0 text-fg-2"
          style={{
            fill: "currentColor",
            transform: "rotate(-45deg)",
          }}
        />
      )}
      <Folder
        className={`h-4 w-4 shrink-0 text-fg-3 ${!pinned ? "ml-[calc(0.875rem+0.75rem)]" : ""}`}
        style={!pinned ? { marginLeft: 0 } : undefined}
      />
      <span className="flex-1 truncate text-[14px] text-fg-1">{name}</span>
      <span className="text-[12px] text-fg-3 tabular-nums shrink-0">{age}</span>
    </div>
  );
}

function MobileTopicsCompare() {
  const topics = [
    { name: "Retrieval eval", age: "3d", pinned: true },
    { name: "Hub architecture", age: "1d", pinned: false },
    { name: "Eval infra and tooling", age: "5d", pinned: false },
    { name: "Pricing and packaging", age: "2w", pinned: false },
    { name: "Dream engine", age: "6h", pinned: false },
  ];

  return (
    <div className="flex flex-wrap gap-6 justify-center">
      <div>
        <BeforeAfterLabel kind="before" text="carded · 375px" />
        <MobilePhone>
          <div className="px-4">
            <div
              className="rounded-xl overflow-hidden"
              style={{
                background: "var(--card)",
                border: "1px solid var(--border)",
              }}
            >
              <div
                className="flex items-center gap-2 px-3 py-2.5"
                style={{ borderBottom: "1px solid var(--border)" }}
              >
                <Hash className="h-3.5 w-3.5 shrink-0 text-fg-3" />
                <span className="text-[14px] font-semibold text-fg-2">
                  主题
                </span>
                <div className="flex-1" />
                <span className="text-[12px] text-fg-3">⊞ ⊟ ≡</span>
              </div>
              {topics.map((t, i) => (
                <div key={t.name}>
                  {i > 0 && (
                    <div
                      style={{
                        borderTop:
                          "1px solid oklch(from var(--border) l c h / 0.6)",
                      }}
                    />
                  )}
                  <TopicRowBefore
                    icon="folder"
                    name={t.name}
                    age={t.age}
                    pinned={t.pinned}
                  />
                </div>
              ))}
            </div>
          </div>
        </MobilePhone>
      </div>

      <div>
        <BeforeAfterLabel kind="after" text="borderless · 375px" />
        <MobilePhone>
          <SectionHeader
            icon={Hash}
            label="主题"
            trailing={<span className="text-[12px] text-fg-3">⊞ ⊟ ≡</span>}
          />
          <div>
            {topics.map((t, i) => (
              <div key={t.name}>
                {i > 0 && <RowDivider />}
                <TopicRowAfter name={t.name} age={t.age} pinned={t.pinned} />
              </div>
            ))}
          </div>
        </MobilePhone>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// 3. Lifecycle signals on a memory row
// ═════════════════════════════════════════════════════════════════════════

function LifecycleShowcase() {
  return (
    <div style={{ background: "var(--background)" }}>
      <SectionHeader icon={Clock} label="新鲜记忆" />

      {/* Normal */}
      <SingleRow
        indicator={<AvatarCircle hue={60} letter="J" />}
        title="Normal row — seen, settled, no delta"
        trailing={
          <>
            <TopicChip label="Retrieval eval" />
            <AgeMeta age="3d" />
          </>
        }
      />
      <RowDivider />

      {/* Fresh breathing — first 5 min */}
      <SingleRow
        indicator={
          <Halo breathing>
            <AvatarCircle hue={200} letter="Z" />
          </Halo>
        }
        title="Fresh · age < 5 min (halo breathes)"
        trailing={
          <>
            <TopicChip label="Hub architecture" />
            <AgeMeta age="2m" />
          </>
        }
      />
      <RowDivider />

      {/* Fresh static — up to 24h */}
      <SingleRow
        indicator={
          <Halo>
            <AvatarCircle hue={150} letter="R" />
          </Halo>
        }
        title="Fresh · age < 24h (halo static, pure time decay)"
        trailing={
          <>
            <TopicChip label="Pricing" />
            <AgeMeta age="4h" />
          </>
        }
      />
      <RowDivider />

      {/* Processing */}
      <SingleRow
        indicator={<ProcessingStar />}
        title="Processing · memax is still remembering (existing)"
        trailing={
          <span
            className="text-[13px] state-slow-breathe shrink-0"
            style={{ color: SIGNATURE }}
          >
            Remembering…
          </span>
        }
      />
      <RowDivider />

      {/* Dream delta — rides on the breadcrumb chip */}
      <SingleRow
        indicator={<AgentIconShell letter="C" />}
        title="Dream-touched · signal lives on the topic breadcrumb"
        trailing={
          <>
            <TopicChip tone="dream" label="Eval infra" />
            <AgeMeta age="6h" />
          </>
        }
      />
      <RowDivider />

      {/* Halo + dream-tinted breadcrumb */}
      <SingleRow
        indicator={
          <Halo>
            <AvatarCircle hue={20} letter="D" />
          </Halo>
        }
        title="Fresh + dream-touched — halo on indicator, tint on breadcrumb"
        trailing={
          <>
            <TopicChip tone="dream" label="Retrieval eval" />
            <AgeMeta age="1h" />
          </>
        }
      />
      <RowDivider />

      {/* Old rowAccent — killed */}
      <div
        className="px-4 py-2.5 flex items-center gap-2 hover:bg-surface-1 transition-colors cursor-pointer"
        style={{
          boxShadow: "inset 2px 0 0 oklch(from var(--signature) l c h / 0.55)",
          opacity: 0.55,
        }}
      >
        <AvatarCircle hue={60} letter="J" />
        <span className="text-[14px] text-fg-1 truncate flex-1 min-w-0">
          (Killed) old <code className="text-[12px]">rowAccent</code>{" "}
          left-border — never again
        </span>
        <span className="text-[13px] text-fg-4 tabular-nums shrink-0">—</span>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// 4. Topic cards with dream delta
// ═════════════════════════════════════════════════════════════════════════

function TopicCardBox({
  name,
  description,
  pinned,
  age,
  delta,
}: {
  name: string;
  description: string;
  pinned?: boolean;
  age: string;
  delta?: string;
}) {
  return (
    <div
      className="rounded-xl p-4 hover:bg-surface-1 transition-colors cursor-pointer"
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
      }}
    >
      <div className="flex items-start gap-3 mb-2">
        <Folder className="h-4 w-4 shrink-0 text-fg-3 mt-0.5" />
        <span className="flex-1 min-w-0 truncate text-[15px] font-medium text-fg-1">
          {name}
        </span>
        <Pin
          className="h-3.5 w-3.5 shrink-0"
          style={{
            color: pinned ? "var(--fg-2)" : "var(--fg-4)",
            fill: pinned ? "currentColor" : "none",
            transform: pinned ? "rotate(-45deg)" : "rotate(0deg)",
          }}
        />
      </div>
      <p className="text-[14px] text-fg-2 mb-2.5 line-clamp-2">{description}</p>
      <div className="flex items-center gap-2">
        {delta && <DeltaChip label={delta} />}
        <span className="flex-1" />
        <span className="text-[12px] text-fg-3 tabular-nums">{age}</span>
      </div>
    </div>
  );
}

function TopicCardsShowcase() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
      <div>
        <BeforeAfterLabel kind="before" text="no delta signal" />
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <TopicCardBox
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            pinned
            age="3d"
          />
          <TopicCardBox
            name="Hub architecture"
            description="Team hubs, access model, push routing."
            age="1d"
          />
        </div>
      </div>
      <div>
        <BeforeAfterLabel kind="after" text="delta chip when active" />
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <TopicCardBox
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            pinned
            age="3d"
            delta="+3 · 2 reorganized"
          />
          <TopicCardBox
            name="Hub architecture"
            description="Team hubs, access model, push routing."
            age="1d"
          />
        </div>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// 5. Loading skeletons — card-shaped (BEFORE) vs row-shaped (AFTER)
// ═════════════════════════════════════════════════════════════════════════

function SkeletonBar({
  width,
  height = 12,
  circle,
}: {
  width: number | string;
  height?: number;
  circle?: boolean;
}) {
  return (
    <div
      style={{
        width,
        height,
        borderRadius: circle ? 999 : 4,
        background: "oklch(from var(--foreground) l c h / 0.07)",
      }}
    />
  );
}

function SkeletonCompare() {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
      <div>
        <BeforeAfterLabel kind="before" text="card-shaped stripes" />
        <div
          className="rounded-xl overflow-hidden"
          style={{
            background: "var(--card)",
            border: "1px solid var(--border)",
          }}
        >
          <div
            className="px-4 py-2.5 flex items-center gap-2"
            style={{ borderBottom: "1px solid var(--border)" }}
          >
            <Clock className="h-3.5 w-3.5 shrink-0 text-fg-3" />
            <span className="text-[14px] font-semibold text-fg-2">
              新鲜记忆
            </span>
          </div>
          <div className="p-4 space-y-3">
            <SkeletonBar width={96} height={12} />
            <SkeletonBar width="100%" height={16} />
            <SkeletonBar width="75%" height={16} />
            <SkeletonBar width="83%" height={16} />
          </div>
        </div>
      </div>

      <div>
        <BeforeAfterLabel kind="after" text="row-shaped · matches loaded" />
        <div style={{ background: "var(--background)" }}>
          <SectionHeader icon={Clock} label="新鲜记忆" />
          <div>
            {[
              { titleW: 240, summaryW: 180, attrW: 120 },
              { titleW: 210, summaryW: 140, attrW: 140 },
              { titleW: 260, summaryW: 160, attrW: 110 },
            ].map((r, i) => (
              <div key={i}>
                {i > 0 && <RowDivider />}
                <div className="px-4 py-2.5 flex items-start gap-2.5">
                  <div className="pt-0.5">
                    <SkeletonBar width={20} height={20} circle />
                  </div>
                  <div className="flex-1 min-w-0 space-y-1.5">
                    <SkeletonBar width={r.attrW} height={10} />
                    <SkeletonBar width={r.titleW} height={14} />
                    <SkeletonBar width={r.summaryW} height={12} />
                  </div>
                  <div className="pt-0.5">
                    <SkeletonBar width={28} height={10} />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Shared label
// ═════════════════════════════════════════════════════════════════════════

function BeforeAfterLabel({
  kind,
  text,
}: {
  kind: "before" | "after";
  text: string;
}) {
  return (
    <div className="flex items-center gap-2 mb-3">
      <span
        className="inline-block h-1.5 w-1.5 rounded-sm shrink-0"
        style={{
          background:
            kind === "before" ? "oklch(0.7 0.15 30)" : "var(--signature)",
        }}
      />
      <span className="text-[10px] font-semibold uppercase tracking-wider text-fg-3">
        {kind === "before" ? "Before" : "After"}
      </span>
      <span className="text-[12px] text-fg-3">· {text}</span>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Demo wrapper — quiet outline so compare blocks are discoverable in kitchen
// ═════════════════════════════════════════════════════════════════════════

function DemoBlock({
  title,
  hint,
  phase,
  children,
}: {
  title: string;
  hint?: string;
  phase?: 1 | 2;
  children: React.ReactNode;
}) {
  return (
    <div className="mb-8">
      <div className="flex items-center gap-2 mb-1">
        <h3 className="text-[15px] font-semibold text-fg-1">{title}</h3>
        {phase && (
          <span
            className="inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider shrink-0"
            style={
              phase === 1
                ? {
                    background: "oklch(from var(--signature) l c h / 0.12)",
                    color: "var(--signature)",
                  }
                : {
                    background: "oklch(from var(--foreground) l c h / 0.06)",
                    color: "var(--fg-3)",
                  }
            }
          >
            {phase === 1 ? "Phase 1 · ships first" : "Phase 2 · preview"}
          </span>
        )}
      </div>
      {hint && <p className="text-[13px] text-fg-3 mb-4">{hint}</p>}
      <div
        className="rounded-xl p-5"
        style={{
          background: "var(--background)",
          border: "1px dashed var(--border)",
        }}
      >
        {children}
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Top-level section
// ═════════════════════════════════════════════════════════════════════════

export function BorderlessRedesignSection() {
  return (
    <Section
      title="40. Borderless Redesign (proposal)"
      description="Two phases. Phase 1 = visual frame only (borderless shells, unified headers, loading consistency) — ships first, no backend work. Phase 2 = lifecycle signals (age-based halo, dream-delta on breadcrumb) — pending product-semantics + data-flow read before design locks. Row internals are unchanged in both phases."
    >
      <DemoBlock
        title="1. Recent feed · desktop"
        phase={1}
        hint="Outer card disappears; header grammar and row internals stay identical."
      >
        <RecentFeedCompare />
      </DemoBlock>

      <DemoBlock
        title="2. Mobile topic list · mode C"
        phase={1}
        hint="~32px horizontal reclaimed. Pin moves to leading position for pinned rows only; unpinned look as today."
      >
        <MobileTopicsCompare />
      </DemoBlock>

      <DemoBlock
        title="5. Loading skeletons"
        phase={1}
        hint="Skeleton geometry mirrors final row geometry within ±2px. No card-shaped skeleton on borderless surfaces. Single animate-content-ready fade; no per-row pop-in."
      >
        <SkeletonCompare />
      </DemoBlock>

      <DemoBlock
        title="3. Lifecycle signals on a memory row (preview)"
        phase={2}
        hint="Halo = pure freshness (age-based, < 5min breathes, < 24h static, else off). Dream-touched signal rides on the existing topic breadcrumb chip: signature tint + leading ✦ replaces a separate chip. Processing star is the existing memax state."
      >
        <LifecycleShowcase />
      </DemoBlock>

      <DemoBlock
        title="4. Topic cards with dream delta (preview)"
        phase={2}
        hint="Chip aggregates adds + reorganizations since viewer's last topic visit. Clears when viewer opens the topic. Server-owned; requires topic_visits infrastructure (see Phase 2 brief)."
      >
        <TopicCardsShowcase />
      </DemoBlock>

      <div
        className="rounded-xl p-4 mt-6"
        style={{ background: "var(--surface-1)" }}
      >
        <p className="text-[13px] text-fg-2 leading-relaxed mb-3">
          <span className="font-semibold text-fg-1">
            What stays unchanged across both phases:
          </span>{" "}
          row padding, title / summary typography, three-tier attribution,
          multi-select, desktop drag grip, long-press on mobile, processing
          star, search highlight, hub pills, batch toolbar, memory detail page,
          desktop topic grid modes A/B.
        </p>
        <p className="text-[13px] text-fg-2 leading-relaxed">
          <span className="font-semibold text-fg-1">Phase 1 → Phase 2:</span>{" "}
          Phase 1 is purely presentational; it leaves{" "}
          <code className="text-[12px]">memory-row-presentation.ts</code> as the
          integration point so Phase 2 can resolve halo + breadcrumb tone
          without touching row internals. No lifecycle state leaks into Phase 1.
        </p>
      </div>
    </Section>
  );
}
