"use client";

/**
 * Custom boards (plan 25 P4) — the user-authored half of the pulse
 * surface. A custom board is a standing instruction ("watch for
 * competitor moves") that the nightly dream run answers with cards.
 *
 * Founder direction (2026-08): custom boards are NOT separate tabs or
 * sections. One unified card stream — custom-board cards merge into
 * the main flow after the system board's, each carrying a small
 * board-title tag (BoardTitleTag) that doubles as the delete
 * affordance. Cooking boards render inline as one compact cooking
 * card each. The old BoardTabs / CustomBoardView surfaces are gone.
 */

import { useState } from "react";
import type { Board, BoardSlot } from "memax-sdk";
import {
  BoardAction,
  BoardActionRow,
  BoardCard,
  BoardKindLabel,
  BoardVoiceStar,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Textarea,
} from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
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
 * BoardTitleTag — the quiet pill naming which custom board a card came
 * from, appended to the card's kind-label row. Tapping it opens a
 * small popover with the board's standing instruction and the delete
 * flow (删除 → confirm), so board management lives ON the cards now
 * that boards have no surface of their own.
 */
export function BoardTitleTag({
  board,
  deletePending,
  onDelete,
}: {
  board: Board;
  deletePending: boolean;
  onDelete: (boardId: string) => void;
}) {
  const { t } = useLocale();
  const [confirming, setConfirming] = useState(false);
  return (
    <Popover>
      <PopoverTrigger className="shrink-0 cursor-pointer rounded-full bg-surface-2 px-2 py-0.5 text-[10.5px] font-medium text-fg-2 transition-colors hover:bg-surface-3 hover:text-fg-1">
        {boardDisplayTitle(board, t.board.title)}
      </PopoverTrigger>
      <PopoverContent side="bottom" align="end" sideOffset={4}>
        <div className="flex max-w-64 flex-col gap-2">
          {board.instruction ? (
            <p className="m-0 text-[12px] leading-relaxed text-fg-3">
              {board.instruction}
            </p>
          ) : null}
          {confirming ? (
            <>
              <p className="m-0 text-[12px] text-fg-2">
                {t.board.deleteBoardConfirm}
              </p>
              <div className="flex items-center gap-2">
                <BoardAction
                  emphasis="primary"
                  disabled={deletePending}
                  onClick={() => onDelete(board.id)}
                >
                  {t.board.deleteBoardConfirmYes}
                </BoardAction>
                <BoardAction
                  emphasis="quiet"
                  onClick={() => setConfirming(false)}
                >
                  {t.board.deleteBoardConfirmNo}
                </BoardAction>
              </div>
            </>
          ) : (
            <BoardAction emphasis="quiet" onClick={() => setConfirming(true)}>
              {t.board.deleteBoard}
            </BoardAction>
          )}
        </div>
      </PopoverContent>
    </Popover>
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
  initialTitle = "",
  initialInstruction = "",
}: {
  pending: boolean;
  onCancel: () => void;
  onCreate: (input: { title: string; instruction: string }) => void;
  /**
   * Prefill (empty-state example chips). Read once on mount — the
   * caller re-keys the composer when the prefill changes.
   */
  initialTitle?: string;
  initialInstruction?: string;
}) {
  const { t } = useLocale();
  const [title, setTitle] = useState(initialTitle);
  const [instruction, setInstruction] = useState(initialInstruction);
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
 * CustomBoardSlotCard — one custom-board card in the unified stream.
 * Renders exactly like a system slot (same kind renderers), plus the
 * board-title tag floating in the label row's trailing corner — the
 * "未观察模式 · [健身 & 睡眠]" read.
 */
export function CustomBoardSlotCard({
  board,
  slot,
  entranceIndex,
  deletePending,
  onDelete,
}: {
  board: Board;
  slot: BoardSlot;
  entranceIndex: number;
  deletePending: boolean;
  onDelete: (boardId: string) => void;
}) {
  return (
    <BoardCard
      state={slot.state}
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
    >
      {/* float-right BEFORE the body — same trick the slot purpose
          InfoPopover uses — so the tag sits on the kind-label line. */}
      <div className="float-right ml-2">
        <BoardTitleTag
          board={board}
          deletePending={deletePending}
          onDelete={onDelete}
        />
      </div>
      {renderBoardSlotBody(slot)}
    </BoardCard>
  );
}

/**
 * CookingBoardCard — a custom board that hasn't produced cards yet,
 * inline in the same stream as everything else (no separate view).
 * Carries the delete flow so a mis-created board can be unmade from
 * where it's visible.
 */
export function CookingBoardCard({
  board,
  entranceIndex,
  deletePending,
  onDelete,
}: {
  board: Board;
  entranceIndex: number;
  deletePending: boolean;
  onDelete: (boardId: string) => void;
}) {
  const { t } = useLocale();
  const [confirming, setConfirming] = useState(false);
  return (
    <BoardCard
      state="fresh"
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      live={
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
              <BoardAction
                emphasis="quiet"
                onClick={() => setConfirming(false)}
              >
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
      }
    >
      <BoardKindLabel star>{t.board.cookingLabel}</BoardKindLabel>
      <p className="m-0 text-[14px] leading-snug text-fg-1">
        {boardDisplayTitle(board, t.board.title)}
      </p>
      {board.instruction ? (
        <p className="m-0 mt-1 text-[12.5px] leading-relaxed text-fg-3">
          {board.instruction}
        </p>
      ) : null}
      <p className="m-0 mt-2 text-[12.5px] text-fg-4">{t.board.cookingBody}</p>
    </BoardCard>
  );
}

/**
 * BoardEmptyState — the /pulse page when the board has no live cards.
 * A designed moment, not a shrug: a ✦-led pitch of what will appear
 * here, then the "让 memax 替你盯一件事" block — three example chips
 * that open the composer PRE-FILLED with a full standing instruction
 * the user can edit before saving.
 */
export function BoardEmptyState({
  onPickExample,
}: {
  onPickExample: (example: { title: string; instruction: string }) => void;
}) {
  const { t } = useLocale();
  const examples = [
    {
      title: t.board.emptyExampleFitness,
      instruction: t.board.emptyExampleFitnessInstruction,
    },
    {
      title: t.board.emptyExampleProject,
      instruction: t.board.emptyExampleProjectInstruction,
    },
    {
      title: t.board.emptyExampleStudy,
      instruction: t.board.emptyExampleStudyInstruction,
    },
  ];
  return (
    <div className="glass-card animate-fade-up flex flex-col items-center gap-6 rounded-[18px] px-5 py-8 text-center">
      <div className="max-w-md">
        <p className="m-0 text-[14.5px] font-medium text-fg-1">
          <BoardVoiceStar /> {t.board.pageEmptyTitle}
        </p>
        <p className="m-0 mt-2 text-[13px] leading-relaxed text-fg-3">
          {t.board.pageEmptyBody}
        </p>
      </div>
      <div className="w-full max-w-md">
        <p className="m-0 text-[13.5px] font-semibold text-fg-2">
          {t.board.emptyWatchTitle}
        </p>
        <div className="mt-3 flex flex-wrap justify-center gap-2">
          {examples.map((example) => (
            <button
              key={example.title}
              type="button"
              onClick={() => onPickExample(example)}
              className="glass-subtle cursor-pointer rounded-full px-3.5 py-1.5 text-[12.5px] font-medium text-fg-2 transition-colors [transition-timing-function:var(--ease-spring)] hover:text-fg-1"
            >
              {example.title}
            </button>
          ))}
        </div>
      </div>
    </div>
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
      // Trailing-action idiom of the section header it sits in (the
      // fresh-memories convention): 12px fg-3 → fg-2 text button. The
      // header's flex-1 spacer right-aligns it — no ml-auto needed.
      className="flex cursor-pointer items-center gap-1 text-[12px] text-fg-3 transition-colors hover:text-fg-2"
    >
      <BoardVoiceStar />
      {t.board.newBoard}
    </button>
  );
}
