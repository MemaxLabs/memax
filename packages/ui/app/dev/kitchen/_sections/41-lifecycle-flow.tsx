"use client";

// ═══════════════════════════════════════════════
// 41. Lifecycle + Dream Delta — E2E User Flow
//
// Visual walkthrough of what a user sees as memax captures, organizes,
// and shows their memories over time. Two distinct signal families —
// notifications (attention/review) and lifecycle (passive browse) —
// never overlap.
//
// Maps to shipped behavior in commit 11f83406. This section is the
// design-review record; the actual signals are wired in MemoryRow /
// ProvenanceStrip / topic-card via memory-row-presentation.ts.
// ═══════════════════════════════════════════════

import {
  AlertTriangle,
  ArrowRight,
  Clock,
  Folder,
  Globe,
  GitMerge,
  Pin,
  Sparkles,
} from "lucide-react";
import {
  TopicChip,
  DreamActionPopoverBody,
  type DreamActionDisplayRef,
} from "@memaxlabs/ui";
import { Section, SIGNATURE } from "../_shared";

// ─── Shared row primitives ───────────────────────────────────────────────

function AvatarCircle({ hue, letter }: { hue: number; letter: string }) {
  return (
    <div
      className="relative z-10 h-5 w-5 rounded-full flex items-center justify-center text-[10px] font-medium text-white shrink-0"
      style={{ background: `oklch(0.7 0.12 ${hue})` }}
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
      {children}
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
        lineHeight: "1.25",
      }}
    >
      <span className="text-[10px] leading-none">✦</span>
      {label}
    </span>
  );
}

function RowDivider() {
  return (
    <div
      style={{
        borderTop: "1px solid oklch(from var(--foreground) l c h / 0.08)",
      }}
    />
  );
}

function AgeMeta({ age }: { age: string }) {
  return (
    <span className="text-[13px] text-fg-3 tabular-nums shrink-0">{age}</span>
  );
}

function Row({
  indicator,
  title,
  trailing,
  highlight,
}: {
  indicator: React.ReactNode;
  title: string;
  trailing: React.ReactNode;
  highlight?: boolean;
}) {
  return (
    <div
      className={`px-4 py-2.5 flex items-center gap-2 hover:bg-surface-1 transition-colors cursor-pointer ${
        highlight ? "relative" : ""
      }`}
    >
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

function SectionHeader({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 px-1 mb-2">
      <Clock className="h-3.5 w-3.5 shrink-0 text-fg-3" />
      <span className="text-[14px] font-semibold text-fg-2">{label}</span>
    </div>
  );
}

// ─── Topic card ──────────────────────────────────────────────────────────

function TopicCard({
  name,
  description,
  age,
  pinned,
  delta,
  highlight,
}: {
  name: string;
  description: string;
  age: string;
  pinned?: boolean;
  delta?: string;
  highlight?: boolean;
}) {
  return (
    <div
      className={`rounded-xl p-4 hover:bg-surface-1 transition-colors cursor-pointer ${
        highlight ? "ring-2 ring-offset-2" : ""
      }`}
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
        ...(highlight
          ? ({
              "--tw-ring-color": "oklch(from var(--signature) l c h / 0.5)",
              "--tw-ring-offset-color": "var(--background)",
            } as React.CSSProperties)
          : {}),
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

// ─── Frame wrapper ───────────────────────────────────────────────────────

function Frame({
  tag,
  tagTone = "neutral",
  children,
}: {
  tag: string;
  tagTone?: "neutral" | "fresh" | "before" | "after";
  children: React.ReactNode;
}) {
  const tagColor =
    tagTone === "fresh" || tagTone === "after"
      ? "var(--signature)"
      : tagTone === "before"
        ? "oklch(0.65 0.15 30)"
        : "var(--fg-3)";
  return (
    <div
      className="relative rounded-2xl p-5"
      style={{
        background: "var(--background)",
        border: "1px dashed var(--border)",
      }}
    >
      <span
        className="absolute -top-2.5 left-3.5 px-2 text-[10px] font-semibold uppercase tracking-wider"
        style={{ background: "var(--background)", color: tagColor }}
      >
        {tag}
      </span>
      {children}
    </div>
  );
}

function Annot({ children }: { children: React.ReactNode }) {
  return (
    <div className="mt-3 p-3 rounded-md bg-surface-1 text-[12px] text-fg-3 leading-relaxed">
      {children}
    </div>
  );
}

// ─── Act headers ─────────────────────────────────────────────────────────

function ActHead({
  index,
  title,
  sub,
}: {
  index: number;
  title: string;
  sub: string;
}) {
  return (
    <div className="mb-5">
      <div className="flex items-baseline gap-3 mb-1.5">
        <span
          className="text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded"
          style={{
            background: "oklch(from var(--signature) l c h / 0.1)",
            color: "var(--signature)",
          }}
        >
          Act {index}
        </span>
        <h3 className="text-[18px] font-bold text-fg-1 tracking-tight m-0">
          {title}
        </h3>
      </div>
      <p className="text-[13px] text-fg-3 max-w-2xl leading-relaxed">{sub}</p>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 1 — Push → Halo
// ═════════════════════════════════════════════════════════════════════════

function Act1() {
  return (
    <div className="mb-10">
      <ActHead
        index={1}
        title="A memory arrives — the halo lifecycle"
        sub="User pushes or an agent captures a memory. A soft signature halo blooms behind the indicator so the row stands out while it's still fresh. Pure time decay from created_at — no server state, no mutations."
      />
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <Frame tag="t = 30 seconds" tagTone="fresh">
          <SectionHeader label="新鲜记忆" />
          <Row
            indicator={
              <Halo breathing>
                <AvatarCircle hue={60} letter="J" />
              </Halo>
            }
            title="Retrieval eval runs on 59 queries"
            trailing={<AgeMeta age="30s" />}
          />
          <Annot>
            <strong className="text-fg-2">Halo state:</strong> breathing. Pulses
            at 2.4s via <code className="text-[11px]">state-slow-breathe</code>.
          </Annot>
        </Frame>

        <Frame tag="t = 4 hours">
          <SectionHeader label="新鲜记忆" />
          <Row
            indicator={
              <Halo>
                <AvatarCircle hue={60} letter="J" />
              </Halo>
            }
            title="Retrieval eval runs on 59 queries"
            trailing={<AgeMeta age="4h" />}
          />
          <Annot>
            <strong className="text-fg-2">Halo state:</strong> static. Still
            glanceable as new, no longer drawing attention.
          </Annot>
        </Frame>

        <Frame tag="t = 2 days">
          <SectionHeader label="新鲜记忆" />
          <Row
            indicator={<AvatarCircle hue={60} letter="J" />}
            title="Retrieval eval runs on 59 queries"
            trailing={<AgeMeta age="2d" />}
          />
          <Annot>
            <strong className="text-fg-2">Halo state:</strong> off. Reads as any
            other row. No lingering shimmer after 24h.
          </Annot>
        </Frame>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 2 — Dream runs → delta appears
// ═════════════════════════════════════════════════════════════════════════

function Act2() {
  return (
    <div className="mb-10">
      <ActHead
        index={2}
        title="Dreams run overnight — delta appears"
        sub="Between visits, the dream engine organizes memories. When the user returns: topic card gains an aggregate chip; moved memory's breadcrumb tints signature color. Both resolved server-side, scoped to last topic visit."
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Frame tag="Before — yesterday" tagTone="before">
          <TopicCard
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            age="3d"
            pinned
          />
          <div className="mt-4">
            <SectionHeader label="新鲜记忆" />
            <Row
              indicator={<AvatarCircle hue={60} letter="J" />}
              title="Eval harness output format"
              trailing={
                <>
                  <TopicChip label="Eval infra" icon="server" />
                  <AgeMeta age="5d" />
                </>
              }
            />
          </div>
        </Frame>

        <Frame tag="After — dream run overnight" tagTone="after">
          <TopicCard
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            age="3d"
            pinned
            delta="+3 · 2 reorganized"
          />
          <div className="mt-4">
            <SectionHeader label="新鲜记忆" />
            <Row
              indicator={<AvatarCircle hue={60} letter="J" />}
              title="Eval harness output format"
              trailing={
                <>
                  <TopicChip tone="dream" label="Retrieval eval" icon="code" />
                  <AgeMeta age="5d" />
                </>
              }
            />
          </div>
          <Annot>
            <strong className="text-fg-2">Breadcrumb tint</strong> appears
            because{" "}
            <code className="text-[11px]">
              memory.lifecycle.pending_dream_action
            </code>{" "}
            is non-null. Topic card chip counts are non-overlapping:{" "}
            <code className="text-[11px]">+3</code> inbound from unassigned,{" "}
            <code className="text-[11px]">2</code> inter-topic moves.
          </Annot>
        </Frame>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 3 — Tap for reason
// ═════════════════════════════════════════════════════════════════════════

// Popover demo — uses the REAL DreamActionPopoverBody component from
// @memaxlabs/ui (the same primitive memory rows mount inside a
// HoverCardContent in production). The surrounding rounded-card frame
// + arrow approximates the HoverCardContent chrome for a static
// visual reference; the inner contents are the actual shipped
// component. If this block looks right, shipped UI looks right.
const kitchenOrganizeAction: DreamActionDisplayRef = {
  action_type: "organize",
  from_topic: { id: "t-eval-infra", name: "Eval infra", icon: "server" },
  to_topic: {
    id: "t-retrieval-eval",
    name: "Retrieval eval",
    icon: "code",
  },
  reason: "semantic fit with eval harness cluster",
};

function Popover() {
  return (
    <div
      className="relative rounded-xl p-3"
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
        boxShadow: "0 8px 32px rgba(0,0,0,0.08)",
        marginLeft: 40,
        marginTop: 12,
        width: "fit-content",
      }}
    >
      <div
        className="absolute w-3 h-3"
        style={{
          top: -6,
          left: 40,
          background: "var(--card)",
          borderTop: "1px solid var(--border)",
          borderLeft: "1px solid var(--border)",
          transform: "rotate(45deg)",
        }}
        aria-hidden
      />
      <DreamActionPopoverBody
        action={kitchenOrganizeAction}
        headerLabel="Moved by dream"
        unassignedLabel="Unassigned"
        verbFallbackLabel="organized"
      />
    </div>
  );
}

function Act3() {
  return (
    <div className="mb-10">
      <ActHead
        index={3}
        title="User taps the tinted chip — reason"
        sub="Curious about what changed, user taps the signature-tinted breadcrumb. Popover anchored to the chip explains what dream did, why. Popover reads from the memory row's already-loaded data — no extra API call."
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Frame tag="Tap indicator">
          <Row
            indicator={<AvatarCircle hue={60} letter="J" />}
            title="Eval harness output format"
            trailing={
              <>
                {/* Ring wrapper simulates a tap/focus state on the real
                    TopicChip — same primitive used at row/history/popover
                    surfaces; this demo just visually highlights that the
                    chip is the active target. */}
                <span
                  className="inline-flex rounded-md ring-2 ring-offset-2"
                  style={
                    {
                      "--tw-ring-color": "var(--signature)",
                      "--tw-ring-offset-color": "var(--background)",
                    } as React.CSSProperties
                  }
                >
                  <TopicChip tone="dream" label="Retrieval eval" icon="code" />
                </span>
                <AgeMeta age="5d" />
              </>
            }
          />
          <Popover />
        </Frame>

        <Frame tag="Payload source">
          <div
            className="rounded-lg p-3 font-mono text-[11px] text-fg-2 overflow-auto"
            style={{ background: "var(--card)" }}
          >
            <div className="text-fg-3">
              memory.lifecycle.pending_dream_action
            </div>
            <div>
              = {"{"}
              <br />
              &nbsp;&nbsp;run_id:{" "}
              <span style={{ color: SIGNATURE }}>"3f4a…"</span>,<br />
              &nbsp;&nbsp;action_type:{" "}
              <span style={{ color: SIGNATURE }}>"organize"</span>,<br />
              &nbsp;&nbsp;at:{" "}
              <span style={{ color: SIGNATURE }}>"2026-04-16T23:14:02Z"</span>,
              <br />
              &nbsp;&nbsp;from_topic: {"{ id, name: "}
              <span style={{ color: SIGNATURE }}>"Eval infra"</span>
              {" }"},<br />
              &nbsp;&nbsp;to_topic: {"{ id, name: "}
              <span style={{ color: SIGNATURE }}>"Retrieval eval"</span>
              {" }"},<br />
              &nbsp;&nbsp;reason:{" "}
              <span style={{ color: SIGNATURE }}>"Closer semantic fit…"</span>
              <br />
              {"}"}
            </div>
          </div>
          <Annot>
            <strong className="text-fg-2">
              No new API call to open the popover.
            </strong>{" "}
            Content already in the row's payload from the list read. Instant. If
            from_topic is out-of-scope the server returns{" "}
            <code className="text-[11px]">from_topic: null</code> and the
            popover renders verb + reason only.
          </Annot>
        </Frame>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 4 — Visit clears everything
// ═════════════════════════════════════════════════════════════════════════

function Act4() {
  return (
    <div className="mb-10">
      <ActHead
        index={4}
        title="User visits the topic — everything clears"
        sub="User taps the topic name. POST /v1/topics/:id/visit fires after 300ms dwell (never prefetch). Client invalidates caches; next fetch returns pending_dream_action null and delta_since_visit null. Both signals gone."
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Frame tag="Before visit" tagTone="before">
          <TopicCard
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            age="3d"
            pinned
            delta="+3 · 2 reorganized"
            highlight
          />
          <div className="mt-4">
            <Row
              indicator={<AvatarCircle hue={60} letter="J" />}
              title="Eval harness output format"
              trailing={
                <>
                  <TopicChip tone="dream" label="Retrieval eval" icon="code" />
                  <AgeMeta age="5d" />
                </>
              }
            />
          </div>
          <Annot>
            <strong style={{ color: "var(--signature)" }}>
              User taps topic →
            </strong>{" "}
            <code className="text-[11px]">markVisit(topicId)</code> after 300ms
            dwell.
          </Annot>
        </Frame>

        <Frame tag="After visit — next fetch" tagTone="after">
          <TopicCard
            name="Retrieval eval"
            description="Server-side recall harness, 59 queries, graded metrics."
            age="3d"
            pinned
          />
          <div className="mt-4">
            <Row
              indicator={<AvatarCircle hue={60} letter="J" />}
              title="Eval harness output format"
              trailing={
                <>
                  <TopicChip label="Retrieval eval" icon="code" />
                  <AgeMeta age="5d" />
                </>
              }
            />
          </div>
          <Annot>
            <strong className="text-fg-2">Both signals gone.</strong> Server
            query joins against{" "}
            <code className="text-[11px]">topic_visits.last_visited_at</code>;
            visit timestamp is now past the action's{" "}
            <code className="text-[11px]">created_at</code>, so the scoped
            lookups return null + empty summary. No client mutation needed
            beyond cache invalidation.
          </Annot>
        </Frame>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 5 — Detail dream history (durable)
// ═════════════════════════════════════════════════════════════════════════

function Act5() {
  return (
    <div className="mb-10">
      <ActHead
        index={5}
        title="Memory detail — durable dream history"
        sub="Even after scan-surface signals clear, user can learn what dream did. Detail page's provenance strip carries dream_history (last 10 actions, unscoped by visit). Reads like quiet context — not a callout."
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Frame tag="Memory detail page">
          <div
            className="rounded-xl p-4"
            style={{
              background: "var(--background)",
              border: "1px solid var(--border)",
            }}
          >
            <div
              className="text-[21px] font-bold text-fg-1 mb-2"
              style={{ letterSpacing: "-0.02em" }}
            >
              Eval harness output format
            </div>
            <div className="flex items-center flex-wrap gap-x-1.5 gap-y-0.5 text-[13px]">
              <Globe className="h-3.5 w-3.5 shrink-0 text-fg-3" />
              <span className="text-fg-2 font-medium">Jiahao</span>
              <span className="text-fg-4">·</span>
              <span className="text-fg-3">memax</span>
              <span className="text-fg-4">·</span>
              <span className="text-fg-3">captured 5d ago</span>
              <span className="text-fg-4">·</span>
              <span className="text-fg-3 tabular-nums">recalled 14×</span>
            </div>
            <div className="mt-2 flex flex-col gap-1">
              {/* Section heading — single ✦ anchor for the block. memax
                  signature star glyph, not a lucide icon — brand continuity
                  with the Dream bar, delta chips, and popover header. */}
              <div className="flex items-center gap-1.5 text-[12px] text-fg-2 font-medium">
                <span
                  className="text-[11px] leading-none shrink-0"
                  style={{ color: "var(--signature)" }}
                  aria-hidden
                >
                  ✦
                </span>
                <span>Reorganized by dream</span>
              </div>
              {/* Entry lines — no per-line ✦. All chips are the real
                  shared TopicChip primitive from @memaxlabs/ui; the
                  unassigned pseudo-chip uses kind="unassigned". */}
              <div className="flex flex-col gap-0.5 text-[12px] text-fg-3">
                <div className="flex items-center flex-wrap gap-x-1.5 gap-y-1">
                  <TopicChip label="Eval infra" icon="server" />
                  <ArrowRight
                    className="h-3 w-3 shrink-0 text-fg-4"
                    aria-hidden
                  />
                  <TopicChip label="Retrieval eval" icon="code" />
                  <span className="text-fg-4">·</span>
                  <span className="tabular-nums">6h ago</span>
                  <span className="text-fg-4">·</span>
                  <span className="text-fg-4 truncate max-w-xs">
                    semantic fit with eval harness
                  </span>
                </div>
                <div className="flex items-center flex-wrap gap-x-1.5 gap-y-1">
                  <TopicChip kind="unassigned" label="Unassigned" />
                  <ArrowRight
                    className="h-3 w-3 shrink-0 text-fg-4"
                    aria-hidden
                  />
                  <TopicChip label="Eval infra" icon="server" />
                  <span className="text-fg-4">·</span>
                  <span className="tabular-nums">5d ago</span>
                </div>
              </div>
            </div>
          </div>
          <Annot>
            <strong className="text-fg-2">Pills, not prose.</strong> Section
            heading carries the single ✦. Each entry renders as a visual from →
            to transition via the shared{" "}
            <code className="text-[11px]">TopicChip</code> primitive — same
            component as the row breadcrumb, so topic identity stays consistent.
            Organize-from-unassigned uses a{" "}
            <code className="text-[11px]">CircleOff</code> pseudo-chip.
          </Annot>
        </Frame>

        <Frame tag="Clear semantics">
          <div className="flex flex-col gap-3">
            <div
              className="rounded-xl p-3 text-[13px]"
              style={{ background: "var(--surface-1)" }}
            >
              <div className="font-semibold text-fg-1 mb-1.5">
                <span style={{ color: SIGNATURE }}>✦</span> pending_dream_action
              </div>
              <ul className="m-0 pl-4 text-fg-2 text-[12px] leading-relaxed">
                <li>
                  Scoped to viewer's last visit of the memory's current topic.
                </li>
                <li>Shows: row breadcrumb tint + tap popover.</li>
                <li>
                  Clears: on{" "}
                  <code className="text-[11px]">POST /v1/topics/:id/visit</code>
                  .
                </li>
              </ul>
            </div>
            <div
              className="rounded-xl p-3 text-[13px]"
              style={{ background: "var(--surface-1)" }}
            >
              <div className="font-semibold text-fg-1 mb-1.5">
                <span style={{ color: SIGNATURE }}>✦</span> dream_history
              </div>
              <ul className="m-0 pl-4 text-fg-2 text-[12px] leading-relaxed">
                <li>
                  Unscoped by visit. Last 10 actions, reverse-chronological.
                </li>
                <li>Shows: low-emphasis provenance lines on memory detail.</li>
                <li>
                  Clears: <strong>never</strong>. Durable audit for "when did
                  this move?"
                </li>
              </ul>
            </div>
            <div
              className="rounded-xl p-3 text-[13px]"
              style={{ background: "var(--surface-1)" }}
            >
              <div className="font-semibold text-fg-1 mb-1.5">
                <span style={{ color: SIGNATURE }}>✦</span> delta_since_visit
              </div>
              <ul className="m-0 pl-4 text-fg-2 text-[12px] leading-relaxed">
                <li>Per-topic aggregate since viewer's last visit.</li>
                <li>
                  Shows: topic card trailing chip{" "}
                  <code className="text-[11px]">✦ +N · M reorganized</code>.
                </li>
                <li>Clears: on topic open (same visit write).</li>
              </ul>
            </div>
          </div>
        </Frame>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Act 6 — Notifications ≠ lifecycle
// ═════════════════════════════════════════════════════════════════════════

function InboxRow({
  kind,
  kindColor,
  kindLabel,
  body,
  Icon,
}: {
  kind: string;
  kindColor: string;
  kindLabel: string;
  body: React.ReactNode;
  Icon: typeof AlertTriangle;
}) {
  return (
    <div
      className="flex items-center gap-3 px-3.5 py-2.5 rounded-lg mb-2"
      style={{
        background: "var(--card)",
        border: "1px solid var(--border)",
      }}
    >
      <span
        className="inline-flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded"
        style={{
          background: `oklch(from ${kindColor} l c h / 0.12)`,
          color: kindColor,
        }}
      >
        <Icon className="h-3 w-3" />
        {kindLabel}
      </span>
      <span className="flex-1 text-[13px] text-fg-2">{body}</span>
    </div>
  );
}

function Act6() {
  return (
    <div className="mb-4">
      <ActHead
        index={6}
        title="Notifications ≠ lifecycle — the split, visually"
        sub="Contradictions, dream-run completion, topic-review suggestions, hub invites — all fire as notifications (attention events). Lifecycle only surfaces the four passive actions that don't need user review."
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <Frame tag="Notifications (review surface)" tagTone="before">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-fg-3 mb-3">
            Inbox
          </div>
          <InboxRow
            kind="contradiction"
            kindColor="oklch(0.7 0.15 60)"
            kindLabel="Contradiction"
            body="These memories got tangled — which one wins?"
            Icon={AlertTriangle}
          />
          <InboxRow
            kind="dream"
            kindColor="var(--signature)"
            kindLabel="Dream"
            body="Dream run completed · 3 organized, 2 reorganized"
            Icon={Sparkles}
          />
          <InboxRow
            kind="merge"
            kindColor="oklch(0.55 0.12 220)"
            kindLabel="Topic merge"
            body={
              <>
                Suggest merging <em>Pricing</em> into <em>Business model</em>?
              </>
            }
            Icon={GitMerge}
          />
          <Annot>
            <strong className="text-fg-2">User action expected.</strong>{" "}
            Resolve, dismiss, or ignore. Inbox{" "}
            <code className="text-[11px]">seen_at</code> tracks per-user.
            Lifecycle doesn't touch this surface.
          </Annot>
        </Frame>

        <Frame tag="Lifecycle (passive browse)" tagTone="after">
          <div className="text-[11px] font-semibold uppercase tracking-wider text-fg-3 mb-3">
            Fresh memories
          </div>
          <div style={{ background: "var(--background)" }}>
            <Row
              indicator={<AvatarCircle hue={60} letter="J" />}
              title="Hub email invites plan v4 approved"
              trailing={
                <>
                  <TopicChip
                    tone="dream"
                    label="Hub architecture"
                    icon="layout"
                  />
                  <AgeMeta age="6h" />
                </>
              }
            />
            <RowDivider />
            <Row
              indicator={<AvatarCircle hue={200} letter="Z" />}
              title="Fly.io cache warmer needs GCS mount"
              trailing={
                <>
                  <TopicChip label="Infrastructure" icon="server" />
                  <AgeMeta age="1d" />
                </>
              }
            />
          </div>
          <Annot>
            <strong className="text-fg-2">No user action expected.</strong>{" "}
            Tinted chips quietly mark what dream moved; user either notices or
            doesn't. Opening the topic clears the signal. No inbox entry, no
            badge, no ping.
          </Annot>
        </Frame>
      </div>

      <div
        className="mt-5 rounded-xl p-4 text-[13px]"
        style={{ background: "var(--surface-1)" }}
      >
        <strong className="text-fg-1">Rule: </strong>
        <span className="text-fg-2">
          Anything that requires a human decision is a{" "}
          <strong>notification</strong>. Anything that's "memax quietly did this
          while you were away" is <strong>lifecycle</strong>.{" "}
          <code className="text-[11px]">contradiction</code>,{" "}
          <code className="text-[11px]">dream_run_completed</code>, topic-merge
          reviews stay notification-only. Lifecycle only renders{" "}
          <code className="text-[11px]">organize</code> /{" "}
          <code className="text-[11px]">merge</code> /{" "}
          <code className="text-[11px]">archive</code> /{" "}
          <code className="text-[11px]">restructure</code> — the server query
          enforces it.
        </span>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Summary
// ═════════════════════════════════════════════════════════════════════════

function Summary() {
  return (
    <div className="mt-8 grid grid-cols-1 md:grid-cols-2 gap-4">
      <div
        className="rounded-xl p-4 text-[13px]"
        style={{ background: "var(--surface-1)" }}
      >
        <div className="font-semibold text-fg-1 mb-2">What you get free</div>
        <ul className="m-0 pl-4 text-fg-2 text-[12px] leading-relaxed">
          <li>Halo on every fresh memory (no API — pure age math).</li>
          <li>
            Breadcrumb tint on any memory dream moved, until you visit the
            topic.
          </li>
          <li>
            Topic card delta chip showing aggregate activity since last visit.
          </li>
          <li>
            Durable dream history on memory detail — always answers "when did
            this move?"
          </li>
        </ul>
      </div>
      <div
        className="rounded-xl p-4 text-[13px]"
        style={{ background: "var(--surface-1)" }}
      >
        <div className="font-semibold text-fg-1 mb-2">What doesn't happen</div>
        <ul className="m-0 pl-4 text-fg-2 text-[12px] leading-relaxed">
          <li>
            No per-memory acknowledgement table — no client mutation on scroll.
          </li>
          <li>No ping/toast/inbox entry for passive dream organization.</li>
          <li>No halo beyond 24h — no indefinite shimmer.</li>
          <li>
            No cross-tenant topic name leak — out-of-scope topics render
            verb-only.
          </li>
          <li>
            No contradiction verb in lifecycle UI — contradictions are a
            notification.
          </li>
        </ul>
      </div>
    </div>
  );
}

// ═════════════════════════════════════════════════════════════════════════
// Top-level section
// ═════════════════════════════════════════════════════════════════════════

export function LifecycleFlowSection() {
  return (
    <Section
      title="41. Lifecycle + Dream Delta — E2E Flow"
      description="What a user actually sees as memax captures, organizes, and shows their memories over time. Maps to shipped behavior in commit 11f83406. Two distinct signal families — notifications (attention/review events) and lifecycle (passive browse-context changes) — never overlap."
    >
      <div
        className="rounded-xl p-4 mb-8 text-[12px] text-fg-2 leading-relaxed"
        style={{ background: "var(--surface-1)" }}
      >
        <div className="flex flex-wrap gap-x-5 gap-y-2">
          <div>
            <strong className="text-fg-1">Halo</strong> — client-derived from{" "}
            <code className="text-[11px]">created_at</code>: &lt;5min breathing
            → &lt;24h static → off.
          </div>
          <div>
            <strong className="text-fg-1">Breadcrumb tint</strong> — from{" "}
            <code className="text-[11px]">pending_dream_action</code>, clears on
            topic visit.
          </div>
          <div>
            <strong className="text-fg-1">Delta chip</strong> — from{" "}
            <code className="text-[11px]">delta_since_visit</code>, clears on
            topic open.
          </div>
          <div>
            <strong className="text-fg-1">Dream history</strong> — detail only,
            durable, never clears.
          </div>
        </div>
      </div>

      <Act1 />
      <Act2 />
      <Act3 />
      <Act4 />
      <Act5 />
      <Act6 />
      <Summary />
    </Section>
  );
}
