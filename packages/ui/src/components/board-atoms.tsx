"use client";

import * as React from "react";
import { cn } from "../utils";

/**
 * Board atoms — the L1 vocabulary of the pulse board (plan 25).
 *
 * Every board card, whatever its kind, is assembled from these eight
 * pieces; a kind that renders with anything else is a design smell.
 * They are deliberately prop-driven and string-agnostic (no i18n
 * hooks) so the kitchen renders the real components and the web app
 * supplies localized copy.
 *
 * Visual spec: the northstar demo (pulse-board-design) and kitchen
 * section 44, "Pulse board".
 */

/**
 * VoiceStar — the signature ✦ that marks first-person memax voice.
 * Only kinds where memax speaks as itself (梦记, 回声, 等你…) get the
 * star; observational kinds (行迹, 项目脉搏) stay starless. That
 * scarcity is the design contract — don't sprinkle it.
 */
export function BoardVoiceStar({
  breathing = false,
  className,
}: {
  /** Slow opacity breathe for live/attention states. */
  breathing?: boolean;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "mr-0.5 inline-block",
        breathing && "state-slow-breathe",
        className,
      )}
      style={{ color: "var(--signature)" }}
      aria-hidden="true"
    >
      ✦
    </span>
  );
}

/**
 * KindLabel — the uppercase micro-eyebrow naming what kind of card
 * this is ("行迹 · 你不在的 9 小时"). Optional colored dot for
 * category-coded kinds, optional VoiceStar for first-person kinds,
 * optional kind icon (lucide, rendered at eyebrow scale — subtle,
 * never a loud background).
 */
export function BoardKindLabel({
  dotColor,
  icon: Icon,
  star = false,
  children,
  className,
}: {
  dotColor?: string;
  /** Kind icon component (lucide). Rendered dot-adjacent at 12px. */
  icon?: React.ComponentType<{ className?: string; "aria-hidden"?: boolean }>;
  star?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "mb-2 flex items-center gap-1.5 text-left text-[10.5px] uppercase tracking-[0.12em] text-fg-3",
        className,
      )}
    >
      {dotColor ? (
        <span
          className="h-1.5 w-1.5 shrink-0 rounded-full"
          style={{ backgroundColor: dotColor }}
          aria-hidden="true"
        />
      ) : null}
      {Icon ? (
        <Icon className="h-3 w-3 shrink-0 opacity-80" aria-hidden />
      ) : null}
      {star ? <BoardVoiceStar /> : null}
      <span className="min-w-0 truncate">{children}</span>
    </div>
  );
}

/**
 * MemQuote — a memory quoted verbatim inside a card, with its date
 * eyebrow. The card's claim is only as strong as what it can quote;
 * this atom is how cards show their receipts inline.
 */
export function BoardMemQuote({
  when,
  children,
  onClick,
  className,
}: {
  /** Date / provenance eyebrow, e.g. "6 月 2 日 · 你问自己". */
  when?: string;
  children: React.ReactNode;
  onClick?: () => void;
  className?: string;
}) {
  const body = (
    <>
      {when ? (
        <span className="mb-0.5 block text-[10.5px] text-fg-4">{when}</span>
      ) : null}
      <span className="text-fg-2">{children}</span>
    </>
  );
  const base =
    "block w-full rounded-xl border border-border/40 px-3 py-2 text-left text-[12.5px]";
  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        className={cn(base, "transition-colors hover:bg-surface-1", className)}
      >
        {body}
      </button>
    );
  }
  return <div className={cn(base, className)}>{body}</div>;
}

/**
 * CiteChip — a compact reference to a cited memory. Tapping opens the
 * memory (or peeks it inline — behavior belongs to the caller). Cards
 * without cite chips or quotes should not exist: no receipts, no card.
 */
export function BoardCiteChip({
  dotColor,
  label,
  onClick,
  active = false,
  className,
}: {
  dotColor?: string;
  label: string;
  onClick?: () => void;
  active?: boolean;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex max-w-full items-center gap-1.5 rounded-[7px] bg-surface-1 px-2 py-1 text-[11px] text-fg-2 transition-colors hover:text-fg-1",
        active && "text-fg-1",
        className,
      )}
    >
      {dotColor ? (
        <span
          className="h-[5px] w-[5px] shrink-0 rounded-full"
          style={{ backgroundColor: dotColor }}
          aria-hidden="true"
        />
      ) : null}
      <span className="min-w-0 truncate">{label}</span>
    </button>
  );
}

/**
 * AgentRow — one agent/source activity line inside 行迹-style cards:
 * colored source dot, bold what, muted detail, right-aligned who/when.
 */
export function BoardAgentRow({
  dotColor,
  title,
  meta,
  who,
  className,
}: {
  dotColor?: string;
  title: React.ReactNode;
  meta?: React.ReactNode;
  who?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex items-start gap-2.5 border-t border-border/40 py-2 first:border-t-0 first:pt-0.5",
        className,
      )}
    >
      <span
        className="mt-1.5 h-[7px] w-[7px] shrink-0 rounded-full"
        style={{ backgroundColor: dotColor ?? "var(--signature)" }}
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="text-[13.5px] font-semibold text-fg-1">{title}</div>
        {meta ? <div className="text-[11.5px] text-fg-3">{meta}</div> : null}
      </div>
      {who ? (
        <span className="whitespace-nowrap pt-0.5 text-[11px] text-fg-4">
          {who}
        </span>
      ) : null}
    </div>
  );
}

/**
 * ActionRow + Action — the card's resolution verbs. Text-weight only:
 * actions never compete with content. `primary` is the suggested verb,
 * `quiet` is the low-stakes acknowledge.
 */
export function BoardActionRow({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("mt-3 flex items-center gap-3.5", className)}>
      {children}
    </div>
  );
}

export function BoardAction({
  emphasis = "default",
  onClick,
  disabled = false,
  children,
  className,
}: {
  emphasis?: "primary" | "default" | "quiet";
  onClick?: () => void;
  disabled?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "text-[12.5px] font-semibold transition-colors disabled:cursor-default disabled:text-fg-4",
        emphasis === "primary" && "text-fg-1",
        emphasis === "default" && "text-fg-2 hover:text-fg-1",
        emphasis === "quiet" && "font-medium text-fg-3 hover:text-fg-2",
        className,
      )}
    >
      {children}
    </button>
  );
}

/**
 * SlotStrip — the collapsed one-line representation of a lower-band
 * slot ("项目脉搏 · 3 个项目有动静"). Tapping expands the stack the
 * caller renders. Keeps the board scannable: only hero slots open by
 * default.
 */
export function BoardSlotStrip({
  label,
  detail,
  open = false,
  onToggle,
  className,
}: {
  label: React.ReactNode;
  detail?: React.ReactNode;
  open?: boolean;
  onToggle?: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={open}
      className={cn(
        "flex w-full items-center justify-between rounded-[14px] border border-border/40 px-4 py-2.5 text-left transition-colors hover:bg-surface-1",
        className,
      )}
    >
      <span className="text-[12.5px] font-semibold text-fg-2">{label}</span>
      {detail ? (
        <span className="text-[12.5px] text-fg-3">{detail}</span>
      ) : null}
    </button>
  );
}

/**
 * Receipt — the terminal state line: what happened and when the card
 * was resolved ("✓ 已收下 · 今天 7:41"). Cards never disappear on
 * resolve; they become receipts until their slot is replaced.
 */
export function BoardReceipt({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex items-baseline gap-1.5 text-[12.5px] text-fg-3",
        className,
      )}
    >
      <span className="text-emerald-500/80" aria-hidden="true">
        ✓
      </span>
      <span className="min-w-0">{children}</span>
    </div>
  );
}
