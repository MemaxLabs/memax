"use client";

import { Surface } from "@memaxlabs/ui";
import {
  NEUTRAL_BORDER_OFF,
  NEUTRAL_INK,
  NEUTRAL_INK_INVERSE,
  NEUTRAL_THUMB_OFF,
  NEUTRAL_TRACK_OFF,
} from "@memaxlabs/ui/tokens/controls";

export function Section({
  title,
  children,
}: {
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mb-8">
      {title && (
        <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
          {title}
        </p>
      )}
      <Surface variant="subtle" rounded="2xl" className="px-5 py-4">
        {children}
      </Surface>
    </section>
  );
}

export function ToggleRow({
  label,
  sublabel,
  checked,
  onToggle,
  disabled,
  variant = "neutral",
}: {
  label: string;
  sublabel?: string;
  checked: boolean;
  onToggle: () => void;
  disabled?: boolean;
  /** `intelligence` uses var(--signature) purple for AI-behavior toggles. */
  variant?: "neutral" | "intelligence";
}) {
  const isIntel = variant === "intelligence";
  return (
    <button
      onClick={disabled ? undefined : onToggle}
      disabled={disabled}
      className={`w-full flex items-center gap-3 px-3 py-3 rounded-lg text-left transition-colors cursor-pointer group ${
        disabled ? "opacity-50 cursor-not-allowed" : "hover:bg-surface-1"
      }`}
    >
      <div className="flex-1 min-w-0">
        <span className="text-[15px] text-fg-2 group-hover:text-fg-1 transition-colors">
          {label}
        </span>
        {sublabel && <p className="text-[13px] text-fg-3 mt-0.5">{sublabel}</p>}
      </div>
      <div
        className="relative w-10 h-6 rounded-full border transition-colors duration-200 shrink-0"
        style={{
          backgroundColor: checked
            ? isIntel
              ? "var(--signature)"
              : NEUTRAL_INK
            : NEUTRAL_TRACK_OFF,
          borderColor: checked
            ? isIntel
              ? "var(--signature)"
              : NEUTRAL_INK
            : NEUTRAL_BORDER_OFF,
        }}
      >
        <div
          className="absolute top-0.5 w-4.5 h-4.5 rounded-full transition-transform duration-200"
          style={{
            transform: checked ? "translateX(18px)" : "translateX(2px)",
            backgroundColor: checked
              ? isIntel
                ? "white"
                : NEUTRAL_INK_INVERSE
              : NEUTRAL_THUMB_OFF,
            opacity: 1,
          }}
        />
      </div>
    </button>
  );
}

export function SelectionOption({
  label,
  sublabel,
  checked,
  onSelect,
  disabled,
}: {
  label: string;
  sublabel: string;
  checked: boolean;
  onSelect: () => void;
  /** When true, the row renders muted and cannot be selected. Used for
   *  read-only views (e.g. team hub viewers seeing owner-controlled options). */
  disabled?: boolean;
}) {
  return (
    <button
      onClick={disabled ? undefined : onSelect}
      disabled={disabled}
      aria-disabled={disabled || undefined}
      className={`flex w-full items-start gap-3 rounded-lg px-3 py-3 text-left transition-colors ${
        disabled ? "cursor-not-allowed opacity-60" : "hover:bg-surface-1"
      }`}
    >
      <span
        className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2"
        style={{
          borderColor: checked ? NEUTRAL_INK : NEUTRAL_BORDER_OFF,
        }}
      >
        {checked && (
          <span
            className="h-2 w-2 rounded-full"
            style={{ background: NEUTRAL_INK }}
          />
        )}
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-[15px] text-fg-2">{label}</p>
        <p className="mt-0.5 text-[13px] text-fg-3">{sublabel}</p>
      </div>
    </button>
  );
}

export function DreamStat({ color, text }: { color: string; text: string }) {
  return (
    <p className="flex items-center gap-2.5 text-[14px] text-fg-2">
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${color}`} />
      {text}
    </p>
  );
}

/** Format usage count with optional limit: "47" or "47 / 200" or "47 / ∞" */
export function formatUsageCount(count: number, limit?: number): string {
  if (limit === undefined) return String(count);
  if (limit < 0) return `${count} / ∞`;
  return `${count} / ${limit}`;
}
