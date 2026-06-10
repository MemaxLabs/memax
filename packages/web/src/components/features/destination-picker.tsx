"use client";

import { cn } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { DrillDownTree } from "./drill-down-tree";

interface DestinationPickerProps {
  /**
   * Called when user selects a topic. hubId set for cross-hub.
   * destinationName is the display string the picker rendered for the
   * row so consumers can show an exactly-matching success toast.
   */
  onSelectTopic?: (
    topicId: string,
    hubId?: string,
    destinationName?: string,
  ) => void;
  /**
   * Called when user selects a hub without a topic.
   * destinationName is the hub display label the picker rendered.
   */
  onSelectHub?: (hubId: string, destinationName?: string) => void;
  /** Whether hub rows are selectable destinations in select mode. */
  allowHubSelection?: boolean;
  /** Navigate to a topic (browse mode) */
  onNavigate?: (topicId: string) => void;
  /** Close the picker */
  onClose: () => void;
  /** Topic ID to exclude from the list (e.g. current topic) */
  excludeTopicId?: string;
  /** Topic ID to mark as currently selected */
  selectedTopicId?: string;
  /** Select vs browse mode */
  mode?: "select" | "browse";
  /** Chrome style: card (default) or plain (sheet) */
  variant?: "card" | "plain";
  /** Fixed list height for stable animation/scroll */
  listHeight?: number | string;
  /** Extra class for positioning — parent controls layout */
  className?: string;
  /** Inline style for positioning */
  style?: React.CSSProperties;
}

/**
 * DestinationPicker — thin wrapper around DrillDownTree in select mode.
 *
 * Adds container styling (solid card, border, shadow). Parent controls
 * positioning and dismissal (via Popover or PickerBackdrop).
 *
 * Used by: BatchToolbar (batch move), TopicLocation (single memory move).
 */
export function DestinationPicker({
  onSelectTopic,
  onSelectHub,
  allowHubSelection = true,
  onNavigate,
  onClose,
  excludeTopicId,
  selectedTopicId,
  mode = "select",
  variant = "card",
  listHeight,
  className,
  style,
}: DestinationPickerProps) {
  const { t } = useLocale();
  const chrome = variant === "card";
  const resolvedListHeight =
    listHeight ?? (chrome ? (mode === "select" ? 268 : 320) : undefined);
  return (
    <div
      className={cn(
        chrome ? "rounded-surface overflow-hidden" : "overflow-hidden",
        className,
      )}
      style={
        chrome
          ? {
              border: "1px solid var(--border)",
              background: "var(--card)",
              boxShadow:
                "0 8px 32px oklch(0 0 0 / 0.08), 0 2px 8px oklch(0 0 0 / 0.04)",
              minWidth: 220,
              maxHeight: 320,
              ...style,
            }
          : style
      }
    >
      {/* Mode hint — informational copy, not chrome. Render in both
          "card" and "plain" so the outer container (PopoverContent with
          its own glass shell) still gets the helpful prompt; the old
          `&& chrome` gate suppressed it inside plain wrappers. */}
      {mode === "select" && (
        <div className="border-b border-border/30 px-3 py-2.5">
          <p className="text-[12px] text-fg-3">{t.topics.movePickerHint}</p>
        </div>
      )}
      <DrillDownTree
        mode={mode}
        onSelect={(target) => {
          if (target.topicId) {
            onSelectTopic?.(
              target.topicId,
              target.hubId,
              target.destinationName,
            );
            return;
          }
          if (target.hubId) {
            onSelectHub?.(target.hubId, target.destinationName);
          }
        }}
        onNavigate={onNavigate}
        onClose={onClose}
        allowHubSelection={allowHubSelection}
        excludeTopicId={excludeTopicId}
        selectedTopicId={selectedTopicId}
        listHeight={resolvedListHeight}
      />
    </div>
  );
}
