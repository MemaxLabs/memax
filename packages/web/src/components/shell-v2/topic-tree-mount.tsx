"use client";

/**
 * TopicTreeMount — wires `<TopicTreeContent>` (existing production
 * component) to `useTopicTreeController` (extracted hook). Used inside
 * `<PinnedSecondaryPanel>` for shell-v2 Memories tab.
 *
 * Sibling to v1's `<PinnedTreeSidebar>` — both consume the same hook so
 * the user's expansion preferences carry across shells.
 *
 * Hub-sync gate: on `/h/<slug>/...` routes the tree reads topics from
 * `auth.activeHubId` via `useTopics()`, which can lag the URL slug for
 * one paint after a deep-link to a different hub (the URL changes
 * before `switchHub` lands). Without the gate the tree briefly flashes
 * the previous hub's topics. We only suppress the tree on hub-scoped
 * routes that aren't synced yet — `/brain`, `/agents`, the bare
 * `/pulse` forwarder, and v1 paths render the tree immediately as
 * before.
 *
 * Topic creation: the tree footer "New topic" button and per-node
 * hover "New subtopic" affordance both open TopicCreateDialog here.
 * On success we navigate straight into the new topic so the user's
 * next action (adding memories) starts from the right place.
 */

import { useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useTopicTreeController } from "@/hooks/use-topic-tree-controller";
import { TopicTreeContent } from "@/components/features/topic/topic-tree-content";
import {
  TopicCreateDialog,
  type TopicCreateTarget,
} from "@/components/features/topic/topic-create-dialog";
import { useTopics } from "@/hooks/use-topics";
import { findTopicName } from "@/lib/topic-label";
import { useV2RouteHubSync } from "@/hooks/use-v2-route-hub-sync";
import { buildTopicPath, getHubSlugForPath } from "@/lib/route-helpers";

export function TopicTreeMount() {
  const pathname = usePathname();
  const router = useRouter();
  const slug = getHubSlugForPath(pathname);
  // The hook is unconditionally called (rules of hooks) but only
  // gates rendering when the URL carries a slug — `slug = ""` is a
  // no-op resolve that always returns `{ resolvedHubId: null,
  // isSynced: false }`, which we ignore on slug-less paths.
  const { isSynced } = useV2RouteHubSync(slug ?? "");
  const { visibleExpandedIds, onToggleExpand, onSpringExpand, activeTopic } =
    useTopicTreeController();
  const { data: topicsData } = useTopics();
  const [createTarget, setCreateTarget] = useState<TopicCreateTarget | null>(
    null,
  );
  if (slug !== null && !isSynced) return null;
  return (
    <>
      <TopicTreeContent
        expandedIds={visibleExpandedIds}
        onToggleExpand={onToggleExpand}
        onSpringExpand={onSpringExpand}
        activeTopic={activeTopic}
        onCreateTopic={() => setCreateTarget({})}
        onCreateSubtopic={(parentId) =>
          setCreateTarget({
            parent: {
              id: parentId,
              name: findTopicName(topicsData?.topics, parentId) ?? "",
            },
          })
        }
      />
      <TopicCreateDialog
        target={createTarget}
        onClose={() => setCreateTarget(null)}
        onCreated={(topicId) =>
          router.push(buildTopicPath(slug, topicId), { scroll: false })
        }
      />
    </>
  );
}
