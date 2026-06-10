"use client";

/**
 * ChatSourcesRow — linkable memory citations rendered beneath an
 * assistant turn once tool work completes. Mirrors the recall
 * surface's "Sources" pattern so chat citations and recall
 * citations look like the same primitive.
 *
 * The source list is derived client-side from any
 * `recall_memories` tool result on the row (JSON-encoded
 * RecallResult[] on the wire). This is a prototype bridge: when
 * additional retrieval tools land (e.g., search_web), we should
 * promote citations into a typed server-emitted event so the UI
 * doesn't have to know about each tool's result shape.
 *
 * Cross-hub safe: the link target is the slug-less
 * `/memories/<id>` path — the app's middleware handles routing to
 * the correct hub from the memory id. Resolving by hub_id → slug
 * client-side would require auth.hubs lookups and risk linking
 * cross-hub citations to the wrong slug, which codex flagged as a
 * High in the plan review (2026-05-04).
 *
 * Layout (2026 redesign, compact-first):
 *
 * Show the first VISIBLE_COUNT chips inline. When there are more,
 * append a `+N more` pill that expands the remaining chips below
 * in place — mirrors the Perplexity / ChatGPT-search / Phind
 * convention of "few visible, expand on demand". Default collapsed
 * because long turns can cite 15-20+ memories and a full grid
 * blows up the page rhythm.
 */

import { useMemo, useState } from "react";
import Link from "next/link";
import { motion, AnimatePresence } from "framer-motion";
import type { ChatToolCallRecord } from "@/hooks/use-chat-stream";
import { cn } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { interpolate } from "@/i18n/interpolate";
import { buildMemoryDetailPath } from "@/lib/route-helpers";

interface SourceRecord {
  memoryId: string;
  hubId?: string;
  title?: string;
  snippet?: string;
}

export interface ChatSourcesRowProps {
  toolCalls: ChatToolCallRecord[];
}

// Visible chip count before the "+N more" affordance kicks in.
// Three chosen empirically — fits one row at most viewport widths
// and matches the visible-citation count in Perplexity's compact
// row, ChatGPT's search-citation row, and Phind's source strip.
const VISIBLE_COUNT = 3;

export function ChatSourcesRow({ toolCalls }: ChatSourcesRowProps) {
  const { t } = useLocale();
  const [expanded, setExpanded] = useState(false);

  const sources = useMemo<SourceRecord[]>(
    () => extractRecallSources(toolCalls),
    [toolCalls],
  );

  if (sources.length === 0) return null;

  const headSources = sources.slice(0, VISIBLE_COUNT);
  const tailSources = sources.slice(VISIBLE_COUNT);
  const hasMore = tailSources.length > 0;

  return (
    <motion.div
      // Fade-up entry once the turn finalizes — the sources row
      // appears AFTER the thinking pill collapses and the answer
      // body settles, so the user reads "answer → here are the
      // memories I used" as a clean punctuation.
      initial={{ opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.22, ease: [0.16, 1, 0.3, 1] }}
      className="mt-3 flex w-full flex-col gap-1.5"
    >
      <div className="text-[11px] font-medium uppercase tracking-wide text-fg-3">
        {t.chat.sources.heading}
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {headSources.map((src, idx) => (
          <SourcePill key={src.memoryId} index={idx} src={src} t={t} />
        ))}
        {hasMore && !expanded && (
          <button
            type="button"
            onClick={() => setExpanded(true)}
            className={cn(
              "inline-flex items-center gap-1 rounded-full border border-border/40 bg-surface-1",
              "px-2.5 py-1 text-[12px] font-medium text-fg-3 transition-colors",
              "hover:border-border/70 hover:bg-surface-2 hover:text-fg-1",
              "active:scale-[0.98]",
            )}
            aria-expanded={false}
          >
            {interpolate(t.chat.sources.showMore, { n: tailSources.length })}
          </button>
        )}
        <AnimatePresence initial={false}>
          {hasMore &&
            expanded &&
            tailSources.map((src, idx) => (
              <motion.div
                key={src.memoryId}
                initial={{ opacity: 0, scale: 0.96 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.96 }}
                transition={{
                  duration: 0.16,
                  delay: 0.02 * idx,
                  ease: [0.16, 1, 0.3, 1],
                }}
              >
                <SourcePill index={VISIBLE_COUNT + idx} src={src} t={t} />
              </motion.div>
            ))}
        </AnimatePresence>
        {hasMore && expanded && (
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className={cn(
              "inline-flex items-center gap-1 rounded-full",
              "px-2.5 py-1 text-[12px] text-fg-3 transition-colors",
              "hover:text-fg-1",
            )}
            aria-expanded={true}
          >
            {t.chat.sources.showLess}
          </button>
        )}
      </div>
    </motion.div>
  );
}

function SourcePill({
  index,
  src,
  t,
}: {
  index: number;
  src: SourceRecord;
  t: ReturnType<typeof useLocale>["t"];
}) {
  const titleText = src.title?.trim() || t.chat.sources.pillUntitled;
  const tooltip = interpolate(t.chat.sources.pillTitle, { title: titleText });
  return (
    <Link
      href={buildMemoryDetailPath(null, src.memoryId)}
      title={tooltip}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-border/40 bg-surface-1",
        "px-2.5 py-1 text-[12px] text-fg-2 transition-colors hover:border-border/70 hover:bg-surface-2 hover:text-fg-1",
      )}
    >
      <span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-foreground/8 px-1 text-[11px] font-semibold tabular-nums text-fg-2">
        {index + 1}
      </span>
      <span className="max-w-[18ch] truncate">{titleText}</span>
    </Link>
  );
}

// ── Source extraction ───────────────────────────────────────────

function extractRecallSources(toolCalls: ChatToolCallRecord[]): SourceRecord[] {
  const seen = new Set<string>();
  const out: SourceRecord[] = [];
  for (const tc of toolCalls) {
    if (tc.name !== "recall_memories") continue;
    if (tc.result === undefined || tc.is_error) continue;
    const raw = typeof tc.result === "string" ? tc.result : "";
    if (!raw) continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      continue;
    }
    if (!Array.isArray(parsed)) continue;
    for (const entry of parsed) {
      if (!entry || typeof entry !== "object") continue;
      const e = entry as Record<string, unknown>;
      const memoryId = typeof e.memory_id === "string" ? e.memory_id : "";
      if (!memoryId || seen.has(memoryId)) continue;
      seen.add(memoryId);
      out.push({
        memoryId,
        hubId: typeof e.hub_id === "string" ? e.hub_id : undefined,
        title: typeof e.title === "string" ? e.title : undefined,
        snippet: typeof e.snippet === "string" ? e.snippet : undefined,
      });
    }
  }
  return out;
}
