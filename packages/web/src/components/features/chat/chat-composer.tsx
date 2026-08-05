"use client";

/**
 * ChatComposer — bottom-of-thread textarea + send affordance.
 *
 * Visual language is aligned with the global bar (glass-bar on
 * desktop, flat fill on mobile, --app-radius-surface, focus shadow
 * tied to --signature) so Brain feels like one cohesive surface
 * across the bar overlay (⌘K) and the inline chat composer.
 *
 * Send semantics — industry standard for chat (ChatGPT / Claude /
 * Cursor):
 *   - Desktop: Enter sends, Shift+Enter inserts newline.
 *   - Mobile: Return inserts newline; the on-screen Send button is
 *     the only way to dispatch (avoids fat-fingered sends from
 *     virtual keyboards).
 *   - Esc cancels an in-flight stream when one is running.
 *
 * Auto-resize is keystroke-driven: textarea grows to max-h-48
 * (192px ≈ 8 lines), then internal scroll takes over.
 *
 * Regenerate was removed: turn-level regeneration belongs on the
 * specific assistant message (per-row affordance), not on a
 * composer that is meant to be the user's input surface.
 */

import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { ArrowUp, Square } from "lucide-react";
import { cn } from "@memaxlabs/ui";
import { useLocale } from "@/i18n";
import { useIsMobile } from "@/hooks/use-is-mobile";

export interface ChatComposerHandle {
  focus: () => void;
  blur: () => void;
  clear: () => void;
}

export interface ChatComposerProps {
  /** Controlled value. Parent owns it so submit + clear are atomic. */
  value: string;
  onChange: (next: string) => void;
  onSend: () => void;
  /**
   * In-flight assistant message id, when one exists. When set the
   * composer renders a Stop button alongside disabled send. null
   * for "no turn in flight" — send is enabled.
   */
  inflightAssistantId: string | null;
  onCancel?: () => void;
  /** Disabled when no session is selected yet. */
  disabled?: boolean;
  composerRef?: React.RefObject<ChatComposerHandle | null>;
}

export function ChatComposer({
  value,
  onChange,
  onSend,
  inflightAssistantId,
  onCancel,
  disabled,
  composerRef,
}: ChatComposerProps) {
  const { t } = useLocale();
  const isMobile = useIsMobile();
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const resize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 192)}px`;
  }, []);

  useEffect(() => {
    resize();
  }, [value, resize]);

  useImperativeHandle(
    composerRef,
    () => ({
      focus: () => textareaRef.current?.focus(),
      blur: () => textareaRef.current?.blur(),
      clear: () => onChange(""),
    }),
    [onChange],
  );

  const trimmed = value.trim();
  const sendDisabled =
    disabled || trimmed.length === 0 || Boolean(inflightAssistantId);
  const showStop = Boolean(inflightAssistantId) && Boolean(onCancel);

  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter") {
      // Mobile: never trap Enter — the on-screen Send button is the
      // explicit action, Return = newline. Avoids accidental sends
      // from autocomplete/swipe-typing on iOS.
      if (isMobile) return;
      // IME composition (CJK input): Enter commits the candidate, do
      // not send mid-composition.
      if (e.nativeEvent.isComposing) return;
      // Shift+Enter inserts a newline; plain Enter sends.
      if (e.shiftKey) return;
      e.preventDefault();
      if (!sendDisabled) onSend();
      return;
    }
    if (e.key === "Escape" && showStop && onCancel) {
      e.preventDefault();
      onCancel();
    }
  };

  return (
    <div
      className={cn(
        "relative flex w-full items-end gap-2",
        // Bar-aligned surface: --app-radius-surface, glass on
        // desktop / flat fill on mobile, focus shadow on --signature.
        "rounded-[var(--app-radius-surface)] px-3 py-2.5",
        isMobile
          ? // EXACTLY the mobile bar shell's classes
            // (MobileThumbBarShell: rounded-surface + border-border/50)
            // so the two input surfaces are the same silhouette — one
            // pill grammar, not near-identical siblings.
            "rounded-surface border border-border/50 bg-background"
          : "glass-bar backdrop-blur-sm border border-border/40",
      )}
      style={
        isMobile
          ? undefined
          : {
              boxShadow:
                "inset 0 1px 0 var(--glass-inset-highlight), 0 8px 24px oklch(0 0 0 / 0.06), 0 2px 6px oklch(0 0 0 / 0.04)",
            }
      }
    >
      <textarea
        ref={textareaRef}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder={t.chat.composer.placeholder}
        disabled={disabled}
        rows={1}
        aria-label={t.chat.composer.placeholder}
        // 24px line-height matches the bar input; max-h-48 caps
        // multi-line growth before internal scroll engages.
        className={cn(
          "flex-1 resize-none bg-transparent text-[15px] leading-[24px] outline-none",
          "min-h-[24px] max-h-48 overflow-y-auto py-1.5 placeholder:text-fg-3",
        )}
      />
      <div className="flex items-center self-end">
        {showStop ? (
          <button
            type="button"
            onClick={onCancel}
            className="flex h-9 w-9 items-center justify-center rounded-full bg-foreground text-background transition-opacity hover:opacity-90"
            aria-label={t.chat.composer.cancel}
            title={t.chat.composer.cancel}
          >
            <Square className="h-3.5 w-3.5 fill-current" aria-hidden />
          </button>
        ) : (
          <button
            type="button"
            onClick={onSend}
            disabled={sendDisabled}
            className={cn(
              "flex h-9 w-9 items-center justify-center rounded-full transition-opacity",
              sendDisabled
                ? "bg-surface-2 text-fg-4"
                : "text-white hover:opacity-90",
            )}
            style={
              sendDisabled ? undefined : { background: "var(--signature)" }
            }
            aria-label={t.chat.composer.send}
            title={`${t.chat.composer.send} · ${t.chat.composer.sendShortcut}`}
          >
            <ArrowUp className="h-4 w-4" aria-hidden />
          </button>
        )}
      </div>
    </div>
  );
}
