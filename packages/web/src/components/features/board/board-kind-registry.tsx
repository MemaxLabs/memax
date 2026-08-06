"use client";

import type { ComponentType, ReactNode } from "react";
import type { BoardSlot } from "memax-sdk";
import { BoardCardFallbackBody, BoardKindLabel } from "@memaxlabs/ui";
import { boardKindEyebrow } from "./board-kind-visuals";

/**
 * L3 kind registry (plan 25). Each board kind registers the component
 * that composes its payload from the L1 atoms. Adding a kind is
 * additive: one component entry here + a producer server-side; nothing
 * else in the pipeline changes. Renderers are React components so they
 * can use hooks (useLocale, useRouter) — the registry itself stays a
 * plain map.
 *
 * Lane A kinds (trace/pulse/capsule/week) register from
 * board-kinds.tsx, Lane B kinds (dreamlog/echo/thread/openq/pattern/
 * musing/decision_gate) from board-kinds-lane-b.tsx; BoardView
 * side-effect-imports board-kinds (which chains the Lane B module) so
 * registration precedes first render.
 */
export interface BoardKindBodyProps {
  slot: BoardSlot;
}

/**
 * Per-kind resolve verbs. Cards don't share one generic verb pair —
 * "收下" on an observation reads differently from "都对 · 收下" on an
 * agent trace. Kinds without labels fall back to the generic pair.
 * Selectors receive the full translations object so labels stay in
 * the i18n system.
 */
export interface BoardKindActionLabels {
  ack?: (t: TranslationsLike) => string;
  dismiss?: (t: TranslationsLike) => string;
}

// Structural alias so this module doesn't import the web i18n type
// into the registry contract; the board section of Translations is
// what selectors actually read.
type TranslationsLike = {
  board: Record<string, string>;
};

/** One-line collapsed-band representation of a slot. */
export interface BoardStripSummary {
  label: string;
  detail?: string;
}

/**
 * Per-kind presentation options consumed by BoardView (the host).
 * All optional — a kind that registers only a Body gets the default
 * ack/dismiss pair and no strip/purpose affordances.
 */
export interface BoardKindOptions {
  /** Per-kind resolve verb labels (defaults to the generic pair). */
  actions?: BoardKindActionLabels;
  /**
   * "Why does this card exist" copy, shown in an InfoPopover on the
   * card — the board explains itself instead of assuming the user
   * reverse-engineers its purpose from numbers.
   */
  purpose?: (t: TranslationsLike) => string;
  /** Collapsed one-line summary; falls back to the slot title. */
  strip?: (slot: BoardSlot, t: TranslationsLike) => BoardStripSummary;
  /**
   * Show the 准/不准 feedback verbs in the live action row. Set on
   * synthesized kinds where the claim can be right or wrong.
   */
  feedback?: boolean;
  /**
   * Suppress the default ack/dismiss verbs — the kind's body renders
   * its own actions (e.g. decision-gate option buttons).
   */
  hideDefaultActions?: boolean;
}

interface BoardKindEntry extends BoardKindOptions {
  Body: ComponentType<BoardKindBodyProps>;
}

const REGISTRY = new Map<string, BoardKindEntry>();

export function registerBoardKind(
  kind: string,
  Body: ComponentType<BoardKindBodyProps>,
  options?: BoardKindOptions,
) {
  REGISTRY.set(kind, { Body, ...options });
}

export function boardKindOptions(kind: string): BoardKindOptions | undefined {
  return REGISTRY.get(kind);
}

export function boardKindPurpose(
  kind: string,
  t: TranslationsLike,
): string | undefined {
  return REGISTRY.get(kind)?.purpose?.(t);
}

export function boardKindStripSummary(
  slot: BoardSlot,
  t: TranslationsLike,
): BoardStripSummary {
  const strip = REGISTRY.get(slot.kind)?.strip;
  if (strip) return strip(slot, t);
  // Fallback: the slot's plain-text title (the same literal-text
  // contract the unknown-kind card body relies on).
  return { label: slot.title };
}

/**
 * Fallback contract (plan-18 §4.2, carried to boards): payload text is
 * plain user-facing strings, so a kind whose renderer hasn't shipped
 * still shows its title + description literally instead of dropping
 * the card.
 */
function FallbackBody({ slot }: BoardKindBodyProps) {
  const description =
    typeof slot.payload?.description === "string"
      ? slot.payload.description
      : undefined;
  return (
    <>
      <BoardKindLabel {...boardKindEyebrow(slot.kind)}>
        {slot.kind}
      </BoardKindLabel>
      <BoardCardFallbackBody title={slot.title} description={description} />
    </>
  );
}

export function renderBoardSlotBody(slot: BoardSlot): ReactNode {
  const Body = REGISTRY.get(slot.kind)?.Body ?? FallbackBody;
  return <Body key={slot.slot_key} slot={slot} />;
}

/** True when the slot's kind has a dedicated renderer (vs the fallback). */
export function hasBoardKindRenderer(kind: string): boolean {
  return REGISTRY.has(kind);
}

/**
 * Generated-at source for a slot's quiet timestamp line: content
 * freshness (`content_updated_at`), not state churn — `updated_at`
 * also moves on resolve, which would lie about when the card was
 * written. Falls back for rows predating migration 021.
 */
export function slotContentTime(slot: BoardSlot): string {
  return slot.content_updated_at ?? slot.updated_at;
}
