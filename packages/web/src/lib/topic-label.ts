import type { TopicTree } from "@/hooks/use-topics";

export interface TopicLabelPathSegment {
  id?: string;
  name: string;
  icon?: string;
}

export type TopicLabelIntent = "placed" | "proposed" | "contested" | "archived";

export interface TopicLabelData {
  path: TopicLabelPathSegment[];
  intent?: TopicLabelIntent;
  onClick?: () => void;
}

export interface CollapsedTopicLabel {
  fullPath: string;
  visible: TopicLabelPathSegment[];
  ellipsis: boolean;
}

export function buildTopicPathLookup(
  trees: TopicTree[] | undefined,
): Map<string, TopicLabelPathSegment[]> {
  const lookup = new Map<string, TopicLabelPathSegment[]>();

  function walk(
    nodes: TopicTree[],
    parentPath: TopicLabelPathSegment[] = [],
  ): void {
    for (const node of nodes) {
      const nextPath = [...parentPath, { id: node.id, name: node.name }];
      lookup.set(node.id, nextPath);
      if (node.children.length > 0) {
        walk(node.children, nextPath);
      }
    }
  }

  if (trees) {
    walk(trees);
  }

  return lookup;
}

export function getCollapsedTopicLabel(
  path: TopicLabelPathSegment[],
): CollapsedTopicLabel {
  return {
    fullPath: path.map((segment) => segment.name).join(" \u203a "),
    visible: path.length > 3 ? path.slice(-2) : path,
    ellipsis: path.length > 3,
  };
}
