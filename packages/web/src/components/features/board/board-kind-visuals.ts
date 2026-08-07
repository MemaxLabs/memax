/**
 * Board kind visual system (founder direction, 2026-08): ONE mapping
 * table from kind → accent dot color + lucide icon + shelf tile size.
 * Applied to tile eyebrows AND expanded card kind labels, so a kind
 * reads the same wherever it surfaces. Subtle by contract — dot+icon
 * at eyebrow scale, never loud backgrounds.
 *
 * Colors reuse the existing oklch palette:
 *   - signature violet (`var(--signature)`) for every first-person ✦
 *     kind — memax's own voice is always violet;
 *   - one hue each for the non-voice categories (capsule / activity /
 *     decision / highlight), drawn from the agent-identity palette in
 *     @memaxlabs/ui tokens so no new hues enter the system.
 *
 * Tile size follows the card's PURPOSE, not uniformity:
 *   - wide (~320px)     — decisions (等你 deck, decision gates): the
 *                         only tiles blocked on the user, so they get
 *                         the most room;
 *   - standard (~272px) — the synthesized intelligence (梦记 / wow
 *                         rotation): a sentence of insight;
 *   - square (~200px)   — a single artifact (capsule quote, a member
 *                         joined): compact, almost square;
 *   - slim (~180px)     — counters (activity): numbers need the least.
 * Height stays on one shared vertical rhythm; only content varies it.
 */

import type { LucideIcon } from "lucide-react";
import {
  ListChecks,
  Activity,
  Clock,
  Coffee,
  HelpCircle,
  Hourglass,
  Link2,
  MessagesSquare,
  Moon,
  Repeat,
  Signpost,
  Sparkles,
  Split,
  UserPlus,
  Waves,
} from "lucide-react";

export type BoardTileSize = "wide" | "standard" | "square" | "slim";

export interface BoardKindVisual {
  /** Accent dot color for the eyebrow (oklch or CSS var). */
  dot: string;
  /** Lucide icon rendered at eyebrow scale next to the dot. */
  icon: LucideIcon;
  /** Shelf tile size variant. */
  tile: BoardTileSize;
}

/** Signature violet — every first-person ✦ kind. */
const VOICE = "var(--signature)";
/** Decision hue — warm coral (the claude-family agent hue). */
const DECISION = "oklch(0.65 0.15 30)";
/** Capsule hue — gold (a year-old quote, warm). */
const CAPSULE = "oklch(0.70 0.13 85)";
/** Activity hue — soft indigo (neutral counters). */
const ACTIVITY = "oklch(0.65 0.10 260)";
/** Highlight hue — teal (news: a member joined). */
const HIGHLIGHT = "oklch(0.65 0.15 145)";
/**
 * Team hue — cyan. The team-native kinds (共识缺口 / 团队回声 /
 * 谁知道这个) are the only synthesized cards that are about PEOPLE
 * rather than about the reader, so they step off signature violet:
 * scanning a shared board, the cyan dots are the ones that exist only
 * because several people write here.
 */
const TEAM = "oklch(0.68 0.13 205)";

/**
 * Pseudo-kinds for board entries that aren't slots: the 等你
 * notification deck, member-joined highlights, and cooking boards.
 */
export const WAITING_KIND = "waiting";
export const HIGHLIGHT_KIND = "highlight";
export const COOKING_KIND = "cooking";

export const BOARD_KIND_VISUALS: Record<string, BoardKindVisual> = {
  // First-person ✦ kinds — signature violet, standard tiles.
  dreamlog: { dot: VOICE, icon: Moon, tile: "standard" },
  echo: { dot: VOICE, icon: Waves, tile: "standard" },
  thread: { dot: VOICE, icon: Link2, tile: "standard" },
  pattern: { dot: VOICE, icon: Repeat, tile: "standard" },
  // 接下来 — memax's voice, but it asks for action, so it earns the
  // wide tile alongside decisions rather than the standard read-only
  // width.
  nextup: { dot: VOICE, icon: ListChecks, tile: "wide" },
  // Team-native kinds — cyan, team hubs only. Standard tiles: each one
  // is a sentence of intelligence plus its receipts, same as the wow
  // rotation it shares a slot with.
  consensus_gap: { dot: TEAM, icon: Split, tile: "standard" },
  team_echo: { dot: TEAM, icon: MessagesSquare, tile: "standard" },
  who_knows: { dot: TEAM, icon: Signpost, tile: "standard" },
  // Decisions — widest tiles: the user is the blocker.
  decision_gate: { dot: DECISION, icon: Hourglass, tile: "wide" },
  [WAITING_KIND]: { dot: DECISION, icon: Hourglass, tile: "wide" },
  // Single-artifact kinds — square-ish.
  [HIGHLIGHT_KIND]: { dot: HIGHLIGHT, icon: UserPlus, tile: "square" },
  // Counters — slim.
  activity: { dot: ACTIVITY, icon: Activity, tile: "slim" },
  // Cooking boards — memax's own promise ("明早见"), violet.
  [COOKING_KIND]: { dot: VOICE, icon: Coffee, tile: "standard" },
  // Retired kinds (capsule / openq / musing). Entries stay so boards
  // still holding one of their slots render coherently until the next
  // producer pass clears it.
  capsule: { dot: CAPSULE, icon: Clock, tile: "square" },
  openq: { dot: VOICE, icon: HelpCircle, tile: "standard" },
  musing: { dot: VOICE, icon: Sparkles, tile: "standard" },
};

/** Unknown / future kinds lead with the voice treatment. */
const FALLBACK_VISUAL: BoardKindVisual = {
  dot: VOICE,
  icon: Sparkles,
  tile: "standard",
};

export function boardKindVisual(kind: string): BoardKindVisual {
  return BOARD_KIND_VISUALS[kind] ?? FALLBACK_VISUAL;
}

/**
 * Spread-ready eyebrow props for BoardKindLabel — dot + icon in one
 * call: `<BoardKindLabel star {...boardKindEyebrow(slot.kind)}>`.
 */
export function boardKindEyebrow(kind: string): {
  dotColor: string;
  icon: LucideIcon;
} {
  const visual = boardKindVisual(kind);
  return { dotColor: visual.dot, icon: visual.icon };
}
