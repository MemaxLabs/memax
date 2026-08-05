"use client";

import type { ReactNode } from "react";
import type { BoardSlot } from "memax-sdk";
import { BoardCardFallbackBody, BoardKindLabel } from "@memaxlabs/ui";
import type { Translations } from "@/i18n/locales/en";

/**
 * L3 kind registry (plan 25). Each board kind registers how its
 * payload composes the L1 atoms into a card body. Adding a kind is
 * additive: one renderer entry here + a producer server-side; nothing
 * else in the pipeline changes.
 *
 * P0 ships the registry mechanism and the unknown-kind fallback only —
 * concrete kinds (行迹, 回声, 等你…) land with their Lane A/B
 * producers in P1/P2, each with a kitchen specimen.
 */
export interface BoardKindRenderer {
  /**
   * The persistent card body (kind label + atoms). Live actions are
   * composed by BoardView from the shared action row, not here.
   */
  body: (slot: BoardSlot, t: Translations) => ReactNode;
}

const REGISTRY = new Map<string, BoardKindRenderer>();

export function registerBoardKind(kind: string, renderer: BoardKindRenderer) {
  REGISTRY.set(kind, renderer);
}

/**
 * Fallback contract (plan-18 §4.2, carried to boards): payload text is
 * plain user-facing strings, so a kind whose renderer hasn't shipped
 * still shows its title + description literally instead of dropping
 * the card.
 */
function fallbackBody(slot: BoardSlot): ReactNode {
  const description =
    typeof slot.payload?.description === "string"
      ? slot.payload.description
      : undefined;
  return (
    <>
      <BoardKindLabel>{slot.kind}</BoardKindLabel>
      <BoardCardFallbackBody title={slot.title} description={description} />
    </>
  );
}

export function renderBoardSlotBody(
  slot: BoardSlot,
  t: Translations,
): ReactNode {
  const renderer = REGISTRY.get(slot.kind);
  if (renderer) return renderer.body(slot, t);
  return fallbackBody(slot);
}

/** True when the slot's kind has a dedicated renderer (vs the fallback). */
export function hasBoardKindRenderer(kind: string): boolean {
  return REGISTRY.has(kind);
}
