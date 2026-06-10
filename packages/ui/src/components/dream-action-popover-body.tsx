"use client";

// ═══════════════════════════════════════════════
// DreamActionPopoverBody — content for the row-breadcrumb hover card
// when memory.lifecycle.pending_dream_action is non-null.
//
// Lives in @memaxlabs/ui so both shipped memory-row mounts AND the
// kitchen design-review deck render the SAME component. No mock body
// — if the kitchen demo looks right, shipped UI looks right.
//
// Strings (header, unassigned label, verb) are passed in as props
// instead of being resolved via useLocale, because @memaxlabs/ui
// cannot depend on web-side i18n. Web callers wire strings through
// from their t.lifecycle.popoverHeader[action_type] etc.
//
// Structure:
//   [✦] header               ← action-aware, caller supplies
//   [from chip] → [to chip]   ← visual transition via TopicChip
//   reason                    ← optional, rendered only when present
//
// Merge/archive actions without a to_topic collapse to a verb-only
// line (caller supplies via verbFallbackLabel). Organize with a null
// from_topic renders kind="unassigned" chip labeled via unassignedLabel.
// ═══════════════════════════════════════════════

import { ArrowRight } from "lucide-react";
import { TopicChip } from "./topic-chip";

/**
 * Minimal view-model shape this primitive needs. Structurally
 * compatible with the SDK's DreamActionRef — web callers just pass
 * their action through. Kept as a local type here so @memaxlabs/ui
 * doesn't depend on memax-sdk (design primitives should stay free
 * of API surface types).
 */
export interface DreamActionDisplayRef {
  action_type: "organize" | "merge" | "archive" | "restructure";
  from_topic: { id: string; name: string; icon?: string } | null;
  to_topic: { id: string; name: string; icon?: string } | null;
  reason?: string;
}

export interface DreamActionPopoverBodyProps {
  action: DreamActionDisplayRef;
  /** Action-aware header string (e.g. "Moved by dream" for organize).
   *  Caller selects based on action.action_type. */
  headerLabel: string;
  /** Label for the unassigned pseudo-chip when action is organize
   *  with a null from_topic. */
  unassignedLabel: string;
  /** Verb-only fallback string when action has no to_topic
   *  (merge/archive rendered as a single verb line). */
  verbFallbackLabel: string;
}

export function DreamActionPopoverBody({
  action,
  headerLabel,
  unassignedLabel,
  verbFallbackLabel,
}: DreamActionPopoverBodyProps) {
  const hasTo = !!action.to_topic;
  const showPills = hasTo;
  const fromIsUnassigned =
    action.action_type === "organize" && !action.from_topic;

  return (
    <div className="flex flex-col gap-1.5 text-[13px] max-w-xs">
      <div className="flex items-center gap-1.5 text-fg-1 font-medium">
        <span
          className="text-[11px] leading-none shrink-0"
          style={{ color: "var(--signature)" }}
          aria-hidden
        >
          ✦
        </span>
        <span>{headerLabel}</span>
      </div>
      <div className="flex items-center flex-wrap gap-x-1.5 gap-y-1 text-fg-2">
        {showPills ? (
          <>
            {fromIsUnassigned ? (
              <TopicChip kind="unassigned" label={unassignedLabel} />
            ) : action.from_topic ? (
              <TopicChip
                label={action.from_topic.name}
                icon={action.from_topic.icon}
              />
            ) : null}
            {(fromIsUnassigned || action.from_topic) && (
              <ArrowRight className="h-3 w-3 shrink-0 text-fg-4" aria-hidden />
            )}
            {action.to_topic && (
              <TopicChip
                label={action.to_topic.name}
                icon={action.to_topic.icon}
              />
            )}
          </>
        ) : (
          <span>{verbFallbackLabel}</span>
        )}
      </div>
      {action.reason && (
        <div className="text-[12px] text-fg-3 leading-relaxed">
          {action.reason}
        </div>
      )}
    </div>
  );
}
