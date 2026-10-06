"use client";

import { use } from "react";
import type { BoardSlot } from "memax-sdk";
import { BoardCard } from "@memaxlabs/ui";
import { LocaleProvider, type Locale } from "@/i18n";
import "@/components/features/board/board-kinds";
import { renderBoardSlotBody } from "@/components/features/board/board-kind-registry";

// Dev fixture for pulse cards (2026-09-22): the real kind renderers on
// fixture payloads so a card can be looked at without a dream run.
// `?lang=` picks the locale.

const NOW = Date.now();
const ago = (h: number) => new Date(NOW - h * 3_600_000).toISOString();

const ACTIVITY: BoardSlot = {
  id: "s-activity",
  board_id: "b1",
  slot_key: "z-activity",
  kind: "activity",
  title: "5 memories in the last 24h",
  state: "fresh",
  created_at: ago(1),
  updated_at: ago(1),
  content_updated_at: ago(1),
  payload: {
    window_hours: 24,
    items: [
      {
        memory_id: "m1",
        title: "Quick-start deck: connector pane, three ways to remember",
        created_at: ago(2),
        agent_slug: "claude-ai",
        author_id: "u-me",
        author_name: "Derek",
        topic_id: "t-eng",
        topic_name: "Memax Engineering",
        topic_icon: "code",
      },
      {
        memory_id: "m2",
        title: "Onboarding surfaces rework: quick-start deck + checklist fix",
        created_at: ago(5),
        agent_slug: "claude-ai",
        author_id: "u-me",
        author_name: "Derek",
        topic_id: "t-eng",
        topic_name: "Memax Engineering",
        topic_icon: "code",
      },
      {
        memory_id: "m3",
        title: "周复盘 09-20：ABC 向导换人，WeChat 接入待昊宝动手，Stripe 停催",
        created_at: ago(18),
        agent_slug: "hermes",
        author_id: "u-ziyang",
        author_name: "Ziyang",
        topic_id: "t-ops",
        topic_name: "运营周复盘",
        topic_icon: "book",
      },
      {
        memory_id: "m4",
        title: "Pulse chips: custom boards get their own chip",
        created_at: ago(20),
        agent_slug: "cursor",
        author_id: "u-me",
        author_name: "Derek",
        topic_id: "t-design",
        topic_name: "Memax UI Design",
        topic_icon: "layout",
      },
      {
        memory_id: "m5",
        title: "牙医说三个月后复查",
        created_at: ago(22),
        agent_slug: "claude-ai",
        author_id: "u-me",
        author_name: "Derek",
      },
    ],
    agents: [
      { slug: "claude-ai", count: 3 },
      { slug: "cursor", count: 1 },
      { slug: "hermes", count: 1 },
    ],
  },
};

export default function PulseFixturesPage({
  searchParams,
}: {
  searchParams: Promise<{ lang?: string }>;
}) {
  const params = use(searchParams);
  const locale: Locale = params.lang === "en" ? "en" : "zh";
  return (
    <LocaleProvider initialLocale={locale}>
      <main className="mx-auto max-w-4xl px-5 py-12 sm:px-8">
        <BoardCard state="fresh" timestamp="1 小时前">
          {renderBoardSlotBody(ACTIVITY)}
        </BoardCard>
      </main>
    </LocaleProvider>
  );
}
