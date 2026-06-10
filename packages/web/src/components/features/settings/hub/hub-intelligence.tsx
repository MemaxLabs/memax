"use client";

import { useState } from "react";
import { useLocale, useInterpolate } from "@/i18n";
import { useHubDetail } from "@/hooks/use-hub-management";
import { useUpdateHubSettings } from "@/hooks/use-hub-settings";
import { useDreamIntelligence } from "@/hooks/use-dream-intelligence";
import { useDreamTrigger } from "@/hooks/use-dreams";
import { useDreamRunStatus } from "@/hooks/use-dream-run-status";
import { useDreamUsage } from "@/hooks/use-dream-usage";
import { renderDreamQuotaHint } from "@/lib/dream-quota-hint";
import { DREAM_SETTINGS_BG } from "@memaxlabs/ui/tokens/dream";
import { ToggleRow, DreamStat } from "../shared/section";
import { HubDreamHistory } from "./hub-dream-history";

/**
 * Hub-scoped phase keys. Mirrors server model.HubSettingsAllowedKeys.
 * Adding a new phase means updating both sides + the i18n keys below.
 */
type PhaseKey =
  | "dreams_enabled"
  | "dreams_merge_enabled"
  | "dreams_archive_enabled"
  | "dreams_organize_enabled"
  | "dreams_restructure_enabled";

/**
 * Engine built-in defaults for each phase. When hub.settings has no
 * explicit entry for a phase, this is what the server's
 * DefaultSettings() resolves to. Mirrored client-side so the UI can
 * render the effective-state toggle without a second round-trip on
 * every render.
 */
const PHASE_DEFAULT: Record<PhaseKey, boolean> = {
  dreams_enabled: true,
  dreams_merge_enabled: true,
  dreams_archive_enabled: true,
  dreams_organize_enabled: true,
  dreams_restructure_enabled: true,
};

/**
 * HubIntelligence — per-hub dream settings + live state for a single
 * hub (personal OR team). Before the per-hub-intelligence release
 * this lived as YouIntelligence (personal toggles) + HubOverrides
 * (team overrides), each with its own model. After migration 018
 * there is one model: hub.settings. This component renders the
 * same shape for every hub scope, with different copy only where
 * it genuinely reads differently (e.g. "your memories" for personal,
 * "your team's memories" for team — driven by hub_type).
 *
 * Permissions — two separate gates, matching the server:
 *   - Settings toggles: owner / admin only. Server enforces this
 *     on the hub update endpoint (handler/hubs.go Update).
 *   - "Run a dream now" button: owner / admin, OR contributor
 *     when the hub has `allow_contributor_dreams=true`. Mirrors
 *     server canTriggerDreams in handler/hub_roles.go.
 * Viewers see the tab but every affordance is disabled.
 *
 * Run status is scoped to the `hubId` prop (via useDreamRunStatus
 * passing the hubId through to useDreamSurfaceState), so opening
 * Hub B's Intelligence tab while the app is active in Hub A shows
 * Hub B's dream status correctly — no leak from the active hub.
 */
export function HubIntelligence({ hubId }: { hubId: string }) {
  const { t } = useLocale();
  const interpolate = useInterpolate();

  const { data: hubDetail, isLoading: hubLoading } = useHubDetail(hubId);
  const updateSettings = useUpdateHubSettings();
  const { snapshot } = useDreamIntelligence(hubId);
  const latestRun = snapshot.latestRun;
  const pending = snapshot.pending;
  const dreamTrigger = useDreamTrigger(hubId);
  const dreamStatus = useDreamRunStatus(hubId);
  const { data: dreamUsage } = useDreamUsage(hubId);
  const isRunning = dreamStatus.status === "running";
  const triggerPending = dreamTrigger.isPending;
  const isFrozen = hubDetail?.is_frozen === true;
  // Quota state. We trust the server's `allowed` flag — soft mode
  // intentionally returns allowed=true past finite-cap exhaustion
  // during Phase 1 rollout, so the button stays clickable but the
  // hint copy still shows "you'd be over". Hard mode + at-cap or
  // limit=0 (tier disabled) produces allowed=false — the gating
  // case. Nil usage (loading or error) defaults to "no gate" so a
  // stale fetch never blocks a working button.
  const quotaBlocks = dreamUsage ? !dreamUsage.allowed : false;

  // All hooks have run; safe to branch on local view state.
  const [view, setView] = useState<"overview" | "history">("overview");
  if (view === "history") {
    return <HubDreamHistory hubId={hubId} onBack={() => setView("overview")} />;
  }

  // canManage gates the settings toggles (server: owner/admin only
  // on the hub-update endpoint).
  const canManage = hubDetail?.role === "owner" || hubDetail?.role === "admin";
  // canTrigger is a looser gate that matches server canTriggerDreams:
  // owner/admin always, plus contributors on hubs that enabled
  // `allow_contributor_dreams`. Viewers and contributors on hubs
  // without that flag cannot trigger.
  const canTrigger =
    canManage ||
    (hubDetail?.role === "contributor" &&
      hubDetail.hub.allow_contributor_dreams === true);
  const settings = (hubDetail?.hub.settings ?? {}) as Partial<
    Record<PhaseKey, boolean>
  >;
  const effective = (key: PhaseKey): boolean =>
    settings[key] ?? PHASE_DEFAULT[key];
  const triggerDisabled =
    !canTrigger ||
    isRunning ||
    triggerPending ||
    isFrozen ||
    !effective("dreams_enabled") ||
    quotaBlocks;

  const handleToggle = (key: PhaseKey) => {
    if (!canManage) return;
    const nextValue = !effective(key);
    // Send null when the toggle lands on the built-in default —
    // keeps hub.settings minimal (no row where none is needed) and
    // lets a future default-change propagate without explicit resets.
    updateSettings.mutate({
      hubId,
      settings: {
        [key]: nextValue === PHASE_DEFAULT[key] ? null : nextValue,
      },
    });
  };

  const hasLatestRunSignals = !!(
    latestRun &&
    (latestRun.merged > 0 ||
      latestRun.contradictionsFound > 0 ||
      latestRun.archived > 0 ||
      latestRun.organized > 0 ||
      latestRun.restructured > 0)
  );

  if (hubLoading) {
    return (
      <div className="space-y-4 animate-pulse" aria-hidden>
        <div className="h-4 w-48 bg-surface-2 rounded" />
        <div className="h-40 bg-surface-2 rounded-surface" />
      </div>
    );
  }

  return (
    <>
      <p className="text-[13px] text-fg-3 mb-4 px-1">
        {t.hubIntelligence.dreamSettingsHub}
      </p>
      <section className="mb-8">
        <p className="text-[13px] text-fg-3 font-medium uppercase tracking-wider mb-2.5 px-1">
          {t.dreams.title}
        </p>
        <div
          className="rounded-surface px-5 py-4 overflow-hidden relative"
          style={{
            border: "1px solid oklch(0.62 0.16 290 / 0.15)",
            background: DREAM_SETTINGS_BG,
          }}
        >
          <div className="relative z-10 space-y-1">
            <ToggleRow
              label={t.userSettings.dreamsEnabled}
              sublabel={t.userSettings.dreamsEnabledDesc}
              checked={effective("dreams_enabled")}
              onToggle={() => handleToggle("dreams_enabled")}
              variant="intelligence"
              disabled={!canManage}
            />
            {effective("dreams_enabled") && (
              <>
                <ToggleRow
                  label={t.userSettings.mergeEnabled}
                  checked={effective("dreams_merge_enabled")}
                  onToggle={() => handleToggle("dreams_merge_enabled")}
                  variant="intelligence"
                  disabled={!canManage}
                />
                <ToggleRow
                  label={t.userSettings.archiveEnabled}
                  checked={effective("dreams_archive_enabled")}
                  onToggle={() => handleToggle("dreams_archive_enabled")}
                  variant="intelligence"
                  disabled={!canManage}
                />
                <ToggleRow
                  label={t.userSettings.organizeEnabled}
                  checked={effective("dreams_organize_enabled")}
                  onToggle={() => handleToggle("dreams_organize_enabled")}
                  variant="intelligence"
                  disabled={!canManage}
                />
                <ToggleRow
                  label={t.userSettings.restructureEnabled}
                  sublabel={t.userSettings.restructureEnabledDesc}
                  checked={effective("dreams_restructure_enabled")}
                  onToggle={() => handleToggle("dreams_restructure_enabled")}
                  variant="intelligence"
                  disabled={!canManage}
                />
                <DreamTriggerButton
                  pending={triggerPending}
                  isDreaming={isRunning}
                  isFrozen={isFrozen}
                  disabled={triggerDisabled}
                  onTrigger={() => dreamTrigger.mutate(undefined)}
                  runningLabel={t.userSettings.runDreamNowRunning}
                  idleLabel={t.userSettings.runDreamNow}
                  frozenHint={t.userSettings.runDreamNowFrozenHint}
                  readOnlyHint={
                    canTrigger ? undefined : t.hubIntelligence.readOnlyHint
                  }
                  quotaHint={renderDreamQuotaHint(
                    dreamUsage,
                    {
                      used: t.hubIntelligence.quotaUsed,
                      unlimited: t.hubIntelligence.quotaUnlimited,
                      exhausted: t.hubIntelligence.quotaExhausted,
                      disabled: t.hubIntelligence.quotaDisabled,
                    },
                    interpolate,
                  )}
                />
              </>
            )}
          </div>

          {(hasLatestRunSignals || pending.total > 0) && (
            <div
              className="relative z-10 mt-4 pt-3"
              style={{
                borderTop: "1px solid oklch(0.62 0.16 290 / 0.10)",
              }}
            >
              <div className="space-y-3">
                {hasLatestRunSignals && latestRun && (
                  <div className="space-y-2">
                    <p className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
                      {t.hubIntelligence.latestDreamHeading}
                    </p>
                    <div className="space-y-2">
                      {latestRun.merged > 0 && (
                        <DreamStat
                          color="bg-emerald-500/70"
                          text={interpolate(t.dreams.merged, {
                            n: String(latestRun.merged),
                          })}
                        />
                      )}
                      {latestRun.contradictionsFound > 0 && (
                        <DreamStat
                          color="bg-amber-500/70"
                          text={interpolate(
                            t.hubIntelligence.foundContradictions,
                            { n: String(latestRun.contradictionsFound) },
                          )}
                        />
                      )}
                      {latestRun.archived > 0 && (
                        <DreamStat
                          color="bg-foreground/40"
                          text={interpolate(t.dreams.archived, {
                            n: String(latestRun.archived),
                          })}
                        />
                      )}
                      {latestRun.organized > 0 && (
                        <DreamStat
                          color="bg-blue-500"
                          text={interpolate(t.dreams.organized, {
                            n: String(latestRun.organized),
                          })}
                        />
                      )}
                      {latestRun.restructured > 0 && (
                        <DreamStat
                          color="bg-violet-500/80"
                          text={interpolate(t.dreams.restructured, {
                            n: String(latestRun.restructured),
                          })}
                        />
                      )}
                    </div>
                  </div>
                )}

                <div className="space-y-2">
                  <p className="text-[11px] font-medium uppercase tracking-wider text-fg-3">
                    {t.hubIntelligence.needsReviewHeading}
                  </p>
                  <div className="space-y-2">
                    {pending.contradictions > 0 && (
                      <DreamStat
                        color="bg-amber-500/70"
                        text={interpolate(t.dreams.contradictions, {
                          n: String(pending.contradictions),
                        })}
                      />
                    )}
                    {pending.topicMerges > 0 && (
                      <DreamStat
                        color="bg-emerald-500/70"
                        text={interpolate(
                          t.hubIntelligence.pendingTopicMerges,
                          { n: String(pending.topicMerges) },
                        )}
                      />
                    )}
                    {pending.topicRestructures > 0 && (
                      <DreamStat
                        color="bg-blue-500"
                        text={interpolate(
                          t.hubIntelligence.pendingTopicRestructures,
                          { n: String(pending.topicRestructures) },
                        )}
                      />
                    )}
                    {pending.total === 0 && (
                      <p className="text-[14px] text-fg-3">
                        {t.hubIntelligence.reviewClear}
                      </p>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </section>
      <div className="mb-6 px-1">
        <button
          type="button"
          onClick={() => setView("history")}
          className="text-[13px] text-fg-2 hover:text-fg-1 underline-offset-2 hover:underline transition-colors duration-[var(--dur-fast)] ease-[var(--ease-spring)] cursor-pointer"
        >
          {t.dreams.history.entry}
        </button>
      </div>
    </>
  );
}

/**
 * DreamTriggerButton — secondary affordance inside the Intelligence
 * card. Primary action is "just let dreams run" (the hero toggle);
 * this is the escape hatch for owners who want an immediate pass
 * ("I just imported 200 notes, dream now").
 *
 * Busy states:
 *   - isDreaming: a cycle is actually running on the server
 *     (status=running). Label flips to "Dreaming now…".
 *   - pending: mutation in-flight (brief, between click and the
 *     SSE/poll surfacing the new dream_runs row). On success the
 *     hook's meta.successMessage surfaces a toast.
 *   - isFrozen: hub subscription is cancelled/past_due or past
 *     its over-limit grace window. Server already 409s the
 *     trigger; this gates upfront so the click doesn't look like
 *     a normal working affordance.
 *   - readOnlyHint: rendered when the caller lacks manage perms
 *     (contributor/viewer on a team hub).
 *
 * The whole block is hidden when dreams_enabled=false (gated by
 * the parent), so there's no "disabled because hero is off" case
 * to handle here.
 */
function DreamTriggerButton({
  pending,
  isDreaming,
  isFrozen,
  disabled,
  onTrigger,
  runningLabel,
  idleLabel,
  frozenHint,
  readOnlyHint,
  quotaHint,
}: {
  pending: boolean;
  isDreaming: boolean;
  isFrozen: boolean;
  disabled: boolean;
  onTrigger: () => void;
  runningLabel: string;
  idleLabel: string;
  frozenHint: string;
  readOnlyHint?: string;
  quotaHint?: { text: string; tone: "info" | "warn" };
}) {
  const label = isDreaming || pending ? runningLabel : idleLabel;
  // Hint precedence: frozen > readOnly > quota. Frozen and readOnly
  // are blocking conditions the user can't fix from this surface;
  // quota is informational (count) or actionable elsewhere
  // (upgrade/wait for reset). Showing all three at once would be
  // noisy and the screen-reader description should pick the most
  // critical.
  const hintId = isFrozen
    ? "dream-trigger-frozen-hint"
    : readOnlyHint
      ? "dream-trigger-readonly-hint"
      : quotaHint
        ? "dream-trigger-quota-hint"
        : undefined;
  return (
    <div className="pt-2 pl-1">
      <button
        type="button"
        onClick={onTrigger}
        disabled={disabled}
        aria-busy={pending || isDreaming}
        aria-describedby={hintId}
        className="inline-flex items-center gap-2 rounded-full border border-border/60 px-3.5 py-1.5 text-[12.5px] font-medium text-fg-1 transition-opacity duration-[var(--dur-fast)] ease-[var(--ease-spring)] hover:bg-surface-1 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {(pending || isDreaming) && (
          <span
            aria-hidden
            className="inline-block h-1.5 w-1.5 rounded-full"
            style={{
              backgroundColor: "oklch(0.62 0.16 290)",
              animation: "pulse 1.4s var(--ease-spring) infinite",
            }}
          />
        )}
        <span>{label}</span>
      </button>
      {isFrozen && (
        <p
          id="dream-trigger-frozen-hint"
          className="mt-1.5 text-[12px] text-amber-600 dark:text-amber-400"
        >
          {frozenHint}
        </p>
      )}
      {!isFrozen && readOnlyHint && (
        <p
          id="dream-trigger-readonly-hint"
          className="mt-1.5 text-[12px] text-fg-3"
        >
          {readOnlyHint}
        </p>
      )}
      {!isFrozen && !readOnlyHint && quotaHint && (
        <p
          id="dream-trigger-quota-hint"
          className={
            quotaHint.tone === "warn"
              ? "mt-1.5 text-[12px] text-amber-600 dark:text-amber-400"
              : "mt-1.5 text-[12px] text-fg-3"
          }
        >
          {quotaHint.text}
        </p>
      )}
    </div>
  );
}
