"use client";

// =============================================================================
// Quick-start launchers — the small surfaces that open the card deck
// (quick-start-dialog.tsx). None of them render checklist rows; the
// deck is the only place the first week is *done*.
//
//   QuickStartHeroCard   — /memories hero slot (replaces the old
//                          expanded checklist card; pinned for the
//                          checklist's lifetime, expires ~1 day after
//                          the last required step — server-side).
//   QuickStartDrawerRow  — notification drawer 入门 rows (checklist +
//                          founder note). Not counted in the bell.
//   QuickStartLauncherRow — 入门与机制 › 快速开始 tab.
// =============================================================================

import { ArrowRight, X } from "lucide-react";
import type { ChecklistPayload, Notification } from "memax-sdk";
import { useInterpolate, useLocale } from "@/i18n";
import { useResolveNotification } from "@/hooks/use-notifications";
import { openQuickStart } from "@/lib/quick-start-store";
import {
  quickStartProgress,
  useQuickStartAvailable,
} from "./quick-start-dialog";

const SIGNATURE = "var(--signature)";

export function ProgressDots({ total, done }: { total: number; done: number }) {
  return (
    <div className="flex items-center gap-1" aria-hidden>
      {Array.from({ length: total }).map((_, i) => (
        <span
          key={i}
          className="block rounded-full transition-colors"
          style={{
            width: 6,
            height: 6,
            background:
              i < done
                ? SIGNATURE
                : "oklch(from var(--foreground) l c h / 0.18)",
          }}
        />
      ))}
    </div>
  );
}

export function QuickStartHeroCard({
  notification,
}: {
  notification: Notification;
}) {
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const resolve = useResolveNotification();
  const copy = t.quickStart;
  const payload = (notification.payload ?? {}) as Partial<ChecklistPayload>;
  if (!payload.items?.length) return null;
  const progress = quickStartProgress(payload as ChecklistPayload);

  return (
    <div
      className="relative flex items-center gap-3 rounded-surface px-5 py-4"
      style={{
        background: "var(--card)",
        boxShadow:
          "0 1px 0 oklch(from var(--foreground) l c h / 0.04), 0 8px 24px -12px oklch(from var(--foreground) l c h / 0.12)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2.5">
          <h3 className="m-0 text-[14px] font-semibold text-foreground">
            {t.onboarding.checklist.title}
          </h3>
          <ProgressDots total={progress.total} done={progress.done} />
        </div>
        <p className="m-0 mt-0.5 truncate text-[12px] text-fg-3">
          {progress.allDone
            ? copy.launcherAllDone
            : interpolate(copy.launcherNext, {
                step: copy.stepLabel[progress.next],
              })}
        </p>
      </div>
      {!progress.allDone ? (
        <button
          type="button"
          onClick={() => openQuickStart(progress.next)}
          className="inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-chrome bg-foreground px-3.5 py-1.5 text-[12.5px] font-medium text-background transition-opacity hover:opacity-90 active:opacity-80"
        >
          {copy.launcherContinue}
          <ArrowRight className="h-3.5 w-3.5" strokeWidth={2.2} />
        </button>
      ) : null}
      <button
        type="button"
        onClick={() =>
          resolve.mutate({ id: notification.id, action: "dismiss" })
        }
        aria-label={t.onboarding.checklist.dismissAria}
        className="flex h-9 w-9 shrink-0 cursor-pointer items-center justify-center rounded-chrome text-fg-4 transition-colors hover:bg-surface-2 hover:text-fg-2"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}

export function QuickStartDrawerRow({
  n,
  onOpened,
}: {
  n: Notification;
  onOpened?: () => void;
}) {
  const { t } = useLocale();
  const copy = t.quickStart;
  const isChecklist = n.kind === "checklist";
  const progress = isChecklist
    ? quickStartProgress(n.payload as unknown as ChecklistPayload)
    : null;
  return (
    <button
      type="button"
      onClick={() => {
        openQuickStart(isChecklist ? progress?.next : "welcome");
        onOpened?.();
      }}
      className="flex w-full cursor-pointer items-center gap-2.5 border-t border-border/20 px-3 py-2.5 text-left transition-colors first:border-t-0 hover:bg-surface-2/60"
    >
      <span
        aria-hidden
        className="grid h-7 w-7 shrink-0 place-items-center rounded-chrome text-[13px]"
        style={{
          background: "oklch(from var(--signature) l c h / 0.12)",
          color: SIGNATURE,
        }}
      >
        ✦
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-fg-1">
          {isChecklist ? t.onboarding.checklist.title : copy.drawerWelcomeRow}
        </span>
        {progress ? (
          <span className="mt-0.5 flex items-center gap-2 text-[11.5px] text-fg-3">
            <ProgressDots total={progress.total} done={progress.done} />
            {progress.done}/{progress.total}
          </span>
        ) : null}
      </span>
      <ArrowRight className="h-3.5 w-3.5 shrink-0 text-fg-4" />
    </button>
  );
}

export function QuickStartLauncherRow({ onOpen }: { onOpen?: () => void }) {
  const { t } = useLocale();
  const copy = t.quickStart;
  const available = useQuickStartAvailable();
  if (!available) return null;
  return (
    <button
      type="button"
      onClick={() => {
        onOpen?.();
        openQuickStart();
      }}
      className="mb-4 flex w-full cursor-pointer items-center gap-3 rounded-surface px-4 py-3 text-left transition-colors hover:bg-surface-2/60"
      style={{
        background: "var(--card)",
        border: "1px solid oklch(from var(--foreground) l c h / 0.06)",
      }}
    >
      <span
        aria-hidden
        className="grid h-9 w-9 shrink-0 place-items-center rounded-chrome text-[16px] text-white"
        style={{ background: SIGNATURE }}
      >
        ✦
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-[13.5px] font-medium text-fg-1">
          {copy.openFromMechanism}
        </span>
        <span className="block text-[12px] text-fg-3">
          {copy.openFromMechanismHint}
        </span>
      </span>
      <ArrowRight className="h-4 w-4 shrink-0 text-fg-4" />
    </button>
  );
}
