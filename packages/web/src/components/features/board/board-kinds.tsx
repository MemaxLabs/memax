"use client";

/**
 * Lane A kind renderers (plan 25 P1): 行迹 trace, 项目脉搏 pulse,
 * 时间胶囊 capsule, 周对比 week. Server payloads are structured data
 * (see Go model.Board*Payload); all user-facing copy is composed here
 * through i18n. Visual reference: kitchen section 44.
 */

import { useState } from "react";
import { useRouter } from "next/navigation";
import { BoardAgentRow, BoardKindLabel, BoardMemQuote } from "@memaxlabs/ui";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useActiveHub } from "@/lib/auth";
import { buildMemoryDetailPath } from "@/lib/route-helpers";
import { interpolate, pluralize, useInterpolate, useLocale } from "@/i18n";
import {
  registerBoardKind,
  type BoardKindBodyProps,
  type BoardStripSummary,
} from "./board-kind-registry";
import { buildTopicPath } from "@/lib/route-helpers";
// Side-effect chain: importing the Lane A module also registers the
// Lane B synthesized kinds (dreamlog/echo/thread/openq/pattern/musing/
// decision_gate), so BoardView's single `import "./board-kinds"` brings
// the whole registry up before first render.
import "./board-kinds-lane-b";

interface TraceAgentItem {
  memory_id?: string;
  title?: string;
}

interface TraceAgent {
  slug?: string;
  display_name?: string;
  count?: number;
  latest_title?: string;
  items?: TraceAgentItem[];
}

function asArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}

function asNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/**
 * One agent's line, expandable to the actual memories behind the count
 * — the number is a claim, the titles are the receipts. Each title
 * opens the memory detail.
 */
function TraceAgentSection({ agent }: { agent: TraceAgent }) {
  const { t } = useLocale();
  const router = useRouter();
  const { activeHub } = useActiveHub();
  const [open, setOpen] = useState(false);
  const slug = asString(agent.slug);
  const identity = slug ? resolveAgentIdentity(slug) : null;
  const name =
    asString(agent.display_name) ||
    identity?.displayName ||
    slug ||
    t.board.traceManual;
  const items = asArray<TraceAgentItem>(agent.items).filter(
    (item) => asString(item.title) !== "",
  );
  const row = (
    <BoardAgentRow
      dotColor={identity?.color}
      title={pluralize(
        t.board.traceCountOne,
        t.board.traceCount,
        asNumber(agent.count),
      )}
      meta={
        !open && asString(agent.latest_title)
          ? interpolate(t.board.traceLatest, {
              title: asString(agent.latest_title),
            })
          : undefined
      }
      who={name}
    />
  );
  if (items.length === 0) return row;
  return (
    <div>
      <button
        type="button"
        className="block w-full text-left"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {row}
      </button>
      {open ? (
        <div className="mb-1 ml-4 flex flex-col gap-1.5">
          {items.map((item) => (
            <BoardMemQuote
              key={asString(item.memory_id) || asString(item.title)}
              onClick={
                asString(item.memory_id)
                  ? () =>
                      router.push(
                        buildMemoryDetailPath(
                          activeHub?.hub.slug ?? null,
                          asString(item.memory_id),
                        ),
                      )
                  : undefined
              }
            >
              {asString(item.title)}
            </BoardMemQuote>
          ))}
        </div>
      ) : null}
    </div>
  );
}

interface PulseTopic {
  topic_id?: string;
  name?: string;
  recent_count?: number;
  contributors?: number;
}

function CapsuleBody({ slot }: BoardKindBodyProps) {
  const { t, locale } = useLocale();
  const router = useRouter();
  const { activeHub } = useActiveHub();
  const quote = asString(slot.payload?.quote);
  const memoryId =
    asString(slot.payload?.memory_id) || slot.cite_memory_ids?.[0] || "";
  const when = asString(slot.payload?.when);
  // Validity-guarded: Intl.format throws RangeError on an Invalid
  // Date, which would take down the whole page for one bad payload.
  const whenDate = when ? new Date(when) : null;
  const whenLabel =
    whenDate && !Number.isNaN(whenDate.getTime())
      ? new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
          year: "numeric",
          month: "long",
          day: "numeric",
        }).format(whenDate)
      : "";
  return (
    <>
      <BoardKindLabel star>{t.board.kindCapsule}</BoardKindLabel>
      <BoardMemQuote
        when={whenLabel}
        onClick={
          memoryId
            ? () =>
                router.push(
                  buildMemoryDetailPath(activeHub?.hub.slug ?? null, memoryId),
                )
            : undefined
        }
      >
        “{quote}”
      </BoardMemQuote>
    </>
  );
}

function stripFromPayload(
  label: string,
  detail: string | undefined,
): BoardStripSummary {
  return { label, detail };
}

/**
 * ActivityBody — the folded counts, expanded. Agent traces, topic
 * movement and the week diff were three separate cards; three cards
 * of counters pushed the cards that actually say something off the
 * screen. Now they're one line that opens into the breakdown.
 */
function ActivityBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const agents = asArray<TraceAgent>(slot.payload?.agents);
  const topics = asArray<PulseTopic>(slot.payload?.topics);
  const thisWeek = asNumber(slot.payload?.this_week);
  const lastWeek = asNumber(slot.payload?.last_week);
  const windowHours = asNumber(slot.payload?.window_hours) || 24;

  return (
    <>
      <BoardKindLabel>
        {interpolate(t.board.kindActivity, { n: String(windowHours) })}
      </BoardKindLabel>
      {agents.map((agent) => (
        <TraceAgentSection
          key={asString(agent.slug) || "manual"}
          agent={agent}
        />
      ))}
      {topics.length > 0 ? (
        <p className="m-0 mt-2 text-[12.5px] text-fg-3">
          {t.board.activityTopics}{" "}
          {topics
            .map(
              (topic) =>
                `${asString(topic.name)} (${asNumber(topic.recent_count)})`,
            )
            .join(" · ")}
        </p>
      ) : null}
      <p className="m-0 mt-1 text-[12.5px] text-fg-4">
        {pluralize(t.board.weekLineOne, t.board.weekLine, thisWeek)} ·{" "}
        {interpolate(t.board.weekCompare, { n: String(lastWeek) })}
      </p>
    </>
  );
}

// The activity strip is the board's quietest row: never the hero, it
// collapses to one line and only opens when asked.
registerBoardKind("activity", ActivityBody, {
  purpose: (t) => t.board.activityPurpose,
  strip: (slot, t) =>
    stripFromPayload(
      t.board.stripActivity,
      (() => {
        const total = asArray<TraceAgent>(slot.payload?.agents).reduce(
          (sum, a) => sum + asNumber(a.count),
          0,
        );
        const week = asNumber(slot.payload?.this_week);
        const parts: string[] = [];
        if (total > 0) {
          parts.push(
            pluralize(t.board.traceCountOne, t.board.traceCount, total),
          );
        }
        if (week > 0) {
          parts.push(interpolate(t.board.stripActivityWeek, { n: week }));
        }
        return parts.length > 0 ? parts.join(" · ") : undefined;
      })(),
    ),
});
registerBoardKind("capsule", CapsuleBody, {
  actions: { ack: (t) => t.board.capsuleAck },
  purpose: (t) => t.board.capsulePurpose,
  strip: (_slot, t) => ({ label: t.board.kindCapsule }),
});
