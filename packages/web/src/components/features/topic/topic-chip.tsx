"use client";

// ═══════════════════════════════════════════════
// TopicChip (web adapter).
//
// The pure primitive lives in @memaxlabs/ui. This file is a thin
// compat layer that:
//   1. Accepts the web-side breadcrumb data shape (full path + web
//      collapsing policy) and forwards pre-collapsed segments to the
//      ui primitive.
//   2. Maps web's TopicLabelIntent to the ui's TopicChipIntent (they
//      are structurally identical today; the indirection keeps us free
//      to diverge intents on either side).
//
// Every consumer on the web side should import from THIS file, not
// directly from @memaxlabs/ui, so the collapsing policy stays in
// packages/web where the full TopicTree lives. Kitchen imports the
// @memaxlabs/ui primitive directly because it has no paths to collapse.
// ═══════════════════════════════════════════════

import type { ReactNode } from "react";
import { TopicChip as UITopicChip, type TopicChipTone } from "@memaxlabs/ui";
import {
  getCollapsedTopicLabel,
  type TopicLabelIntent,
  type TopicLabelPathSegment,
} from "@/lib/topic-label";

export type { TopicChipTone };

interface WebTopicChipSharedProps {
  tone?: TopicChipTone;
  intent?: TopicLabelIntent;
  onClick?: () => void;
  popover?: ReactNode;
  title?: string;
  className?: string;
}

interface WebTopicChipBreadcrumbProps extends WebTopicChipSharedProps {
  /** Full path — web-side collapsing policy is applied here, not in
   *  the ui primitive. */
  segments: TopicLabelPathSegment[];
  label?: never;
  icon?: never;
  kind?: never;
}

interface WebTopicChipSingleProps extends WebTopicChipSharedProps {
  kind?: "topic" | "unassigned";
  label: string;
  icon?: string;
  segments?: never;
}

export type TopicChipProps =
  | WebTopicChipBreadcrumbProps
  | WebTopicChipSingleProps;

export function TopicChip(props: TopicChipProps) {
  if ("segments" in props && props.segments) {
    const { visible, ellipsis } = getCollapsedTopicLabel(props.segments);
    const { segments: _segments, ...rest } = props;
    return (
      <UITopicChip visibleSegments={visible} ellipsis={ellipsis} {...rest} />
    );
  }

  // label branch — pass through; the ui primitive handles kind + icon.
  return <UITopicChip {...(props as WebTopicChipSingleProps)} />;
}
