"use client";

import { ArrowUp, Loader2, Search, Spotlight } from "lucide-react";

export type BarSendKind = "recall" | "remember";
export type BarSendState = "disabled" | "ready" | "loading";

export function SendButton({
  kind,
  state,
  onClick,
  ariaLabel,
}: {
  kind: BarSendKind;
  state: BarSendState;
  onClick?: () => void;
  ariaLabel?: string;
}) {
  const disabled = state === "disabled" || state === "loading";

  return (
    <button
      type="button"
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full transition-all disabled:cursor-default sm:h-8 sm:w-8 max-sm:h-11 max-sm:w-11"
      style={{
        background:
          state === "disabled"
            ? undefined
            : kind === "recall"
              ? "var(--signature)"
              : "var(--foreground)",
        color:
          state === "disabled"
            ? "var(--fg-4)"
            : kind === "recall"
              ? "#fff"
              : "var(--background)",
        opacity: state === "disabled" ? 0.4 : 1,
      }}
    >
      {kind === "recall" ? (
        state === "loading" ? (
          // Loading for recall = "the spotlight is now searching".
          // Swap Search → Spotlight with a custom sweep animation
          // (defined in globals.css) that rotates the icon ±12° —
          // reads as a stage beam actively scanning memory, not the
          // generic Loader2 spin. Different icon than ready state
          // is intentional: the swap itself signals the ask landed.
          <Spotlight className="h-4 w-4 animate-spotlight-sweep" />
        ) : (
          <Search className="h-4 w-4" />
        )
      ) : state === "loading" ? (
        // Remember (file upload) keeps the canonical Loader2 spinner
        // — it's literal "upload in progress", no search metaphor
        // applies.
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : (
        <ArrowUp className="h-4 w-4" />
      )}
    </button>
  );
}
