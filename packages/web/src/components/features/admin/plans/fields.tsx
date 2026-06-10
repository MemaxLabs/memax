"use client";

import * as React from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@memaxlabs/ui";

const inputClass =
  "w-full rounded-md border border-border/60 bg-background px-2.5 py-1.5 text-sm text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10 tabular-nums";

export function FieldLabel({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block space-y-1">
      <span className="block text-[12px] text-fg-3">{label}</span>
      {children}
      {hint ? (
        <span className="block text-[11px] text-fg-4">{hint}</span>
      ) : null}
    </label>
  );
}

export function TextField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <FieldLabel label={label}>
      <input
        type="text"
        className={inputClass}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </FieldLabel>
  );
}

export function NumberField({
  label,
  value,
  onChange,
  hint,
  min,
  step = 1,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  hint?: string;
  min?: number;
  step?: number;
}) {
  return (
    <FieldLabel label={label} hint={hint}>
      <input
        type="number"
        className={inputClass}
        value={Number.isFinite(value) ? value : 0}
        step={step}
        min={min}
        onChange={(e) => {
          const n = Number(e.target.value);
          onChange(Number.isFinite(n) ? n : 0);
        }}
      />
    </FieldLabel>
  );
}

export function NullableNumberField({
  label,
  value,
  onChange,
  hint,
}: {
  label: string;
  value: number | null;
  onChange: (v: number | null) => void;
  hint?: string;
}) {
  return (
    <FieldLabel label={label} hint={hint}>
      <input
        type="number"
        className={inputClass}
        value={value === null ? "" : value}
        onChange={(e) => {
          const raw = e.target.value;
          if (raw === "") {
            onChange(null);
            return;
          }
          const n = Number(raw);
          onChange(Number.isFinite(n) ? n : null);
        }}
      />
    </FieldLabel>
  );
}

// --- Bytes field ---
//
// Human-friendly byte-count input. Stores a raw byte value (int) but
// lets the operator type a number + pick a unit (B / KB / MB / GB)
// so the input never forces them to count zeros on a plan with
// 10_737_418_240. On mount the unit auto-selects the largest clean
// multiple (e.g. 10_000_000_000 → "10 GB", 1_500_000 → "1500 KB" if
// it's not a whole MB).
//
// Decimal units (1 KB = 1000 B) — standard for human-facing limits
// and what the server's existing labels use. Binary (KiB/MiB) would
// round some common plan values into fractions and confuse operators.
//
// -1 (unlimited) and 0 are passed through as-is — no conversion, no
// forced unit. Callers usually pass hint="-1 = unlimited" so the
// sentinel is discoverable.

const BYTE_UNITS: Record<string, number> = {
  B: 1,
  KB: 1_000,
  MB: 1_000_000,
  GB: 1_000_000_000,
};

const BYTE_UNIT_ORDER: ReadonlyArray<"GB" | "MB" | "KB" | "B"> = [
  "GB",
  "MB",
  "KB",
  "B",
];

// Upper bound on the byte count we'll accept — server fields are
// int64 in Go, but we clamp at Number.MAX_SAFE_INTEGER (2^53 − 1) so
// JS can represent the number exactly through the round-trip. A plan
// with 9 PB is not a realistic input, so this doesn't restrict any
// reasonable operator action.
const MAX_BYTES = Number.MAX_SAFE_INTEGER;

// bestByteUnit picks the largest unit whose quotient is at least 1 —
// so a 10 GiB plan (10_737_418_240 B) opens showing "10.73741824 GB"
// instead of "10737418240 B". We prefer the larger unit even when
// the quotient isn't an integer because the common plan-editor case
// is "at a glance, what's the cap?" — and a fractional number under
// a clean unit scans far faster than raw bytes. The displayed value
// is exact (not rounded) so the round-trip is lossless.
//
// Sentinels (value < 0) are handled by the caller; pass-through is
// "B" for safety, the caller overrides display.
function bestByteUnit(bytes: number): "GB" | "MB" | "KB" | "B" {
  if (!Number.isFinite(bytes) || bytes <= 0) return "B";
  for (const u of BYTE_UNIT_ORDER) {
    if (bytes >= BYTE_UNITS[u]) return u;
  }
  return "B";
}

export function BytesField({
  label,
  value,
  onChange,
  hint,
  unitAriaLabel,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  hint?: string;
  // Localized accessible name for the unit selector. Defaults to
  // "{label} unit" in English; callers pass the interpolated i18n
  // string so screen-reader users in any locale hear the right name.
  unitAriaLabel?: string;
}) {
  // Unit is local state so the operator can switch the lens (B → GB)
  // without the byte value changing. Initial value is auto-derived
  // from the current byte count so a 10 GiB plan opens showing
  // "10.737 GB", not "10737418240 B".
  //
  // We do NOT reset the unit when `value` changes from outside (e.g.
  // parent re-syncs after a save), because that would clobber a
  // deliberate operator choice mid-edit. The stored byte value is
  // still the source of truth; the unit only affects display + the
  // multiplier applied to new input.
  const [unit, setUnit] = React.useState<"GB" | "MB" | "KB" | "B">(() =>
    bestByteUnit(value),
  );

  // Any negative value is treated as a sentinel (-1 unlimited is the
  // one currently in use; others are reserved). Show raw, disable the
  // unit selector, and pass-through on input — otherwise a negative
  // value with GB selected would display as "-0.000000002".
  const isSentinel = value < 0;
  const effectiveUnit = isSentinel ? "B" : unit;
  const displayValue = isSentinel ? value : value / BYTE_UNITS[effectiveUnit];

  return (
    <FieldLabel label={label} hint={hint}>
      <div className="flex gap-1">
        <input
          type="number"
          className={`${inputClass} flex-1`}
          value={Number.isFinite(displayValue) ? displayValue : 0}
          step="any"
          onChange={(e) => {
            const raw = e.target.value;
            if (raw === "") {
              onChange(0);
              return;
            }
            const n = Number(raw);
            if (!Number.isFinite(n)) return;
            if (n < 0) {
              // Pass negative through as-is so -1 / -N sentinels
              // survive without unit multiplication.
              onChange(n);
              return;
            }
            // Round to integer bytes after unit conversion (avoids
            // floating-point crumbs like 1500000.0000001 from typing
            // 1.5 in MB). Clamp at MAX_BYTES so a fat-fingered
            // "1e20 GB" becomes a representable value instead of an
            // int64 overflow downstream.
            //
            // Use effectiveUnit, not the raw `unit` state — when the
            // field is in a sentinel state the displayed select is
            // forced to "B", so a positive number typed into the
            // input must also interpret as B, not the stale local
            // unit (otherwise "GB selected → -1 → 100" would store
            // 100_000_000_000 despite the UI showing "100 B").
            const bytes = Math.min(
              MAX_BYTES,
              Math.round(n * BYTE_UNITS[effectiveUnit]),
            );
            onChange(bytes);
          }}
        />
        <select
          value={effectiveUnit}
          onChange={(e) => setUnit(e.target.value as "GB" | "MB" | "KB" | "B")}
          disabled={isSentinel}
          aria-label={unitAriaLabel ?? `${label} unit`}
          className="rounded-md border border-border/60 bg-background px-2 py-1.5 text-sm text-fg-1 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <option value="B">B</option>
          <option value="KB">KB</option>
          <option value="MB">MB</option>
          <option value="GB">GB</option>
        </select>
      </div>
    </FieldLabel>
  );
}

export function SelectField<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: Array<{ value: T; label: string }>;
  onChange: (v: T) => void;
}) {
  const items = Object.fromEntries(options.map((o) => [o.value, o.label]));
  return (
    <FieldLabel label={label}>
      <Select
        value={value}
        onValueChange={(v: string) => onChange(v as T)}
        items={items}
      >
        <SelectTrigger className="py-1.5 px-3 rounded-md text-sm" />
        <SelectContent>
          {options.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </FieldLabel>
  );
}

export function ToggleField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between gap-3 rounded-md border border-border/60 bg-background px-3 py-2 cursor-pointer hover:bg-surface-1 transition-colors">
      <span className="text-[13px] text-fg-1">{label}</span>
      <input
        type="checkbox"
        className="accent-fg-1"
        checked={value}
        onChange={(e) => onChange(e.target.checked)}
      />
    </label>
  );
}

export function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="text-[11px] uppercase tracking-wider text-fg-3 font-medium">
      {children}
    </h3>
  );
}
