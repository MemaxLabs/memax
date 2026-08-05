"use client";

import type { ComponentType, ReactNode } from "react";
import type { BoardSlot } from "memax-sdk";
import { BoardCardFallbackBody, BoardKindLabel } from "@memaxlabs/ui";

/**
 * L3 kind registry (plan 25). Each board kind registers the component
 * that composes its payload from the L1 atoms. Adding a kind is
 * additive: one component entry here + a producer server-side; nothing
 * else in the pipeline changes. Renderers are React components so they
 * can use hooks (useLocale, useRouter) — the registry itself stays a
 * plain map.
 *
 * Lane A kinds (trace/pulse/capsule/week) register from
 * board-kinds.tsx; BoardView side-effect-imports that module so
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

interface BoardKindEntry {
  Body: ComponentType<BoardKindBodyProps>;
  actions?: BoardKindActionLabels;
}

const REGISTRY = new Map<string, BoardKindEntry>();

export function registerBoardKind(
  kind: string,
  Body: ComponentType<BoardKindBodyProps>,
  actions?: BoardKindActionLabels,
) {
  REGISTRY.set(kind, { Body, actions });
}

export function boardKindActionLabels(
  kind: string,
): BoardKindActionLabels | undefined {
  return REGISTRY.get(kind)?.actions;
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
      <BoardKindLabel>{slot.kind}</BoardKindLabel>
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
