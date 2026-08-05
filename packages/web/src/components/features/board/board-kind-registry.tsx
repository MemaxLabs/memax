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

const REGISTRY = new Map<string, ComponentType<BoardKindBodyProps>>();

export function registerBoardKind(
  kind: string,
  Body: ComponentType<BoardKindBodyProps>,
) {
  REGISTRY.set(kind, Body);
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
  const Body = REGISTRY.get(slot.kind) ?? FallbackBody;
  return <Body key={slot.slot_key} slot={slot} />;
}

/** True when the slot's kind has a dedicated renderer (vs the fallback). */
export function hasBoardKindRenderer(kind: string): boolean {
  return REGISTRY.has(kind);
}
