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
import { useActiveHub, useAuth } from "@/lib/auth";
import { formatAge } from "@/lib/format-age";
import { TopicChip } from "@/components/features/topic/topic-chip";
import { buildMemoryDetailPath } from "@/lib/route-helpers";
import { interpolate, pluralize, useInterpolate, useLocale } from "@/i18n";
import {
  registerBoardKind,
  type BoardKindBodyProps,
  type BoardStripSummary,
} from "./board-kind-registry";
import { buildTopicPath } from "@/lib/route-helpers";
import { boardKindEyebrow } from "./board-kind-visuals";
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
      expandable={items.length > 0}
      expanded={open}
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
interface ActivityItem {
  memory_id?: string;
  title?: string;
  created_at?: string;
  agent_slug?: string;
  agent_display_name?: string;
  author_id?: string;
  author_name?: string;
  topic_id?: string;
  topic_name?: string;
  topic_icon?: string;
}

/**
 * ActivityBody — the 动静 receipt (founder redesign 2026-09-22): what
 * landed in the window, grouped by the topic it was filed under, one
 * row per memory with the same attribution vocabulary the memory row
 * uses (author initial · agent identity · age). No weekly comparison,
 * no counters. Boards written before `items` existed fall back to the
 * per-agent sections.
 */
function ActivityBody({ slot }: BoardKindBodyProps) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const items = asArray<ActivityItem>(slot.payload?.items).filter(
    (it) => asString(it.title) !== "",
  );
  const windowHours = asNumber(slot.payload?.window_hours) || 24;
  const label = (
    <BoardKindLabel {...boardKindEyebrow("activity")}>
      {interpolate(t.board.kindActivity, { n: String(windowHours) })}
    </BoardKindLabel>
  );

  if (items.length === 0) {
    const agents = asArray<TraceAgent>(slot.payload?.agents);
    return (
      <>
        {label}
        {agents.map((agent) => (
          <TraceAgentSection
            key={asString(agent.slug) || "manual"}
            agent={agent}
          />
        ))}
      </>
    );
  }

  // Group by topic, keeping the newest-first order of first appearance.
  const groups = new Map<
    string,
    { id: string; name: string; icon: string; items: ActivityItem[] }
  >();
  for (const it of items) {
    const id = asString(it.topic_id);
    const g = groups.get(id);
    if (g) g.items.push(it);
    else
      groups.set(id, {
        id,
        name: asString(it.topic_name),
        icon: asString(it.topic_icon),
        items: [it],
      });
  }

  return (
    <>
      {label}
      <div className="flex flex-col gap-3">
        {[...groups.values()].map((g) => (
          <ActivityTopicGroup key={g.id || "unassigned"} group={g} />
        ))}
      </div>
    </>
  );
}

function ActivityTopicGroup({
  group,
}: {
  group: { id: string; name: string; icon: string; items: ActivityItem[] };
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const router = useRouter();
  const { activeHub } = useActiveHub();
  const { user } = useAuth();
  const slug = activeHub?.hub.slug ?? null;
  return (
    <div>
      <div className="mb-1 flex items-center gap-2">
        {group.id ? (
          <TopicChip
            kind="topic"
            label={group.name}
            icon={group.icon || undefined}
            onClick={() => router.push(buildTopicPath(slug, group.id))}
          />
        ) : (
          <TopicChip kind="unassigned" label={t.board.activityUnassigned} />
        )}
        <span className="text-[11px] text-fg-4">
          {pluralize(
            t.board.traceCountOne,
            t.board.traceCount,
            group.items.length,
          )}
        </span>
      </div>
      <ul className="m-0 flex list-none flex-col p-0">
        {group.items.map((it) => {
          const memoryId = asString(it.memory_id);
          const agentSlug = asString(it.agent_slug);
          const identity = agentSlug ? resolveAgentIdentity(agentSlug) : null;
          const AgentIcon = identity?.icon;
          const agentName =
            asString(it.agent_display_name) || identity?.displayName || "";
          const authorName = asString(it.author_name);
          const isMe = !!user?.id && asString(it.author_id) === user.id;
          const who = isMe ? t.board.activityYou : authorName;
          const created = asString(it.created_at);
          return (
            <li key={memoryId || asString(it.title)}>
              <button
                type="button"
                onClick={
                  memoryId
                    ? () => router.push(buildMemoryDetailPath(slug, memoryId))
                    : undefined
                }
                className="flex w-full cursor-pointer items-center gap-3 rounded-chrome px-2 py-1.5 text-left transition-colors hover:bg-surface-1"
              >
                <span className="min-w-0 flex-1 truncate text-[13px] text-fg-1">
                  {asString(it.title)}
                </span>
                <span className="inline-flex shrink-0 items-center gap-1.5 text-[11px] text-fg-3">
                  {who ? (
                    <>
                      {/* Same initial-avatar shape the memory row draws. */}
                      <span
                        aria-hidden
                        className="flex h-[18px] w-[18px] items-center justify-center rounded-full text-[9px] font-medium text-foreground/55"
                        style={{
                          background:
                            "oklch(from var(--foreground) l c h / 0.08)",
                        }}
                      >
                        {(authorName || who)[0]?.toUpperCase()}
                      </span>
                      <span>{who}</span>
                    </>
                  ) : null}
                  {agentName ? (
                    <>
                      {who ? <span className="text-fg-4">·</span> : null}
                      {AgentIcon ? (
                        <AgentIcon
                          className="h-3.5 w-3.5"
                          style={{ color: identity?.color }}
                          strokeWidth={1.8}
                        />
                      ) : null}
                      <span>{agentName}</span>
                    </>
                  ) : null}
                  {created ? (
                    <>
                      <span className="text-fg-4">·</span>
                      <span className="tabular-nums">
                        {formatAge(created, t, interpolate)}
                      </span>
                    </>
                  ) : null}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

registerBoardKind("activity", ActivityBody, {
  purpose: (t) => t.board.activityPurpose,
  // Additive: the card is a receipt of what landed, not a state with a
  // 历史 — the history disclosure was confusing here (founder, 09-22).
  strip: (slot, t) =>
    stripFromPayload(
      t.board.stripActivity,
      (() => {
        const total =
          asArray<ActivityItem>(slot.payload?.items).length ||
          asArray<TraceAgent>(slot.payload?.agents).reduce(
            (sum, a) => sum + asNumber(a.count),
            0,
          );
        return total > 0
          ? pluralize(t.board.traceCountOne, t.board.traceCount, total)
          : undefined;
      })(),
    ),
});
