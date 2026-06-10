import type { Notification } from "memax-sdk";

// Temporary shim for memax-sdk@0.4.1. Remove this and import
// `isNeedsActionKind` from `memax-sdk` once >=0.4.2 is published and consumed.
// Source of truth: MemaxLabs/memax/packages/sdk/src/types.ts.
const NEEDS_ACTION_KINDS = new Set<Notification["kind"]>([
  "review_contradiction",
  "review_topic_merge",
  "review_topic_restructure",
  "review_stale",
  "review_low_confidence",
  "hub_invite",
  "hub_ownership_transfer",
]);

export function isNeedsActionKind(kind: Notification["kind"]): boolean {
  return NEEDS_ACTION_KINDS.has(kind);
}
