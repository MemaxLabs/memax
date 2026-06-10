"use client";

import { useCallback, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { useBar } from "@/contexts/bar-context";
import { useLocale, useInterpolate } from "@/i18n";
import { useAutoGrow } from "@/hooks/use-auto-grow";

const MAX_HEIGHT = 200;

export function BarInputPortal() {
  const {
    value,
    inputRef,
    handleChange,
    handleKeyDown,
    addStagedFiles,
    stagedFiles,
    isBarExpanded,
    setIsBarExpanded,
    selectedMemory,
    isMobile,
    openMobileComposeActive,
    openMobileCompose,
    interaction,
    setComposing,
  } = useBar();
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const handleExpand = useCallback(
    () => setIsBarExpanded(true),
    [setIsBarExpanded],
  );
  useAutoGrow(inputRef, value, MAX_HEIGHT, isBarExpanded, handleExpand);

  // Tracks the pending rAF id from `onCompositionEnd` so a new
  // composition start can cancel it before the stale callback fires
  // (codex-review v3 L4).
  const composingEndRafRef = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (composingEndRafRef.current !== null) {
        cancelAnimationFrame(composingEndRafRef.current);
      }
    };
  }, []);

  useEffect(() => {
    if (
      !isMobile ||
      interaction.mobileComposeState === "fullscreen" ||
      !value.trim()
    )
      return;
    const el = inputRef.current;
    if (!el) return;
    if (el.scrollHeight >= 96) {
      openMobileCompose();
    }
  }, [
    inputRef,
    interaction.mobileComposeState,
    isMobile,
    openMobileCompose,
    value,
  ]);

  const slot =
    typeof document !== "undefined"
      ? document.getElementById("bar-input-slot")
      : null;
  if (!slot || (isMobile && interaction.mobileComposeState !== "docked"))
    return null;

  const placeholder = selectedMemory
    ? interpolate(t.bar.placeholder.searchIn, {
        title: selectedMemory.title.slice(0, 30),
      })
    : t.bar.placeholder.default;

  return createPortal(
    <div className="relative flex min-h-6 min-w-0 flex-1 items-center">
      {!value ? (
        <div className="pointer-events-none absolute inset-0 flex items-center overflow-hidden">
          <span className="truncate text-[16px] leading-[24px] text-fg-2">
            {stagedFiles.length > 0 ? t.remember.hintPlaceholder : placeholder}
          </span>
        </div>
      ) : null}

      <textarea
        ref={inputRef}
        value={value}
        onFocus={() => {
          if (isMobile) openMobileComposeActive();
        }}
        onChange={(e) => handleChange(e.target.value)}
        onKeyDown={handleKeyDown}
        onCompositionStart={() => {
          // Cancel any pending rAF from a prior composition end so a
          // stale callback can't flip composing=false mid-new-compose
          // (codex-review v3 L4).
          if (composingEndRafRef.current !== null) {
            cancelAnimationFrame(composingEndRafRef.current);
            composingEndRafRef.current = null;
          }
          setComposing(true);
        }}
        onCompositionEnd={() => {
          // M2 (codex review 2026-04-21): browsers fire compositionend
          // BEFORE the final committed text lands via `input`/`onChange`.
          // Flipping composing=false synchronously would let a transient
          // buffer starting with "@" briefly tick isMentionMode true
          // before the final composed text replaces it. Defer one frame
          // so handleChange commits first. The rAF id is tracked in a
          // ref so a new composition start can cancel the stale callback
          // before it fires.
          if (typeof window === "undefined") {
            setComposing(false);
            return;
          }
          if (composingEndRafRef.current !== null) {
            cancelAnimationFrame(composingEndRafRef.current);
          }
          composingEndRafRef.current = window.requestAnimationFrame(() => {
            composingEndRafRef.current = null;
            setComposing(false);
          });
        }}
        onPaste={(e) => {
          const items = e.clipboardData?.items;
          if (!items) return;
          for (const item of Array.from(items)) {
            if (!item.type.startsWith("image/")) continue;
            e.preventDefault();
            const file = item.getAsFile();
            if (!file) return;
            const reader = new FileReader();
            reader.onload = () => {
              const raw = reader.result as string;
              addStagedFiles([
                {
                  name: file.name || `paste-${Date.now()}.png`,
                  content: raw.split(",")[1] || raw,
                  binary: true,
                  contentType: file.type || item.type || "image/png",
                },
              ]);
            };
            reader.readAsDataURL(file);
            return;
          }
        }}
        rows={1}
        className="block h-6 w-full resize-none overflow-hidden bg-transparent text-[16px] leading-[24px] text-foreground outline-none scrollbar-thin"
        style={{
          minHeight: 24,
          height: 24,
          lineHeight: "24px",
          paddingTop: 0,
          paddingBottom: 0,
          margin: 0,
        }}
      />
    </div>,
    slot,
  );
}
