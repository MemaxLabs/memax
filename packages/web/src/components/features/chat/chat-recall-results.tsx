"use client";

/**
 * ChatRecallResults — semantic rendering for recall_memories tool
 * results. The tool returns a JSON array of flat hits
 * ({memory_id, hub_id, title?, snippet, score?}); instead of dumping
 * that JSON, render each hit as a tappable memory row that navigates
 * to /memories/[id] (design rule: every memory tap goes to the one
 * unified detail route). Falls back to nothing (caller shows raw
 * JSON) when the payload doesn't parse as recall hits — the JSON
 * fallback must never lie about unknown shapes.
 */

import { useRouter } from "next/navigation";
import { Brain } from "lucide-react";
import { useLocale } from "@/i18n";

export interface RecallHit {
  memory_id: string;
  hub_id?: string;
  title?: string;
  snippet: string;
  score?: number;
}

/** Parse a tool result (JSON string or decoded value) into recall hits. */
export function parseRecallHits(result: unknown): RecallHit[] | null {
  let value: unknown = result;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  if (!Array.isArray(value)) return null;
  const hits: RecallHit[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") return null;
    const o = item as Record<string, unknown>;
    if (typeof o.memory_id !== "string" || typeof o.snippet !== "string") {
      return null;
    }
    hits.push({
      memory_id: o.memory_id,
      hub_id: typeof o.hub_id === "string" ? o.hub_id : undefined,
      title: typeof o.title === "string" ? o.title : undefined,
      snippet: o.snippet,
      score: typeof o.score === "number" ? o.score : undefined,
    });
  }
  return hits;
}

export function ChatRecallResults({ hits }: { hits: RecallHit[] }) {
  const { t } = useLocale();
  const router = useRouter();

  if (hits.length === 0) {
    return (
      <p className="text-[12px] text-fg-4">{t.chat.thinking.recallNoHits}</p>
    );
  }

  return (
    <div className="flex flex-col">
      {hits.map((hit) => (
        <button
          key={hit.memory_id}
          type="button"
          onClick={() => router.push(`/memories/${hit.memory_id}`)}
          className="group flex w-full items-start gap-2 rounded-md px-1.5 py-1.5 text-left transition-colors hover:bg-surface-2 cursor-pointer"
        >
          <Brain
            className="mt-0.5 h-3 w-3 shrink-0 text-fg-4 group-hover:text-fg-3"
            aria-hidden
          />
          <span className="min-w-0 flex-1">
            {hit.title && (
              <span className="block truncate text-[12px] font-medium text-fg-1">
                {hit.title}
              </span>
            )}
            <span className="line-clamp-2 break-words font-sans text-[12px] leading-snug text-fg-3">
              {hit.snippet}
            </span>
          </span>
          {hit.score !== undefined && (
            <span className="shrink-0 text-[11px] tabular-nums text-fg-4">
              {Math.round(hit.score * 100)}%
            </span>
          )}
        </button>
      ))}
    </div>
  );
}
