"use client";

/**
 * Custom boards (plan 25 P4) — the user-authored half of the pulse
 * surface. A custom board is a standing instruction ("watch for
 * competitor moves") that the nightly dream run answers with cards.
 *
 * Server gap this UI works around: `boards.getForHub` returns the
 * SYSTEM board plus its slots, and there is no per-board slot endpoint
 * yet. So the tab bar lists every board, the system tab renders slots,
 * and a custom tab renders its 酝酿中 state (title + instruction +
 * "明早见"). When the per-board slot endpoint lands, the custom branch
 * swaps its body for the same slot renderer the system board uses.
 */

import { useState } from "react";
import type { Board } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardKindLabel,
  BoardVoiceStar,
  Input,
  Textarea,
} from "@memaxlabs/ui";
import { useInterpolate, useLocale } from "@/i18n";
import { useBoardSlots } from "@/hooks/use-board";
import { renderBoardSlotBody } from "./board-kind-registry";

/**
 * Board titles are optional on the wire (system boards carry none), so
 * every render site funnels through one fallback rule: the system
 * board is always "✦ Pulse", a title-less custom board borrows the
 * same name rather than rendering an empty tab.
 */
export function boardDisplayTitle(board: Board, systemTitle: string): string {
  if (board.kind === "system") return systemTitle;
  return board.title?.trim() || systemTitle;
}

/**
 * BoardTabs — one row of board names. Renders nothing when the hub has
 * only its system board, so single-board hubs keep the plain header.
 */
export function BoardTabs({
  boards,
  activeBoardId,
  onSelect,
}: {
  boards: Board[];
  activeBoardId: string | null;
  onSelect: (boardId: string) => void;
}) {
  const { t } = useLocale();
  if (boards.length < 2) return null;
  return (
    <div
      role="tablist"
      aria-label={t.board.boardsLabel}
      className="-mx-0.5 flex flex-wrap items-center gap-1"
    >
      {boards.map((board) => {
        const active = board.id === activeBoardId;
        return (
          <button
            key={board.id}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onSelect(board.id)}
            className={`cursor-pointer rounded-full px-2.5 py-1 text-[11.5px] font-medium transition-colors [transition-timing-function:var(--ease-spring)] ${
              active
                ? "bg-surface-3 text-fg-1"
                : "text-fg-3 hover:bg-surface-2 hover:text-fg-2"
            }`}
          >
            {boardDisplayTitle(board, t.board.title)}
            {board.status === "cooking" ? (
              <span className="ml-1 text-fg-4">·</span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}

/**
 * BoardComposer — the inline "+ 新建板" form. Compact on purpose: a
 * name and the standing instruction are the whole contract, so a modal
 * would be heavier than the decision it hosts.
 */
export function BoardComposer({
  pending,
  onCancel,
  onCreate,
}: {
  pending: boolean;
  onCancel: () => void;
  onCreate: (input: { title: string; instruction: string }) => void;
}) {
  const { t } = useLocale();
  const [title, setTitle] = useState("");
  const [instruction, setInstruction] = useState("");
  const canSave = title.trim().length > 0 && instruction.trim().length > 0;

  return (
    <form
      className="glass-card flex flex-col gap-2 rounded-[18px] px-4 py-3.5"
      onSubmit={(event) => {
        event.preventDefault();
        if (!canSave || pending) return;
        onCreate({ title: title.trim(), instruction: instruction.trim() });
      }}
    >
      <BoardKindLabel star>{t.board.newBoard}</BoardKindLabel>
      <Input
        value={title}
        onChange={(event) => setTitle(event.target.value)}
        placeholder={t.board.newBoardTitlePlaceholder}
        aria-label={t.board.newBoardTitleLabel}
        autoFocus
      />
      <Textarea
        value={instruction}
        onChange={(event) => setInstruction(event.target.value)}
        placeholder={t.board.newBoardInstructionPlaceholder}
        aria-label={t.board.newBoardInstructionLabel}
        rows={3}
      />
      <BoardActionRow>
        <BoardAction
          emphasis="primary"
          disabled={!canSave || pending}
          onClick={() => {
            if (!canSave || pending) return;
            onCreate({ title: title.trim(), instruction: instruction.trim() });
          }}
        >
          {t.board.newBoardSave}
        </BoardAction>
        <BoardAction emphasis="quiet" className="ml-auto" onClick={onCancel}>
          {t.board.newBoardCancel}
        </BoardAction>
      </BoardActionRow>
    </form>
  );
}

/**
 * CustomBoardView — a custom board's own surface. Once synthesis has
 * written cards they render exactly like the system board's; until
 * then this is the 酝酿中 state: what the board was told to watch,
 * plus when to expect its first answer.
 */
export function CustomBoardView({
  board,
  hubId,
  deletePending,
  onDelete,
}: {
  board: Board;
  hubId: string;
  deletePending: boolean;
  onDelete: (boardId: string) => void;
}) {
  const { t } = useLocale();
  const [confirming, setConfirming] = useState(false);
  const { data } = useBoardSlots(hubId, board.id);
  const slots = data?.slots ?? [];

  const deleteRow = (
    <BoardActionRow>
      {confirming ? (
        <>
          <span className="text-[12.5px] text-fg-3">
            {t.board.deleteBoardConfirm}
          </span>
          <BoardAction
            emphasis="primary"
            disabled={deletePending}
            className="ml-auto"
            onClick={() => onDelete(board.id)}
          >
            {t.board.deleteBoardConfirmYes}
          </BoardAction>
          <BoardAction emphasis="quiet" onClick={() => setConfirming(false)}>
            {t.board.deleteBoardConfirmNo}
          </BoardAction>
        </>
      ) : (
        <BoardAction
          emphasis="quiet"
          className="ml-auto"
          onClick={() => setConfirming(true)}
        >
          {t.board.deleteBoard}
        </BoardAction>
      )}
    </BoardActionRow>
  );

  if (slots.length > 0) {
    // The board earned its cards — show them, with the brief demoted
    // to a caption so the user can still see what they asked for.
    return (
      <div className="flex flex-col gap-2">
        {board.instruction ? (
          <p className="m-0 px-0.5 text-[12px] leading-relaxed text-fg-4">
            {board.instruction}
          </p>
        ) : null}
        {slots.map((slot, index) => (
          <BoardCard
            key={slot.slot_key}
            state={slot.state}
            className="animate-fade-up"
            style={{ animationDelay: `${Math.min(index, 4) * 60}ms` }}
          >
            {renderBoardSlotBody(slot)}
          </BoardCard>
        ))}
        <div className="px-0.5">{deleteRow}</div>
      </div>
    );
  }

  return (
    <BoardCard state="fresh" className="animate-fade-up" live={deleteRow}>
      <BoardKindLabel star>
        {board.status === "cooking"
          ? t.board.cookingLabel
          : board.status === "paused"
            ? t.board.pausedLabel
            : t.board.boardsLabel}
      </BoardKindLabel>
      <p className="m-0 text-[14px] leading-snug text-fg-1">
        {boardDisplayTitle(board, t.board.title)}
      </p>
      {board.instruction ? (
        <p className="m-0 mt-1 text-[12.5px] leading-relaxed text-fg-3">
          {board.instruction}
        </p>
      ) : null}
      <p className="m-0 mt-2 text-[12.5px] text-fg-4">
        {board.status === "cooking"
          ? t.board.cookingBody
          : t.board.customBoardNoSlots}
      </p>
    </BoardCard>
  );
}

/**
 * BoardCookingReceipt — the one-shot confirmation right after a board
 * is created. Names what memax now owns and when the user gets the
 * first result, so "created" doesn't read as "nothing happened".
 */
export function BoardCookingReceipt({ title }: { title: string }) {
  const interpolate = useInterpolate();
  const { t } = useLocale();
  return (
    <p className="m-0 px-0.5 text-[12px] leading-relaxed text-fg-3 animate-fade-up">
      {interpolate(t.board.cookingReceipt, { title })}
    </p>
  );
}

/**
 * NewBoardButton — the "+ 新建板" affordance next to the ✦ Pulse title.
 */
export function NewBoardButton({ onClick }: { onClick: () => void }) {
  const { t } = useLocale();
  return (
    <button
      type="button"
      onClick={onClick}
      className="ml-auto flex cursor-pointer items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium text-fg-3 transition-colors hover:bg-surface-2 hover:text-fg-1"
    >
      <BoardVoiceStar />
      {t.board.newBoard}
    </button>
  );
}
