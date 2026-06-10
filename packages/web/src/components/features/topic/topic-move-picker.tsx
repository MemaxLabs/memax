"use client";

import { useEffect, useRef, useState } from "react";
import { Home, Search } from "lucide-react";
import type { TopicTree } from "memax-sdk";
import { TopicIcon } from "./topic-icon";
import { useLocale } from "@/i18n";
import { useIsMobile } from "@/hooks/use-is-mobile";

interface TopicMovePickerProps {
  topic: TopicTree;
  hubId: string;
  /**
   * Current hub's display name. Surfaced as the label of the top-level
   * (root) destination entry — consistent with the drag-and-drop hub
   * root drop row, which uses the same label. When empty (auth not yet
   * hydrated), the top-level entry is hidden and the caller falls back
   * to a neutral success string via t.topics.topicMoved.
   */
  hubName: string;
  forest: readonly TopicTree[];
  /**
   * Set of topic ids that must be excluded from the destination list
   * (typically the moving topic itself plus all its descendants — the
   * same set TopicDndProvider computes on drag-start). Passed in rather
   * than recomputed here so the caller decides the source of truth.
   */
  excludedIds: ReadonlySet<string>;
  onSelect: (parentId: string | null, parentName?: string) => void;
}

/**
 * TopicMovePicker — accessible content body for moving a topic.
 *
 * This is a POPOVER CONTENT COMPONENT, not a modal. It renders a search
 * input + a filtered list of same-hub topics the user can drop the
 * current topic under, including an explicit "Top level" entry for
 * reparenting to the root. Keyboard users reach every destination via
 * arrow keys + Enter. Selecting a destination calls onSelect, which in
 * the tree-node wiring routes through the same
 * useTopicMove.moveTopicWithUndo path as drag/drop — so kbd/touch/mobile
 * users get the same optimistic apply, undo, and error mapping as
 * mouse users.
 *
 * Previously rendered as a full-screen `fixed inset-0 z-50` modal,
 * which caused visible overlap with the topic detail page and violated
 * the memax container-morphing principle (a tree-local action should
 * stay tree-anchored, not spawn a page-wide overlay). The fix, per
 * Codex review, is to strip all modal chrome here and have
 * TopicTreeNode wrap the trigger in <Popover>/<PopoverTrigger asChild>/
 * <PopoverContent side="right">. Escape + click-outside + focus
 * management become the popover primitive's responsibility.
 *
 * Hub scoping: destinations are drawn from the caller's `forest` arg,
 * which in practice is the currently active hub's topic tree. No
 * cross-hub destinations are surfaced — the server also guards
 * (invalid_parent), but the UI must not present them to begin with.
 */
export function TopicMovePicker({
  topic,
  // hubId is not used here — the picker only surfaces topics from the
  // passed-in forest, which is already hub-scoped. Kept for API parity
  // so future multi-hub pickers can reuse the component without a
  // breaking prop change.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  hubId: _hubId,
  hubName,
  forest,
  excludedIds,
  onSelect,
}: TopicMovePickerProps) {
  const { t } = useLocale();
  const isMobile = useIsMobile();
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  // Autofocus the search input on mount so keyboard users can start
  // typing immediately. No global Escape / click-outside listeners —
  // the parent Popover owns dismissal.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Flatten the tree into a depth-annotated list, filtering out excluded
  // nodes (self + descendants). Depth is used for indent.
  const flat: Array<{ topic: TopicTree; depth: number }> = [];
  const walk = (nodes: readonly TopicTree[], depth: number) => {
    for (const node of nodes) {
      if (excludedIds.has(node.id)) continue;
      flat.push({ topic: node, depth });
      if (node.children.length > 0) walk(node.children, depth + 1);
    }
  };
  walk(forest, 0);

  const q = query.trim().toLowerCase();
  const filtered =
    q === ""
      ? flat
      : flat.filter((entry) => entry.topic.name.toLowerCase().includes(q));

  return (
    <div className="w-72 max-w-[320px]" aria-label={t.topics.moveTopic}>
      {/* Search header */}
      <div className="flex items-center gap-2 border-b border-border/40 px-3 py-2">
        <Search className="h-3.5 w-3.5 text-fg-4 shrink-0" />
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t.topics.moveTopic}
          className="flex-1 bg-transparent text-[13px] text-fg-1 placeholder:text-fg-4 focus:outline-none"
        />
      </div>

      {/* Options. Inset rows with px-1 so row hover (rounded-lg) sits
          cleanly inside the glass edge — matches ActionMenu. */}
      <div className="max-h-[50vh] overflow-y-auto p-1">
        {/* Hub-root option — labelled with the current hub name,
            mirroring the drag-and-drop hub-root drop row. Hidden when:
              (a) the topic is already at the root (no-op destination), or
              (b) the hub name hasn't resolved yet (defensive — rare)
            Filtered in/out by the search query against the hub name. */}
        {hubName &&
          topic.parent_id !== null &&
          (q === "" || hubName.toLowerCase().includes(q)) && (
            <button
              type="button"
              onClick={() => onSelect(null, hubName)}
              className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[13px] text-fg-2 cursor-pointer transition-colors ${
                isMobile ? "" : "hover:bg-surface-1"
              }`}
            >
              <Home className="h-3.5 w-3.5 text-fg-3 shrink-0" />
              <span className="flex-1 truncate">{hubName}</span>
            </button>
          )}

        {filtered.length === 0 ? (
          <div className="px-3 py-6 text-center text-[12px] text-fg-4">
            {t.topics.noOtherTopics}
          </div>
        ) : (
          filtered.map((entry) => (
            <button
              key={entry.topic.id}
              type="button"
              onClick={() => onSelect(entry.topic.id, entry.topic.name)}
              className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[13px] text-fg-2 cursor-pointer transition-colors ${
                isMobile ? "" : "hover:bg-surface-1"
              }`}
              style={{ paddingLeft: `${10 + entry.depth * 14}px` }}
            >
              <TopicIcon
                name={entry.topic.icon}
                className="h-3.5 w-3.5 text-fg-3 shrink-0"
              />
              <span className="flex-1 truncate">{entry.topic.name}</span>
              <span className="text-[12px] text-fg-4 tabular-nums shrink-0">
                {entry.topic.total_memory_count}
              </span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}
