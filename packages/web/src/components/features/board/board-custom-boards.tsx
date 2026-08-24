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

import { useEffect, useState, type ReactNode } from "react";
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
import { useInterpolate, useLocale } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { renderBoardSlotBody, slotContentTime } from "./board-kind-registry";
import { boardKindEyebrow, COOKING_KIND } from "./board-kind-visuals";

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
  onEmptyBlur,
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
  /**
   * Ghost-morph contract: focus left the composer with BOTH fields
   * empty → the host collapses back to its ghost form. Never fires
   * with content on board — an unfinished thought is kept, not eaten.
   */
  onEmptyBlur?: () => void;
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
      // Esc anywhere in the form backs out (the ghost re-forms).
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          onCancel();
        }
      }}
      onBlur={(event) => {
        if (!onEmptyBlur) return;
        const next = event.relatedTarget as Node | null;
        if (next && event.currentTarget.contains(next)) return;
        if (title.trim() === "" && instruction.trim() === "") onEmptyBlur();
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
  deckControls,
}: {
  board: Board;
  slot: BoardSlot;
  entranceIndex: number;
  deletePending: boolean;
  onDelete: (boardId: string) => void;
  /** Same-kind stack pill + ↻ cycle (BoardDeckControls) when decked. */
  deckControls?: ReactNode;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  return (
    <BoardCard
      state={slot.state}
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      timestamp={formatAge(slotContentTime(slot), t, interpolate)}
    >
      {/* float-right BEFORE the body — same trick the slot purpose
          InfoPopover uses — so the tag sits on the kind-label line. */}
      <div className="float-right ml-2 flex items-center gap-1.5">
        {deckControls}
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
  const interpolate = useInterpolate();
  const [confirming, setConfirming] = useState(false);
  return (
    <BoardCard
      state="fresh"
      className="animate-fade-up"
      style={{ animationDelay: `${Math.min(entranceIndex, 4) * 60}ms` }}
      timestamp={formatAge(board.created_at, t, interpolate)}
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
      <BoardKindLabel star {...boardKindEyebrow(COOKING_KIND)}>
        {t.board.cookingLabel}
      </BoardKindLabel>
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
 * BoardGhostCard — the latent new-board affordance closing the /pulse
 * card stream (2026-08 founder spec, replacing the header 新建板
 * button): the same latent-creation metaphor as the memory grid's
 * dashed ComposePlusCell. At rest it is a dashed glass card with a
 * quiet ✦ and placeholder copy; tapping morphs it IN PLACE into the
 * composer (no modal, no jump — one container, dashed→solid, spring).
 * Save → the new board's cooking card takes this spot on the next
 * boards refetch and the ghost re-forms behind it. Esc or leaving the
 * fields empty morphs it back to a ghost.
 */
export function BoardGhostCard({
  pending,
  onCreate,
  prefill,
  onPrefillConsumed,
}: {
  pending: boolean;
  onCreate: (input: { title: string; instruction: string }) => void;
  /**
   * Empty-state example chip payload — arriving prefill snaps the
   * ghost open with the example loaded, ready to edit.
   */
  prefill?: { title: string; instruction: string } | null;
  /** Prefill landed (or was abandoned) — parent clears its state. */
  onPrefillConsumed: () => void;
}) {
  const { t } = useLocale();
  const [composing, setComposing] = useState(false);
  // A chip tap composes immediately, with the example loaded.
  useEffect(() => {
    if (prefill) setComposing(true);
  }, [prefill]);
  const close = () => {
    setComposing(false);
    onPrefillConsumed();
  };

  if (!composing) {
    return (
      <button
        type="button"
        onClick={() => setComposing(true)}
        // The ComposePlusCell "fill me in" idiom: dashed glass border,
        // transparent body — a latent card, not a filled one.
        className="animate-fade-up flex w-full cursor-pointer flex-col items-center justify-center gap-1.5 rounded-[18px] bg-transparent px-4 py-6 transition-colors [transition-timing-function:var(--ease-spring)] hover:bg-surface-1"
        style={{ border: "1px dashed var(--glass-border)" }}
      >
        <BoardVoiceStar className="text-[15px]" />
        <span className="text-[12.5px] text-fg-4">{t.board.ghostTitle}</span>
      </button>
    );
  }

  return (
    <div className="animate-fade-up">
      <BoardComposer
        // Re-key on prefill so a chip tap always lands its copy even
        // if the composer was already open.
        key={prefill ? prefill.title : "blank"}
        pending={pending}
        initialTitle={prefill?.title}
        initialInstruction={prefill?.instruction}
        onCancel={close}
        onEmptyBlur={close}
        onCreate={onCreate}
      />
    </div>
  );
}
