import type { TopicTree } from "memax-sdk";

// Shared topic helpers used by topic.ts, topic-archive.ts, recall.ts,
// list.ts, ask.ts and show.ts. Lives in its own module so command files
// never import from each other (circular-dep rule in the CLI skill).

export function topicDisplayCount(
  topic: Pick<TopicTree, "total_memory_count" | "memory_count">,
): number {
  return topic.total_memory_count ?? topic.memory_count ?? 0;
}

export function buildTopicPathMap(
  topics: TopicTree[],
  parentPath = "",
  map = new Map<string, string>(),
): Map<string, string> {
  for (const topic of topics) {
    const path = parentPath ? `${parentPath} / ${topic.name}` : topic.name;
    map.set(topic.id, path);
    if (topic.children?.length) {
      buildTopicPathMap(topic.children, path, map);
    }
  }
  return map;
}

export function flattenTopics(
  topics: TopicTree[],
  output: TopicTree[] = [],
): TopicTree[] {
  for (const topic of topics) {
    output.push(topic);
    if (topic.children?.length) {
      flattenTopics(topic.children, output);
    }
  }
  return output;
}

export function topicDisplayID(id: string, verbose = false): string {
  return verbose ? id : id.slice(0, 8);
}

export function resolveTopicReference(
  topics: TopicTree[],
  ref: string,
): string {
  const normalized = ref.trim().toLowerCase();
  const flat = flattenTopics(topics);

  const exact = flat.find((topic) => topic.id.toLowerCase() === normalized);
  if (exact) {
    return exact.id;
  }

  const prefixMatches = flat.filter((topic) =>
    topic.id.toLowerCase().startsWith(normalized),
  );
  if (prefixMatches.length === 1) {
    return prefixMatches[0].id;
  }
  if (prefixMatches.length > 1) {
    throw new Error(
      `Topic ID prefix is ambiguous. Matches: ${prefixMatches.map((topic) => `${topic.name} (${topic.id.slice(0, 8)})`).join(", ")}`,
    );
  }

  throw new Error(
    "Topic not found. Run `memax topic list` to see available topic IDs.",
  );
}
