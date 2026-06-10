"use client";

import { useTopicMemories, useTopic } from "@/hooks/use-topics";
import { useLocale, useInterpolate } from "@/i18n";
import { DataCardSkeleton, DataSectionCard } from "@memaxlabs/ui";
import { MemoryNavRow } from "@/components/features/memory-detail/memory-nav-row";

/**
 * TopicSiblings — contextual navigation at the bottom of memory detail.
 *
 * Shows 2-3 other memories from the same topic. Makes memory detail
 * a node in the knowledge graph, not a dead end.
 *
 * Kitchen reference: section 02, demo 2a (bottom of full case).
 */
export function TopicSiblings({
  topicId,
  currentMemoryId,
}: {
  topicId: string;
  currentMemoryId: string;
}) {
  const {
    data,
    isLoading: memoriesLoading,
    isError: memoriesError,
    refetch: refetchMemories,
  } = useTopicMemories(topicId, 4);
  const {
    data: topic,
    isLoading: topicLoading,
    isError: topicError,
    refetch: refetchTopic,
  } = useTopic(topicId);
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const siblings = (data?.memories ?? [])
    .filter((m) => m.id !== currentMemoryId)
    .slice(0, 3);
  const phase =
    topicLoading || memoriesLoading
      ? "loading"
      : topicError || memoriesError
        ? "error"
        : !topic || siblings.length === 0
          ? "empty"
          : "loaded";

  return (
    <div className="mt-6">
      <DataSectionCard
        label={
          topic ? interpolate(t.note.alsoIn, { topic: topic.name }) : undefined
        }
        phase={phase}
        skeleton={<DataCardSkeleton rows={2} />}
        variant="glass"
        errorCopy={{
          title: t.states.error.network,
          retryLabel: t.states.error.retry,
          onRetry: () => {
            void refetchTopic();
            void refetchMemories();
          },
        }}
        emptyCopy={{ title: t.note.siblingsEmpty }}
      >
        {siblings.map((m) => (
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
