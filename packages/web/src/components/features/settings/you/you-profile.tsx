"use client";

import { useState } from "react";
import { useAuth } from "@/lib/auth";
import { getMemaxClient } from "@/lib/memax-client";
import { useLocale, useInterpolate } from "@/i18n";
import { useUsage, hasLimits } from "@/hooks/use-usage";
import { useDevMode } from "@/hooks/use-dev-mode";
import { clearMemaxDebugEvents } from "@/lib/memax-debugger";
import { Section, ToggleRow, formatUsageCount } from "../shared/section";
import {
  useOnboardingState,
  useRestartOnboarding,
} from "@/hooks/use-notifications";

export function YouProfile() {
  const { user, activeHubId, refreshProfile } = useAuth();
  const { t } = useLocale();
  const interpolate = useInterpolate();
  const { data: usage } = useUsage();
  const {
    isDevUser,
    flags: devFlags,
    setFlag: setDevFlag,
    updateFlags: updateDevFlags,
  } = useDevMode();

  // Prefer the server-resolved effective plan from useUsage
  // (already reflects per-hub elevation and is human-readable, e.g.
  // "Early Access") over user.plan, which is the legacy column that
  // stopped being written in phase 6 and will read "free" for any
  // user upgraded via personal_plan_id.
  const planLabel =
    (hasLimits(usage) ? usage.plan_display_name : undefined) ??
    user?.personal_plan_id ??
    user?.plan;

  return (
    <>
      <Section title={t.userSettings.account}>
        <div className="flex items-center gap-3.5">
          {user?.avatar_url ? (
            <img
              src={user.avatar_url}
              alt=""
              className="h-10 w-10 rounded-full shrink-0"
            />
          ) : (
            <div
              className="h-10 w-10 rounded-full shrink-0 flex items-center justify-center text-[18px] font-medium text-fg-2"
              style={{
                background: "oklch(from var(--foreground) l c h / 0.06)",
              }}
            >
              {user?.name?.[0]?.toUpperCase()}
            </div>
          )}
          <div className="min-w-0 flex-1">
            <input
              className="text-[16px] font-semibold text-foreground bg-transparent border-b border-transparent hover:border-foreground/20 focus:border-foreground/40 focus:outline-none w-full transition-colors"
              defaultValue={user?.display_name || user?.name}
              placeholder={t.userSettings.displayName}
              onBlur={async (e) => {
                const val = e.target.value.trim();
                if (val && val !== (user?.display_name || user?.name)) {
                  await getMemaxClient().auth.updateProfile(val);
                  refreshProfile();
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") (e.target as HTMLInputElement).blur();
              }}
            />
            <p className="text-[14px] text-fg-2 truncate">{user?.email}</p>
          </div>
          {planLabel && (
            <span className="text-[13px] font-medium text-fg-2 bg-surface-2 px-2.5 py-0.5 rounded-full shrink-0">
              {planLabel}
            </span>
          )}
        </div>

        {usage &&
          (usage.push_count > 0 ||
            usage.recall_count > 0 ||
            usage.ask_count > 0) && (
            <div className="mt-4 pt-3 border-t border-border">
              <p className="text-[13px] text-fg-3 mb-2">
                {t.userSettings.usage}
              </p>
              <div className="flex gap-6 text-[14px] text-fg-2 tabular-nums">
                <span>
                  {interpolate(t.userSettings.pushes, {
                    n: formatUsageCount(
                      usage.push_count,
                      hasLimits(usage) ? usage.limits.push_limit : undefined,
                    ),
                  })}
                </span>
                <span>
                  {interpolate(t.userSettings.recalls, {
                    n: formatUsageCount(
                      usage.recall_count,
                      hasLimits(usage) ? usage.limits.recall_limit : undefined,
                    ),
                  })}
                </span>
                <span>
                  {interpolate(t.userSettings.asks, {
                    n: formatUsageCount(
                      usage.ask_count,
                      hasLimits(usage) ? usage.limits.ask_limit : undefined,
                    ),
                  })}
                </span>
              </div>
            </div>
          )}
      </Section>

      <GettingStartedSection />

      {isDevUser && (
        <DevModeSection
          devFlags={devFlags}
          setDevFlag={setDevFlag}
          updateDevFlags={updateDevFlags}
          activeHubId={activeHubId}
          userName={user?.name ?? ""}
          t={t}
        />
      )}
    </>
  );
}

// -----------------------------------------------------------------------------
// GettingStartedSection — plan 18 §3.3 / §5.4 restart-onboarding row.
// Same restart endpoint for "initial" + "restart"; the UI only varies
// the button label + helper text based on whether the user currently
// has a pending onboarding checklist.
// -----------------------------------------------------------------------------

function GettingStartedSection() {
  const { t } = useLocale();
  const copy = t.settingsOnboarding;
  const stateQuery = useOnboardingState();
  const restart = useRestartOnboarding();

  // "Start" = never had any prior row (any status). "Restart" =
  // had one (pending OR dismissed/resolved/expired). Plan 18 §6.6
  // — codex P3 L1 finding.
  const hasPrior = !!stateQuery.data?.has_prior;
  const status = stateQuery.data?.current_status;
  const buttonLabel = hasPrior ? copy.restartCta : copy.startCta;
  const description = !hasPrior
    ? copy.descriptionInitial
    : status === "pending"
      ? copy.descriptionPending
      : copy.descriptionDone;

  // Per-error copy (codex P3 L2). Map server status code → i18n
  // key. Anything else falls back to a generic message.
  const errorCopy = (() => {
    const e = restart.error as { status?: number } | undefined;
    if (!e) return null;
    if (e.status === 429) return copy.rateLimited;
    if (e.status === 401) return copy.errorUnauthorized;
    return copy.errorGeneric;
  })();

  return (
    <Section title={copy.title}>
      <div className="flex flex-col gap-3">
        <p className="text-[13px] text-fg-3 leading-relaxed">{description}</p>
        <div>
          <button
            type="button"
            onClick={() => restart.mutate()}
            disabled={restart.isPending}
            className="inline-flex items-center gap-1.5 h-9 px-4 rounded-lg text-[13px] font-medium text-foreground bg-surface-2 hover:bg-surface-3 transition-colors disabled:opacity-50"
          >
            {buttonLabel}
          </button>
        </div>
        {errorCopy && <p className="text-[12px] text-red-500">{errorCopy}</p>}
      </div>
    </Section>
  );
}

function DevModeSection({
  devFlags,
  setDevFlag,
  updateDevFlags,
  activeHubId,
  userName,
  t,
}: {
  devFlags: ReturnType<typeof useDevMode>["flags"];
  setDevFlag: ReturnType<typeof useDevMode>["setFlag"];
  updateDevFlags: ReturnType<typeof useDevMode>["updateFlags"];
  activeHubId: string | null;
  userName: string;
  t: ReturnType<typeof useLocale>["t"];
}) {
  return (
    <Section title={t.dev.title}>
      <ToggleRow
        label={t.dev.mockDreams}
        sublabel={t.dev.mockDreamsDesc}
        checked={devFlags.mockDreams}
        onToggle={() => {
          const next = !devFlags.mockDreams;
          updateDevFlags({
            mockDreams: next,
            ...(next ? { mockDreaming: false } : {}),
          });
          window.location.reload();
        }}
      />
      <ToggleRow
        label={t.dev.mockDreaming}
        sublabel={t.dev.mockDreamingDesc}
        checked={devFlags.mockDreaming}
        onToggle={() => {
          const next = !devFlags.mockDreaming;
          updateDevFlags({
            mockDreaming: next,
            ...(next ? { mockDreams: false } : {}),
          });
          window.location.reload();
        }}
      />
      <ToggleRow
        label={t.dev.mockEmptyInbox}
        sublabel={t.dev.mockEmptyInboxDesc}
        checked={devFlags.mockEmptyInbox}
        onToggle={() => setDevFlag("mockEmptyInbox", !devFlags.mockEmptyInbox)}
      />
      <ToggleRow
        label={t.dev.mockProUser}
        sublabel={t.dev.mockProUserDesc}
        checked={devFlags.mockProUser}
        onToggle={() => setDevFlag("mockProUser", !devFlags.mockProUser)}
      />
      <ToggleRow
        label={t.dev.debuggerToggle}
        sublabel={t.dev.debuggerToggleDesc}
        checked={devFlags.debuggerEnabled}
        onToggle={() => {
          const next = !devFlags.debuggerEnabled;
          setDevFlag("debuggerEnabled", next);
          if (next === false) clearMemaxDebugEvents();
        }}
      />
      <ToggleRow
        label={t.dev.skipRerank}
        sublabel={t.dev.skipRerankDesc}
        checked={devFlags.skipRerank}
        onToggle={() => setDevFlag("skipRerank", !devFlags.skipRerank)}
      />
      <ToggleRow
        label={t.dev.showChatCapabilities}
        sublabel={t.dev.showChatCapabilitiesDesc}
        checked={!!devFlags.showChatCapabilities}
        onToggle={() =>
          setDevFlag("showChatCapabilities", !devFlags.showChatCapabilities)
        }
      />
      <DevActionButton
        label={t.dev.resetDreams}
        onClick={async () => {
          for (let i = localStorage.length - 1; i >= 0; i--) {
            const key = localStorage.key(i);
            if (key?.startsWith("memax-dream-dismissed-")) {
              localStorage.removeItem(key);
            }
          }
          const { queryClient } = await import("@/lib/query-client");
          queryClient.invalidateQueries({ queryKey: ["dreams"] });
          queryClient.invalidateQueries({ queryKey: ["reviews"] });
        }}
        successText="✓"
      />
      <DevActionButton
        label={t.dev.triggerDream}
        onClick={async () => {
          await getMemaxClient().dreams.trigger(activeHubId || undefined);
        }}
        successText={t.dev.dreamTriggered}
        errorText={t.dev.dreamTriggerFailed}
      />
      <ImpersonateInput userName={userName} t={t} />
    </Section>
  );
}

function DevActionButton({
  label,
  onClick,
  successText = "✓",
  errorText,
}: {
  label: string;
  onClick: () => Promise<void>;
  successText?: string;
  errorText?: string;
}) {
  const [state, setState] = useState<"idle" | "loading" | "success" | "error">(
    "idle",
  );

  const handleClick = async () => {
    if (state === "loading") return;
    setState("loading");
    try {
      await onClick();
      setState("success");
      setTimeout(() => setState("idle"), 2000);
    } catch {
      setState("error");
      setTimeout(() => setState("idle"), 3000);
    }
  };

  return (
    <button
      onClick={handleClick}
      disabled={state === "loading"}
      className="w-full flex items-center justify-between px-3 py-3 rounded-lg text-[15px] hover:bg-surface-1 transition-colors cursor-pointer disabled:cursor-wait"
    >
      <span className="text-fg-2">{label}</span>
      <span
        className={`text-[13px] transition-colors ${
          state === "success"
            ? "text-emerald-500/70"
            : state === "error"
              ? "text-destructive/60"
              : state === "loading"
                ? "text-fg-3 state-slow-breathe"
                : "text-fg-3"
        }`}
      >
        {state === "loading"
          ? "✦"
          : state === "success"
            ? successText
            : state === "error"
              ? (errorText ?? "✕")
              : "→"}
      </span>
    </button>
  );
}

function ImpersonateInput({
  userName,
  t,
}: {
  userName: string;
  t: ReturnType<typeof useLocale>["t"];
}) {
  const [value, setValue] = useState("");
  const [state, setState] = useState<"idle" | "loading" | "error">("idle");
  const [errorMsg, setErrorMsg] = useState("");

  const handleImpersonate = async () => {
    const trimmed = value.trim();
    if (!trimmed || state === "loading") return;
    setState("loading");
    setErrorMsg("");

    const token = localStorage.getItem("memax_access_token");
    if (!token) {
      setState("error");
      setErrorMsg(t.dev.impersonateNotAuth);
      return;
    }

    const isUUID =
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
        trimmed,
      );
    const body = isUUID ? { user_id: trimmed } : { email: trimmed };

    try {
      const res = await fetch("/api/auth/impersonate", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
      });
      const json = await res.json();

      if (!res.ok) {
        setState("error");
        setErrorMsg(json?.error?.message ?? t.dev.impersonateFailed);
        setTimeout(() => setState("idle"), 3000);
        return;
      }

      const { startImpersonating } = await import("@/lib/impersonation");
      startImpersonating(userName, json.data.access_token);
    } catch {
      setState("error");
      setErrorMsg(t.dev.impersonateFailed);
      setTimeout(() => setState("idle"), 3000);
    }
  };

  return (
    <div className="pt-2 border-t border-white/5">
      <p className="text-[13px] text-fg-3 mb-1.5 px-1">{t.dev.impersonate}</p>
      <p className="text-[11px] text-fg-4 mb-2.5 px-1">
        {t.dev.impersonateDesc}
      </p>
      <div className="flex gap-2 items-center">
        <input
          type="text"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") handleImpersonate();
          }}
          placeholder={t.dev.impersonatePlaceholder}
          className="flex-1 rounded-lg bg-surface-1 border border-white/10 px-3 py-2 text-[13px] text-fg-1 placeholder:text-fg-4 outline-none focus:border-white/20 transition-colors"
        />
        <button
          onClick={handleImpersonate}
          disabled={!value.trim() || state === "loading"}
          className="rounded-lg bg-amber-500/20 text-amber-400 px-3 py-2 text-[13px] font-medium hover:bg-amber-500/30 transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed whitespace-nowrap"
        >
          {state === "loading"
            ? t.dev.impersonateLoading
            : t.dev.impersonateButton}
        </button>
      </div>
      {state === "error" && errorMsg && (
        <p className="text-[11px] text-destructive/70 mt-1.5 px-1">
          {errorMsg}
        </p>
      )}
    </div>
  );
}
