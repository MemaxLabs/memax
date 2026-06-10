"use client";

import { AISummary } from "@/components/features/ai-summary";
import { useLocale, useInterpolate } from "@/i18n";
import { RecallingText } from "@/components/features/recalling-text";
import { Kbd } from "./kbd";
import { StarGlyph } from "./star-glyph";

// Stable no-op fallback so AISummary's memoized children don't invalidate
// when the parent doesn't pass an onSourceClick handler. Declared at module
// scope (not inline) to keep identity stable across renders.
const noop = (_: string): void => {};

export type SynthesisState =
  | "placeholder"
  | "thinking"
  | "streaming"
  | "complete"
  | "free"
  | "error";

export function SynthesisSlot({
  state,
  text,
  sources = [],
  query,
  onRetry,
  onUpgrade,
  onCopy,
  onCopyForAI,
  onSourceClick,
}: {
  state: SynthesisState;
  text?: string;
  sources?: Array<{ id: string }>;
  query?: string;
  onRetry?: () => void;
  onUpgrade?: () => void;
  onCopy?: () => void;
  onCopyForAI?: () => void;
  onSourceClick?: (id: string) => void;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  if (state === "placeholder") {
    return (
      <div className="flex items-center gap-2 px-5 py-3.5 text-[13px] text-fg-3 sm:px-6">
        <StarGlyph />
        <span>
          {t.bar.ai.placeholderBefore} <Kbd>↵</Kbd> {t.bar.ai.placeholderAfter}
        </span>
      </div>
    );
  }

  if (state === "free") {
    return (
      <div
        className="flex items-center gap-2 px-5 py-3.5 text-[13px] text-fg-3 sm:px-6"
        style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
      >
        <StarGlyph />
        <span className="flex-1">{t.bar.ai.freeStub}</span>
        <button
          type="button"
          onClick={onUpgrade}
          className="cursor-pointer font-medium text-fg-1 underline underline-offset-2"
        >
          {t.billing.upgrade}
        </button>
      </div>
    );
  }

  if (state === "error") {
    return (
      <div className="flex items-center gap-2 px-5 py-3.5 text-[13px] text-fg-3 sm:px-6">
        <span
          aria-hidden
          className="inline-flex h-[13px] w-[13px] shrink-0 items-center justify-center leading-none text-fg-3"
        >
          ⚠
        </span>
        <span className="flex-1">{t.bar.ai.error}</span>
        <button
          type="button"
          onClick={onRetry}
          className="cursor-pointer text-[12px] text-fg-2 underline underline-offset-2 hover:text-fg-1"
        >
          {t.common.retry}
        </button>
      </div>
    );
  }

  if (state === "thinking") {
    return (
      <div
        className="px-5 py-3.5 sm:px-6"
        style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
      >
        <div className="flex items-start gap-2">
          <div className="flex h-6 shrink-0 items-center">
            <StarGlyph breathe />
          </div>
          <div className="flex-1">
            <div className="flex h-6 items-center text-[14px] leading-relaxed text-fg-3">
              <RecallingText variant="ai" />
            </div>
            {query ? (
              <div className="mt-1 text-[12px] leading-relaxed text-fg-4">
                {interpolate(t.bar.ai.thinking, { query })}
              </div>
            ) : null}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className={
        state === "streaming"
          ? "animate-content-ready px-5 py-3.5 sm:px-6"
          : "px-5 py-3.5 sm:px-6"
      }
      style={{ background: "oklch(from var(--signature) l c h / 0.03)" }}
    >
      <div className="flex items-start gap-2">
        <div className="flex h-[24.5px] shrink-0 items-center">
          <StarGlyph breathe={state === "streaming"} />
        </div>
        <div className="min-w-0 flex-1 text-[14px] leading-relaxed text-fg-1">
          <AISummary
            text={text ?? ""}
            sources={sources}
            onSourceClick={onSourceClick ?? noop}
            isStreaming={state === "streaming"}
          />
        </div>
      </div>
      {state === "complete" && (
        <div className="ml-5 mt-3 flex items-center gap-2">
          <button
            type="button"
            onClick={onCopy}
            className="cursor-pointer rounded bg-surface-2 px-2 py-1 text-[12px] text-fg-3 hover:text-fg-2"
          >
            {t.recall.copy}
          </button>
          <button
            type="button"
            onClick={onCopyForAI}
            className="cursor-pointer rounded bg-surface-2 px-2 py-1 text-[12px] text-fg-3 hover:text-fg-2"
          >
            {t.recall.copyForAI}
          </button>
        </div>
      )}
    </div>
  );
}
