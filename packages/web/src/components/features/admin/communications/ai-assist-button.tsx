"use client";

import { useState } from "react";
import { Button } from "@memaxlabs/ui";
import { Sparkles, Check, X, RotateCw } from "lucide-react";
import { useMutation } from "@tanstack/react-query";
import { useLocale } from "@/i18n";
import { ApiError } from "@/lib/api";
import { generateAIAssistEmailCopy } from "@/lib/admin-client";
import type {
  AdminAIAssistFieldKind,
  AdminAIAssistIntent,
  AdminAIAssistLocale,
  AdminAIAssistResult,
  AdminAIAssistTone,
} from "@/lib/admin-client";

/**
 * AIAssistButton is the per-field ✨ affordance that opens an inline
 * popover with tone/intent/context knobs and streams back a ready-to-
 * paste rewrite. The button is deliberately minimal — the heavy PMM
 * lifting lives in the server's system prompt, not in this UI. The
 * admin picks intent + tone + optional context; the server injects
 * memax voice, marketing best practices, and legal compliance
 * (CAN-SPAM / GDPR / CASL / ePrivacy) into the prompt.
 *
 * Apply vs regenerate vs discard is a preview-first flow: we never
 * overwrite the admin's field until they click Apply. A regenerate
 * button re-runs with the same inputs; discard closes the popover
 * without touching the field.
 */
export function AIAssistButton({
  fieldKind,
  current,
  onApply,
  defaultIntent = "polish",
  defaultTone = "warm",
}: {
  fieldKind: AdminAIAssistFieldKind;
  current: string;
  /** Invoked with the suggestion when the admin clicks Apply. The
   *  caller decides how to merge (usually: overwrite the field with
   *  the returned text). */
  onApply: (suggestion: string) => void;
  defaultIntent?: AdminAIAssistIntent;
  defaultTone?: AdminAIAssistTone;
}) {
  const { t, locale } = useLocale();
  const copy = t.admin.communications.aiAssist;

  const [open, setOpen] = useState(false);
  const [intent, setIntent] = useState<AdminAIAssistIntent>(defaultIntent);
  const [tone, setTone] = useState<AdminAIAssistTone>(defaultTone);
  const [note, setNote] = useState("");
  // Grounding is opt-in-by-default — the PMM-assistant value-prop is
  // founder-knowledge-grounded copy. Admins can turn it off for
  // blank-slate generation (e.g., writing copy for a new product
  // angle that shouldn't be contaminated by prior memos).
  const [useRecall, setUseRecall] = useState(true);

  const mutation = useMutation<AdminAIAssistResult, Error, void>({
    mutationFn: () =>
      generateAIAssistEmailCopy({
        field_kind: fieldKind,
        intent,
        tone,
        locale: resolveAssistLocale(locale),
        current,
        context: note.trim() || undefined,
        use_recall: useRecall,
      }),
  });

  function handleGenerate() {
    mutation.mutate();
  }

  function handleApply() {
    if (mutation.data) {
      onApply(mutation.data.text);
      setOpen(false);
      mutation.reset();
      setNote("");
    }
  }

  function handleDiscard() {
    setOpen(false);
    mutation.reset();
    setNote("");
  }

  // Draft intent is the only one allowed to run with empty current —
  // in that case the note alone is enough signal. Every other intent
  // (rewrite/polish/shorten/expand/translate) requires content to
  // edit; the server rejects them with `missing_current` otherwise,
  // so we mirror that gate client-side to avoid guaranteed-fail
  // round trips.
  const canGenerate =
    intent === "draft"
      ? note.trim().length > 0 || current.trim().length > 0
      : current.trim().length > 0;

  if (!open) {
    return (
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        className="cursor-pointer"
        aria-label={copy.open}
      >
        <Sparkles className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
        {copy.open}
      </Button>
    );
  }

  return (
    <div className="space-y-3 rounded-surface border border-border/60 bg-surface-1 p-3">
      <div className="flex items-start gap-2">
        <Sparkles className="mt-0.5 h-4 w-4 text-fg-2" aria-hidden="true" />
        <div className="flex-1">
          <p className="text-[12.5px] font-medium text-fg-1">{copy.title}</p>
          <p className="mt-0.5 text-[11.5px] text-fg-3">{copy.description}</p>
        </div>
        <button
          type="button"
          onClick={handleDiscard}
          className="rounded-md p-1 text-fg-3 hover:bg-surface-2 hover:text-fg-1"
          aria-label={copy.close}
        >
          <X className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>

      <div className="grid gap-2 sm:grid-cols-2">
        <Selector
          label={copy.intentLabel}
          value={intent}
          options={INTENT_OPTIONS.map((v) => ({
            value: v,
            label: copy.intents[v],
          }))}
          onChange={(v) => setIntent(v as AdminAIAssistIntent)}
        />
        <Selector
          label={copy.toneLabel}
          value={tone}
          options={TONE_OPTIONS.map((v) => ({
            value: v,
            label: copy.tones[v],
          }))}
          onChange={(v) => setTone(v as AdminAIAssistTone)}
        />
      </div>

      <label className="block">
        <span className="mb-1 block text-[11.5px] font-medium text-fg-2">
          {copy.noteLabel}
        </span>
        <textarea
          rows={2}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={copy.notePlaceholder}
          className="w-full rounded-lg border border-border/60 bg-background px-2.5 py-1.5 text-[12.5px] text-fg-1 placeholder:text-fg-3 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
          maxLength={2000}
        />
      </label>

      <label className="flex items-start gap-2 text-[12px] text-fg-2">
        <input
          type="checkbox"
          checked={useRecall}
          onChange={(e) => setUseRecall(e.target.checked)}
          className="mt-0.5 h-3.5 w-3.5 cursor-pointer rounded border-border/60 accent-fg-1"
        />
        <span>
          <span className="font-medium text-fg-1">{copy.useRecallLabel}</span>
          <span className="ml-1 text-fg-3">{copy.useRecallHint}</span>
        </span>
      </label>

      {mutation.isError ? (
        <p className="text-[11.5px] text-destructive">
          {mutation.error instanceof ApiError &&
          mutation.error.code === "ai_disabled"
            ? copy.aiDisabled
            : mutation.error.message}
        </p>
      ) : null}

      {mutation.data ? (
        <div className="rounded-md border border-border/50 bg-background p-2.5">
          <div className="mb-1 flex flex-wrap items-center justify-between gap-2">
            <p className="text-[11px] uppercase tracking-wide text-fg-3">
              {copy.suggestionLabel}
            </p>
            {/*
              Badge is driven by RESPONSE state, never by the local
              checkbox — otherwise toggling `useRecall` after
              generation would change the badge on stale data.
              Three states:
                - grounding_count > 0 → "Grounded in N notes" (emerald)
                - grounding_attempted && grounding_count === 0 →
                  "No matching notes" (muted)
                - !grounding_attempted && !grounding_available →
                  "Grounding unavailable" (muted, distinct copy)
                - otherwise silent (admin opted out explicitly)
            */}
            {mutation.data.grounding_count > 0 ? (
              <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-1.5 py-0.5 text-[10.5px] font-medium text-emerald-700 dark:text-emerald-400">
                <Sparkles className="h-2.5 w-2.5" aria-hidden="true" />
                {copy.groundedBadge.replace(
                  "{n}",
                  String(mutation.data.grounding_count),
                )}
              </span>
            ) : mutation.data.grounding_attempted ? (
              <span className="inline-flex items-center rounded-full bg-surface-2 px-1.5 py-0.5 text-[10.5px] text-fg-3">
                {copy.notGroundedBadge}
              </span>
            ) : !mutation.data.grounding_available ? (
              <span className="inline-flex items-center rounded-full bg-surface-2 px-1.5 py-0.5 text-[10.5px] text-fg-3">
                {copy.groundingUnavailableBadge}
              </span>
            ) : null}
          </div>
          <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words font-sans text-[12.5px] leading-relaxed text-fg-1">
            {mutation.data.text}
          </pre>
        </div>
      ) : null}

      <div className="flex flex-wrap items-center justify-end gap-2">
        {mutation.data ? (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleGenerate}
              disabled={mutation.isPending}
              className="cursor-pointer"
            >
              <RotateCw className="mr-1 h-3 w-3" aria-hidden="true" />
              {copy.regenerate}
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleApply}
              disabled={mutation.isPending}
              className="cursor-pointer"
            >
              <Check className="mr-1 h-3 w-3" aria-hidden="true" />
              {copy.apply}
            </Button>
          </>
        ) : (
          <Button
            type="button"
            size="sm"
            onClick={handleGenerate}
            disabled={mutation.isPending || !canGenerate}
            className="cursor-pointer"
          >
            <Sparkles className="mr-1 h-3 w-3" aria-hidden="true" />
            {mutation.isPending ? copy.generating : copy.generate}
          </Button>
        )}
      </div>
    </div>
  );
}

function Selector({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (v: string) => void;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[11.5px] font-medium text-fg-2">
        {label}
      </span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full rounded-lg border border-border/60 bg-background px-2.5 py-1.5 text-[12.5px] text-fg-1 focus:border-fg-1/40 focus:outline-none focus:ring-2 focus:ring-fg-1/10"
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
    </label>
  );
}

// Keep intent/tone vocabularies tight — matches the server's allow-list.
const INTENT_OPTIONS: AdminAIAssistIntent[] = [
  "polish",
  "rewrite",
  "shorten",
  "expand",
  "translate",
  "draft",
];
const TONE_OPTIONS: AdminAIAssistTone[] = [
  "warm",
  "professional",
  "playful",
  "urgent",
  "minimal",
  "friendly",
];

// The i18n locale string is "en" | "zh" but the assist endpoint only
// accepts those two. This is a type-narrowing shim so callers can pass
// through whatever useLocale returns without explicit casting.
function resolveAssistLocale(l: string): AdminAIAssistLocale {
  return l === "zh" ? "zh" : "en";
}
