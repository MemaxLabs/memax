// =============================================================================
// memax-sdk — super-notif sub-item primitives (plan 18)
// =============================================================================
//
// Both `checklist` (decision) and `digest` (receipt) notification kinds
// consume the same `Item` shape so future digest producers (weekly
// dream digest, release notes, hub welcomes, …) don't have to
// relitigate field names. The abstraction lives at the Item level, not
// at the parent kind — plan 18 Appendix A.
//
// The TS types here mirror the Go structs in
// packages/server/internal/model/notification.go one-for-one. When you
// add or rename a field, change both files in the same PR.

/**
 * Optional progress-bar payload on an item. Used by the `five_memories`
 * onboarding step today; reusable by any item that tracks a count-toward-
 * target signal.
 */
export interface ItemProgress {
  current: number;
  target: number;
}

/**
 * Shared sub-item shape used by both super-notif kinds.
 *
 * `title` and `description` MUST be plain user-facing strings — no
 * HTML, no markdown, no interpolation placeholders. The inbox unknown-
 * kind fallback renders `payload.title + payload.description` literally
 * when a producer ships ahead of its renderer (plan 18 §4.2).
 */
export interface Item {
  id: string;
  title: string;
  description?: string;
  /** Lucide icon name OR emoji glyph. */
  icon?: string;
  /** Static route path or external URL. Producer is responsible for
   * authoring safe values — no user input. */
  cta_url?: string;
  cta_label?: string;
  viewed_at?: string;
  progress?: ItemProgress;
}

/**
 * Checklist sub-item — extends Item with completion state and
 * dependency metadata.
 *
 * `locked_by` lists other item ids that must complete first; the
 * server's /complete endpoint refuses the call with `400 item_locked`
 * if any dependency is still pending.
 *
 * `trigger` is a server-only hint (e.g. "memory_count_gte:5") consumed
 * by the OnboardingRecorder — the client should ignore it. Surfaced
 * here for type completeness, not for client consumption.
 */
export interface ChecklistItem extends Item {
  completed_at?: string;
  locked_by?: string[];
  trigger?: string;
}

/**
 * Digest sub-item — receipt-kind. Identical shape to Item today; the
 * named type exists so future digest-only fields can land without
 * touching the shared Item.
 */
export type DigestItem = Item;

/**
 * `pin_context` routes a super-notif row to a specific pinned region
 * on the client. Empty / undefined means inbox-only.
 *   - `memories_hero` → rendered above the topic grid on /memories.
 *   - `inbox_hero`    → reserved for the inbox top-strip (future).
 */
export type PinContext = "memories_hero" | "inbox_hero" | "";

/**
 * Payload shape for `kind=checklist`. The server validator enforces:
 *   - title non-empty
 *   - 1 ≤ items.length ≤ 20
 *   - every required_ids entry exists in items[].id
 *   - every locked_by entry exists in items[].id
 */
export interface ChecklistPayload {
  title: string;
  description?: string;
  items: ChecklistItem[];
  required_ids?: string[];
  /** Compact-mode strip label. */
  collapse_hint?: string;
  pin_context?: PinContext;
}

/**
 * Payload shape for `kind=digest`. Same validation as checklist minus
 * required_ids and locked_by — digests don't auto-resolve.
 */
export interface DigestPayload {
  title: string;
  description?: string;
  items: DigestItem[];
  pin_context?: PinContext;
}

/**
 * The kinds that today carry an items[] payload. Renderers that ship
 * before their producer (plan 18 §4.2 fallback) use this set to decide
 * whether to attempt item-level rendering or fall back to literal
 * `payload.title + payload.description`.
 */
export const SUPER_NOTIF_KINDS: ReadonlySet<string> = new Set<string>([
  "checklist",
  "digest",
]);
