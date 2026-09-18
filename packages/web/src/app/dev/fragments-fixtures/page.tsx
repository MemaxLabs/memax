"use client";

import { use, useState } from "react";
import type { Memory } from "memax-sdk";
import { Surface } from "@memaxlabs/ui";
import { IsMobileProvider } from "@/hooks/use-is-mobile";
import { BarProvider } from "@/contexts/bar-context";
import { InboxSurfaceProvider } from "@/contexts/inbox-surface-context";
import { LocaleProvider, useLocale, type Locale } from "@/i18n";
import { DraggableMemoryRow } from "@/components/features/memory-card/memory-row-draggable";
import { FragmentPreview } from "@/components/features/topic/fragment-preview";
import { FragmentConflictStrip } from "@/components/features/topic/fragment-conflict-strip";
import {
  FragmentsModeControls,
  type FragmentsMode,
  type RecentWindow,
} from "@/components/features/topic/fragment-mode-controls";
import type { FragmentConflict } from "@/lib/fragment-conflicts";

// Dev fixture for the 记忆片段 「最近」 mode (2026-09-17): mounts the
// real row / preview / conflict-strip components on fixture memories so
// the surface can be looked at without a populated database. Actions
// hit real hooks; with no API behind them they just fail quietly.

const minutesAgo = (m: number) =>
  new Date(Date.now() - m * 60_000).toISOString();

function fixtureMemory(
  id: string,
  title: string,
  content: string,
  opts: Partial<Memory> = {},
): Memory {
  return {
    id,
    hub_id: "hub-dev",
    owner_id: "user-dev",
    title,
    content,
    content_type: "text",
    content_hash: id,
    summary: content.slice(0, 140),
    kind: "note",
    stability: "stable",
    retrieval_weight: 1,
    tags: [],
    boundary: "private",
    state: "active",
    pinned: false,
    source: "agent",
    source_agent: "claude-code",
    version: 1,
    access_count: 0,
    created_at: minutesAgo(1),
    updated_at: minutesAgo(1),
    accessed_at: minutesAgo(1),
    ...opts,
  } as Memory;
}

const MEMORIES: Memory[] = [
  fixtureMemory(
    "11111111-1111-4111-8111-111111111111",
    "memax 的 dream merge 超时从 12s 提到 30s",
    "DeepSeek 切换后 dream merge 的 LLM 调用 12 秒会 100% 超时，现在统一 30 秒；DreamCycleWorker 的 job 超时同步提到 30 分钟，和 staleRunThreshold 看门狗一致。",
  ),
  fixtureMemory(
    "22222222-2222-4222-8222-222222222222",
    "手机端 dock 与 ✦ 输入栏共存的层级规则",
    "dock 必须在输入栏任何非 docked 状态下隐藏，不能只看键盘：mirror 态时输入栏降到距底 12px，正好压在 dock 带上，而 MobileBarSurface 的 z 层（51）低于 z-bar（52），dock 会盖住输入行且仍可点。硬件键盘永远不会触发 useKeyboardOpen，所以键盘信号不可靠。顶栏高度要带上刘海 inset，设置面板的锚点才落在头像下面。",
    { created_at: minutesAgo(4) },
  ),
  fixtureMemory(
    "33333333-3333-4333-8333-333333333333",
    "Derek 偏好 oklch 色值，不用 hex",
    "设计稿和代码里颜色一律写 oklch，含透明度用 oklch(from var(--x) l c h / 0.12) 相对语法。",
    { source_agent: "cursor", created_at: minutesAgo(60 * 26) },
  ),
  fixtureMemory(
    "44444444-4444-4444-8444-444444444444",
    "dream 各阶段 LLM 调用统一 12 秒超时",
    "所有 dream 阶段（synthesis / merge / restructure）共用 12 秒 LLM 超时，超时即跳过合并。",
    { source_agent: "cursor", created_at: minutesAgo(60 * 24 * 3) },
  ),
];

const CONFLICT: FragmentConflict = {
  notificationId: "notif-dev-1",
  side: "a",
  other: { id: MEMORIES[3].id, title: MEMORIES[3].title },
  reason: "和 3 天前的一条说法不一致，昨晚做梦时发现",
  similarity: 0.91,
  createdAt: minutesAgo(60 * 9),
};

export default function FragmentsFixturesPage({
  searchParams,
}: {
  searchParams: Promise<{ lang?: string; mode?: string }>;
}) {
  // ?lang=zh|en&mode=recent|all — resolved at render time so a
  // screenshot run gets the right locale and mode from the first paint.
  const params = use(searchParams);
  const locale: Locale = params.lang === "zh" ? "zh" : "en";
  const initialMode: FragmentsMode = params.mode === "all" ? "all" : "recent";
  return (
    <LocaleProvider initialLocale={locale}>
      <FragmentsFixtures initialMode={initialMode} />
    </LocaleProvider>
  );
}

function FragmentsFixtures({ initialMode }: { initialMode: FragmentsMode }) {
  const { t } = useLocale();
  const [mode, setMode] = useState<FragmentsMode>(initialMode);
  const [window, setWindow] = useState<RecentWindow>("3d");
  const [previewId, setPreviewId] = useState<string | null>(MEMORIES[1].id);
  return (
    <IsMobileProvider>
      <InboxSurfaceProvider>
        <BarProvider>
          <div className="mx-auto max-w-3xl px-6 py-16">
            <header className="mb-8 space-y-2">
              <h1 className="text-display-2 font-semibold text-fg-1">
                记忆片段 · 最近模式
              </h1>
              <p className="text-[14px] text-fg-3">
                Dev-only fixture. Real components, fixture memories: the preview
                is pinned open under the second row; the first row carries a
                pending contradiction.
              </p>
            </header>

            <Surface rounded="2xl" className="overflow-hidden">
              <div className="flex flex-wrap items-center gap-1 border-b border-border/30 px-4 py-2.5">
                <span className="text-[13px] font-semibold text-fg-1">
                  {t.memoryView.freshMemory.other}
                </span>
                <span className="ml-auto inline-flex items-center">
                  <FragmentsModeControls
                    mode={mode}
                    window={mode === "all" ? "all" : window}
                    onSwitchMode={setMode}
                    onSwitchWindow={setWindow}
                  />
                </span>
              </div>
              <div>
                {MEMORIES.map((m, i) => (
                  <div key={m.id} onMouseEnter={() => setPreviewId(m.id)}>
                    <DraggableMemoryRow
                      memory={m}
                      surface="recent"
                      topicLabel={{
                        path: [
                          { name: "memax" },
                          { name: i < 2 ? "前端" : "设计" },
                        ],
                      }}
                      showDivider={i > 0}
                      isNew={i === 0}
                      onClick={() => {}}
                    />
                    {previewId === m.id && (
                      <FragmentPreview
                        memory={m}
                        topicPath={["memax", i < 2 ? "前端" : "设计"]}
                        onOpen={() => {}}
                        onHold={() => {}}
                      />
                    )}
                    {i === 0 && (
                      <FragmentConflictStrip
                        memory={m}
                        conflict={CONFLICT}
                        otherMemory={MEMORIES[3]}
                        onOpenOther={() => {}}
                      />
                    )}
                  </div>
                ))}
              </div>
              <div className="flex items-center justify-center border-t border-border/30 px-4 py-2.5">
                <span className="text-[13px] text-fg-3">
                  {t.memoryView.loadMoreRecent}
                </span>
              </div>
            </Surface>
          </div>
        </BarProvider>
      </InboxSurfaceProvider>
    </IsMobileProvider>
  );
}
