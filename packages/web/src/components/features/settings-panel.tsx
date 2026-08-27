"use client";

import { useEffect } from "react";
import { useAuth } from "@/lib/auth";
import { useTheme } from "next-themes";
import {
  Moon,
  Sun,
  Monitor,
  LogOut,
  Settings,
  Shield,
  BookOpen,
} from "lucide-react";
import { DOCS_URL } from "@/lib/urls";
import { useMemories, memoriesTotalCount } from "@/hooks/use-memories";
import { useHubs } from "@/hooks/use-hubs";
import { useUsage, hasLimits } from "@/hooks/use-usage";
import { useLocale } from "@/i18n";
import { useSettingsDialog } from "@/contexts/settings-dialog-context";
import { useUpdateSettings } from "@/hooks/use-settings";
import { getHubDisplayName } from "@/lib/hub-display";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";

interface Props {
  open: boolean;
  onClose: () => void;
  /**
   * Where the panel pops out from. `top-right` matches v1's floating
   * avatar trigger in the top-right chrome. `bottom-left` matches v2's
   * LeftRail footer trigger so the menu opens NEAR the click instead
   * of in the opposite corner of the viewport. Default: `top-right`.
   */
  anchor?: "top-right" | "bottom-left";
}

const menuItemClass =
  "w-full flex items-center gap-2.5 px-2 py-2 rounded-lg text-[14px] text-fg-2 hover:text-fg-1 hover:bg-surface-1 transition-colors cursor-pointer";

export function SettingsPanel({ open, onClose, anchor = "top-right" }: Props) {
  const { user, hubs, activeHubId, logout } = useAuth();
  const settingsDialog = useSettingsDialog();
  const updateSettings = useUpdateSettings();
  const { theme, setTheme } = useTheme();
  const { locale, setLocale, t } = useLocale();
  const { data: memoriesPages } = useMemories();
  const { data: liveHubs } = useHubs();
  const { data: usage } = useUsage();

  // Same three-tier lookup as the settings dialog + account card.
  // Prefers the server-resolved human label ("Early Access") over
  // the raw plan id. users.plan is legacy (phase 6) and is only
  // used as a last-ditch fallback before useUsage resolves.
  const planLabel =
    (hasLimits(usage) ? usage.plan_display_name : undefined) ??
    user?.personal_plan_id ??
    user?.plan;
  // Gate on the EFFECTIVE plan id (what the label actually
  // represents), not the user's personal plan id. For a
  // personal_free user who's been elevated by a team hub (e.g.
  // hub_free_team), the effective plan is the hub's — the label
  // says "Team" and we should show it. If we gated on
  // personal_plan_id we'd hide the elevated label here while the
  // settings dialog + account card still show it, re-introducing
  // the exact cross-surface mismatch that motivated this fix.
  const effectivePlanId =
    (hasLimits(usage) ? usage.plan : undefined) ??
    user?.personal_plan_id ??
    user?.plan ??
    "";
  const showPlan =
    !!planLabel &&
    effectivePlanId !== "free" &&
    effectivePlanId !== "personal_free";

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  useEffect(() => {
    if (!open) return;
    return acquireBodyScrollLock();
  }, [open]);

  const memoryCount = memoriesTotalCount(memoriesPages);
  const hubList = liveHubs ?? hubs;
  const activeHub = hubList.find((h) => h.hub.id === activeHubId);
  const scopeLabel = activeHub
    ? getHubDisplayName(activeHub.hub, t, {
        name: user?.name,
        displayName: user?.display_name,
      })
    : "";

  if (!open) return null;

  // Snap-instant open/close — no framer motion, no opacity animation.
  // Click-outside catcher is a plain div; the panel renders directly.
  return (
    <>
      {/* SettingsPanel sits ABOVE the mobile drawer (also z-modal) so
          tapping the avatar in the drawer surfaces the panel ON TOP
          rather than behind. z-popover (70) > z-modal (60). Same
          tier as other popover-shaped surfaces. Plan 26 follow-up. */}
      <div className="fixed inset-0 z-popover" onClick={onClose} />

      <div
        className={`glass-dropdown backdrop-blur-sm fixed z-popover w-[calc(100vw-32px)] max-w-70 ${
          anchor === "bottom-left"
            ? // v2 LeftRail footer trigger — sits next to the rail
              // (~70px from the left edge accounting for inset+rail width)
              // and bottoms out above the rail's bottom inset so the panel
              // doesn't crowd the rail's user button.
              "left-4 bottom-16 md:left-20"
            : // Default: top-right. Matches v1's floating avatar trigger.
              "top-16 right-4 md:right-8"
        }`}
      >
        {/* Account */}
        <div className="px-5 pt-4 pb-3">
          <div className="flex items-center gap-3">
            {user?.avatar_url ? (
              <img
                src={user.avatar_url}
                alt=""
                className="h-9 w-9 rounded-full"
              />
            ) : (
              <div
                className="h-9 w-9 rounded-full flex items-center justify-center text-[16px] font-medium text-fg-2"
                style={{
                  background: "oklch(from var(--foreground) l c h / 0.06)",
                }}
              >
                {user?.name?.[0]?.toUpperCase()}
              </div>
            )}
            <div className="min-w-0 flex-1">
              <p className="text-[14px] font-medium text-foreground truncate">
                {user?.display_name || user?.name}
              </p>
              <p className="text-[13px] text-fg-2">
                {hubList.length > 1 && (
                  <span className="text-fg-3">{scopeLabel} · </span>
                )}
                {memoryCount === 0
                  ? t.memory.zero
                  : memoryCount === 1
                    ? t.memory.one
                    : t.memory.other.replace("{n}", String(memoryCount))}
                {showPlan && (
                  <span className="ml-1.5 text-fg-3">· {planLabel}</span>
                )}
              </p>
            </div>
            {/* Theme + language — tight inline icon buttons */}
            <div className="shrink-0 flex items-center gap-0.5">
              <button
                onClick={() => {
                  document.documentElement.classList.add("theme-transition");
                  const next =
                    theme === "light"
                      ? "dark"
                      : theme === "dark"
                        ? "system"
                        : "light";
                  setTheme(next);
                  setTimeout(
                    () =>
                      document.documentElement.classList.remove(
                        "theme-transition",
                      ),
                    500,
                  );
                }}
                className="p-1.5 rounded-lg text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
              >
                {theme === "dark" ? (
                  <Moon className="h-3.5 w-3.5" />
                ) : theme === "light" ? (
                  <Sun className="h-3.5 w-3.5" />
                ) : (
                  <Monitor className="h-3.5 w-3.5" />
                )}
              </button>
              <button
                onClick={() => {
                  const next = locale === "en" ? "zh" : "en";
                  setLocale(next);
                  // Persist the choice server-side so agentic output
                  // (board synthesis etc.) follows it and other
                  // devices converge. On PATCH failure the settings
                  // cache rolls back and LocaleServerSync snaps the
                  // UI back to the server value (plus an error
                  // toast) — the toggle is server-authoritative.
                  updateSettings.mutate({ locale: next });
                }}
                className="p-1.5 rounded-lg text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer text-[13px] font-medium"
              >
                {locale === "en" ? "中" : "EN"}
              </button>
            </div>
          </div>
        </div>

        {/* Actions — one container, one item style. Per-item wrappers
            previously had drifted padding (py-1.5 vs py-0.5, px-3 vs
            px-4) and mismatched hover states. */}
        <div className="px-3 py-1.5 flex flex-col gap-0.5">
          <button
            onClick={() => {
              onClose();
              settingsDialog.open();
            }}
            className={menuItemClass}
          >
            <Settings className="h-3.5 w-3.5" />
            {t.userSettings.title}
          </button>
          {/* Connect agents — the setup command is otherwise buried in a
              Settings disclosure, so users who install and then lose the
              command have nowhere obvious to find it again. */}
          <a
            href={DOCS_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={menuItemClass}
          >
            <BookOpen className="h-3.5 w-3.5" />
            {t.settings.documentation}
          </a>
          {user?.admin_role && (
            <a href="/admin" className={menuItemClass}>
              <Shield className="h-3.5 w-3.5" />
              {t.admin.settings.adminDashboard}
            </a>
          )}
        </div>

        <Divider />

        {/* Sign out */}
        <div className="px-3 py-1.5">
          <button
            onClick={() => {
              logout();
              onClose();
            }}
            className={menuItemClass}
          >
            <LogOut className="h-3.5 w-3.5" /> {t.settings.signOut}
          </button>
        </div>
      </div>
    </>
  );
}

function Divider() {
  return <div className="mx-3 border-t border-border/30" />;
}
