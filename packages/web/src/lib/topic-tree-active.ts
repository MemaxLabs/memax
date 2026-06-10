/**
 * Topic-tree active helpers.
 *
 * Path-shape parsers (`getMemoryIdForPath`, `getActiveTopicIdForPath`)
 * moved to `lib/route-helpers.ts` so they can handle both shell-v1
 * (`/memories/...`) and shell-v2 (`/h/<slug>/memories/...`) routes from
 * a single source. This module re-exports them for backwards
 * compatibility with existing import sites; new callers should import
 * directly from `route-helpers`.
 *
 * `buildAncestorTopicIds` stays here because it operates on the topics
 * tree shape, not on URLs.
 */
import type { TopicTree } from "@/hooks/use-topics";

export { getMemoryIdForPath, getActiveTopicIdForPath } from "./route-helpers";

export function buildAncestorTopicIds(
  topics: TopicTree[],
  topicId: string,
): string[] | null {
  const path: string[] = [];
  function visit(nodes: TopicTree[]): string[] | null {
    for (const topic of nodes) {
      if (topic.id === topicId) {
        return [...path];
      }
      path.push(topic.id);
      const found = visit(topic.children);
      if (found) return found;
      path.pop();
    }
    return null;
  }
  return visit(topics);
}
