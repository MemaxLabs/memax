"use client";

import { useRelatedMemories } from "@/hooks/use-related-memories";
import { useLocale } from "@/i18n";
import { DataCardSkeleton, DataSectionCard } from "@memaxlabs/ui";
import { MemoryNavRow } from "@/components/features/memory-detail/memory-nav-row";

/**
 * RelatedMemories — semantically related memories via vector nearest-neighbor.
 *
 * Shows 2-3 memories with the highest embedding similarity to the current one.
 * Sub-50ms, no external API calls — pure Postgres HNSW index.
 * Ordered by relevance — no percentage shown (order IS the signal).
 * Renders above TopicSiblings on the memory detail page.
 *
 * This is a decorative reading aid, not a primary surface. Failure modes are
 * deliberately quiet:
 *   - parent memory still processing → muted one-line hint, no card chrome
 *   - server error / network error → collapse entirely (return null)
 * A scary "Couldn't reach memax" card here would imply the whole product is
 * down when only a sidebar query failed.
 */
export function RelatedMemories({
  memoryId,
  memoryState,
}: {
  memoryId: string;
  memoryState?: string;
}) {
  const isProcessing = memoryState === "processing";
  const { data, isLoading, isError } = useRelatedMemories(
    memoryId,
    memoryState,
  );
  const { t } = useLocale();

  if (isProcessing) {
    return (
      <p className="mt-6 text-[12px] text-fg-3">{t.note.relatedProcessing}</p>
    );
  }

  if (isError) {
    return null;
  }

  const related = data ?? [];
  const phase = isLoading
    ? "loading"
    : related.length === 0
      ? "empty"
      : "loaded";

  return (
    <div className="mt-6">
      <DataSectionCard
        label={t.note.related}
        phase={phase}
        skeleton={<DataCardSkeleton rows={2} />}
        variant="glass"
        emptyCopy={{
          title: t.note.relatedEmptyTitle,
          hint: t.note.relatedEmptyHint,
        }}
      >
        {related.map(({ memory: m }) => (
          <MemoryNavRow
            key={m.id}
            memoryId={m.id}
            title={m.title}
            ageISO={m.updated_at}
          />
        ))}
      </DataSectionCard>
    </div>
  );
}
