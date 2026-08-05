"use client";

/**
 * Lane A kind renderers (plan 25 P1): 行迹 trace, 项目脉搏 pulse,
 * 时间胶囊 capsule, 周对比 week. Server payloads are structured data
 * (see Go model.Board*Payload); all user-facing copy is composed here
 * through i18n. Visual reference: kitchen section 44.
 */

import { useRouter } from "next/navigation";
import { BoardAgentRow, BoardKindLabel, BoardMemQuote } from "@memaxlabs/ui";
import { resolveAgentIdentity } from "@memaxlabs/ui/tokens/agents";
import { useActiveHub } from "@/lib/auth";
import { buildMemoryDetailPath } from "@/lib/route-helpers";
import { pluralize, useInterpolate, useLocale } from "@/i18n";
import {
  registerBoardKind,
  type BoardKindBodyProps,
} from "./board-kind-registry";

interface TraceAgent {
  slug?: string;
  display_name?: string;
  count?: number;
  latest_title?: string;
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

function TraceBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const agents = asArray<TraceAgent>(slot.payload?.agents);
  const windowHours = asNumber(slot.payload?.window_hours) || 24;
  return (
    <>
      <BoardKindLabel>
        {interpolate(t.board.kindTrace, { n: String(windowHours) })}
      </BoardKindLabel>
      {agents.map((agent) => {
        const slug = asString(agent.slug);
        const identity = slug ? resolveAgentIdentity(slug) : null;
        const name =
          asString(agent.display_name) ||
          identity?.displayName ||
          slug ||
          t.board.traceManual;
        return (
          <BoardAgentRow
            key={slug || "manual"}
            dotColor={identity?.color}
            title={pluralize(
              t.board.traceCountOne,
              t.board.traceCount,
              asNumber(agent.count),
            )}
            meta={asString(agent.latest_title)}
            who={name}
          />
        );
      })}
    </>
  );
}

interface PulseTopic {
  topic_id?: string;
  name?: string;
  recent_count?: number;
  contributors?: number;
}

function PulseBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const topics = asArray<PulseTopic>(slot.payload?.topics);
  const windowDays = asNumber(slot.payload?.window_days) || 7;
  return (
    <>
      <BoardKindLabel>
        {interpolate(t.board.kindPulse, { n: String(windowDays) })}
      </BoardKindLabel>
      {topics.map((topic) => {
        const contributors = asNumber(topic.contributors);
        return (
          <BoardAgentRow
            key={asString(topic.topic_id) || asString(topic.name)}
            dotColor="var(--signature)"
            title={asString(topic.name)}
            meta={pluralize(
              t.board.pulseRecentOne,
              t.board.pulseRecent,
              asNumber(topic.recent_count),
            )}
            who={
              contributors > 1
                ? interpolate(t.board.pulseContributors, {
                    n: String(contributors),
                  })
                : undefined
            }
          />
        );
      })}
    </>
  );
}

function CapsuleBody({ slot }: BoardKindBodyProps) {
  const { t, locale } = useLocale();
  const router = useRouter();
  const { activeHub } = useActiveHub();
  const quote = asString(slot.payload?.quote);
  const memoryId =
    asString(slot.payload?.memory_id) || slot.cite_memory_ids?.[0] || "";
  const when = asString(slot.payload?.when);
  const whenLabel = when
    ? new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
        year: "numeric",
        month: "long",
        day: "numeric",
      }).format(new Date(when))
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

function WeekBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const thisWeek = asNumber(slot.payload?.this_week);
  const lastWeek = asNumber(slot.payload?.last_week);
  return (
    <>
      <BoardKindLabel>{t.board.kindWeek}</BoardKindLabel>
      <p className="m-0 text-[14px] text-fg-1">
        {pluralize(t.board.weekLineOne, t.board.weekLine, thisWeek)}
      </p>
      <p className="m-0 mt-0.5 text-[12.5px] text-fg-3">
        {interpolate(t.board.weekCompare, { n: String(lastWeek) })}
      </p>
    </>
  );
}

registerBoardKind("trace", TraceBody);
registerBoardKind("pulse", PulseBody);
registerBoardKind("capsule", CapsuleBody);
registerBoardKind("week", WeekBody);
