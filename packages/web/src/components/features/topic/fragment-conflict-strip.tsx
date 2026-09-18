"use client";

import type { Memory } from "memax-sdk";
import { useLocale, useInterpolate } from "@/i18n";
import { formatAge } from "@/lib/format-age";
import { sanitizeSummary } from "@/lib/sanitize-metadata";
import {
  fragmentConflictAction,
  type FragmentConflict,
  type FragmentConflictVerdict,
} from "@/lib/fragment-conflicts";
import { useResolveNotification } from "@/hooks/use-notifications";
import { queryClient } from "@/lib/query-client";

/**
 * FragmentConflictStrip — a pending contradiction, rendered under the
 * memory that is party to it. Always present (not hover-gated) until a
 * verdict lands; the verdict resolves the same notification the pulse
 * 「等你」 deck shows, so both surfaces clear together. Amber only on
 * the eyebrow band: this is "something for you to decide", not an alarm.
 *
 * The payload carries only id + title for the other memory; when that
 * memory happens to be in the same list we show its summary too.
 */
export function FragmentConflictStrip({
  memory: m,
  conflict,
  otherMemory,
  onOpenOther,
  variant = "rows",
}: {
  memory: Memory;
  conflict: FragmentConflict;
  otherMemory?: Memory;
  onOpenOther: (id: string) => void;
  /** "rows" indents under the row's leading column; "grid" spans a card grid row. */
  variant?: "rows" | "grid";
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const resolve = useResolveNotification();
  const pending = resolve.isPending;

  const verdict = (v: FragmentConflictVerdict) =>
    resolve.mutate(
      {
        id: conflict.notificationId,
        action: fragmentConflictAction(conflict, v),
      },
      {
        onSuccess: () => {
          // keep_a / keep_b archive the loser server-side without a
          // memory event; the loser may be on screen, so refetch lists.
          if (v !== "keep_both") {
            void queryClient.invalidateQueries({
              queryKey: ["recent-memories"],
            });
            void queryClient.invalidateQueries({ queryKey: ["memories"] });
          }
        },
      },
    );

  const thisSnippet = sanitizeSummary(m.summary) || m.content || "";
  const otherSnippet = otherMemory
    ? sanitizeSummary(otherMemory.summary) || otherMemory.content || ""
    : "";
  const buttonClass =
    "rounded-full border border-border/50 px-3 py-1 text-[12px] text-fg-2 transition-colors cursor-pointer hover:bg-surface-2 hover:text-fg-1 disabled:cursor-wait disabled:opacity-60";

  return (
    <div
      className={`mb-3 overflow-hidden rounded-xl border border-border/50 bg-card ${
        variant === "grid" ? "" : "mx-3 max-w-[600px] sm:ml-12"
      }`}
      role="group"
      aria-label={t.memoryView.conflictEyebrow}
    >
      <div
        className="flex flex-wrap items-center gap-x-2 gap-y-0.5 px-3 py-1.5 text-[12px] text-fg-2"
        style={{
          background:
            "oklch(from var(--warning, oklch(0.72 0.14 70)) l c h / 0.12)",
        }}
      >
        <span
          className="font-mono text-[9.5px] font-medium uppercase tracking-[0.12em]"
          style={{ color: "var(--warning, oklch(0.62 0.14 70))" }}
        >
          {t.memoryView.conflictEyebrow}
        </span>
        <span>
          {conflict.reason ||
            interpolate(t.memoryView.conflictWith, {
              title: conflict.other.title,
            })}
        </span>
        <span className="ml-auto text-[11px] text-fg-3">
          {formatAge(conflict.createdAt, t, interpolate)}
        </span>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2">
        <div className="px-3 py-2 text-[12.5px] leading-relaxed text-fg-2">
          <div className="mb-0.5 text-[11px] text-fg-3">
            {t.memoryView.conflictThis} ·{" "}
            {formatAge(m.created_at, t, interpolate)}
          </div>
          <div className="font-medium text-fg-1">{m.title}</div>
          {thisSnippet ? (
            <div className="line-clamp-3">{thisSnippet}</div>
          ) : null}
        </div>
        <button
          type="button"
          onClick={() => onOpenOther(conflict.other.id)}
          className="border-t border-border/40 px-3 py-2 text-left text-[12.5px] leading-relaxed text-fg-2 transition-colors cursor-pointer hover:bg-surface-1 sm:border-l sm:border-t-0"
        >
          <div className="mb-0.5 text-[11px] text-fg-3">
            {t.memoryView.conflictOther}
            {otherMemory
              ? ` · ${formatAge(otherMemory.created_at, t, interpolate)}`
              : ""}
          </div>
          <div className="font-medium text-fg-1">
            {otherMemory?.title || conflict.other.title}
          </div>
          {otherSnippet ? (
            <div className="line-clamp-3">{otherSnippet}</div>
          ) : (
            <div className="text-fg-3">{t.memoryView.conflictOtherOpen}</div>
          )}
        </button>
      </div>
      <div className="flex flex-wrap items-center gap-1.5 border-t border-border/40 px-3 py-2">
        <button
          type="button"
          disabled={pending}
          onClick={() => verdict("keep_this")}
          className="rounded-full bg-foreground px-3 py-1 text-[12px] font-medium text-background transition-opacity cursor-pointer hover:opacity-85 disabled:cursor-wait disabled:opacity-60"
        >
          {t.memoryView.conflictKeepThis}
        </button>
        <button
          type="button"
          disabled={pending}
          onClick={() => verdict("keep_other")}
          className={buttonClass}
        >
          {t.memoryView.conflictKeepOther}
        </button>
        <button
          type="button"
          disabled={pending}
          onClick={() => verdict("keep_both")}
          className={buttonClass}
        >
          {t.memoryView.conflictKeepBoth}
        </button>
        <span className="ml-auto text-[11px] text-fg-3">
          {t.memoryView.conflictNote}
        </span>
      </div>
    </div>
  );
}
