"use client";

import { useState, useEffect, useRef } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { FAST } from "@memaxlabs/ui/tokens/motion";
import { X } from "lucide-react";
import {
  useSettingsDialog,
  type SettingsScope,
  type YouSection,
  type HubSection,
} from "@/contexts/settings-dialog-context";
import { useAuth } from "@/lib/auth";
import { useUsage, hasLimits } from "@/hooks/use-usage";
import { useLocale } from "@/i18n";
import { useMobileShell } from "@/hooks/use-is-mobile";
import { SettingsScopeSwitcher } from "./settings-scope-switcher";
import {
  useSettingsNavItems,
  SettingsNavDesktop,
  SettingsNavMobile,
} from "./settings-nav";

// You scope sections
import { YouProfile } from "./you/you-profile";
import { YouAppearance } from "./you/you-appearance";
import { AgentConfigsSection } from "@/components/features/agent-configs-section";
import { YouApiKeys } from "./you/you-api-keys";
import { YouConnectedAccounts } from "./you/you-connected-accounts";

// Hub scope sections
import { HubGeneral } from "./hub/hub-general";
import { HubAppearance } from "./hub/hub-appearance";
import { HubIntelligence } from "./hub/hub-intelligence";
import { HubMembers } from "./hub/hub-members";
import { HubPermissions } from "./hub/hub-permissions";
import { HubDangerZoneSection } from "./hub/hub-danger-zone";

function getSectionTitle(
  scope: SettingsScope,
  t: ReturnType<typeof useLocale>["t"],
): string {
  if (scope.kind === "you") {
    const s = scope.section ?? "profile";
    const labels: Record<YouSection, string> = {
      profile: t.userSettings?.account ?? "Profile",
      appearance: t.userSettings?.appearance ?? "Appearance",
      agents: t.userSettings?.agents ?? "Agents",
      "api-keys": t.apiKeys?.title ?? "API Keys",
      "connected-accounts":
        t.userSettings?.connectedAccounts ?? "Connected Accounts",
    };
    return labels[s];
  }
  const s = scope.section ?? "general";
  const labels: Record<HubSection, string> = {
    general: t.hubs?.general ?? "General",
    appearance: t.userSettings?.appearance ?? "Appearance",
    intelligence: t.userSettings?.intelligence ?? "Intelligence",
    members: t.hubs?.membersTab ?? "Members",
    permissions: t.hubs?.permissions ?? "Permissions",
    "danger-zone": t.userSettings?.dangerZone ?? "Danger Zone",
  };
  return labels[s];
}

export function SettingsDialog() {
  const { isOpen, defaultScope, close } = useSettingsDialog();
  // useMobileShell (not useIsMobile) so landscape/short-height coarse
  // viewports also get the full-screen mobile shell instead of cramming
  // a 220px sidebar + 80vh modal into the available space.
  const isMobile = useMobileShell();
  const { user, hubs, logout } = useAuth();
  const { data: usage } = useUsage();
  const { t } = useLocale();

  // Plan badge prefers (in order):
  //   1. `usage.plan_display_name` from useUsage — already
  //      resolved through the server's effective plan resolver
  //      with per-hub elevation applied (e.g. "Early Access")
  //   2. `user.personal_plan_id` — scoped personal plan id
  //   3. `user.plan` — legacy column, stale since phase 6 when
  //      personal_plan_id became the sole authority. Only hit if
  //      usage hasn't loaded AND personal_plan_id is unset.
  const planLabel =
    (hasLimits(usage) ? usage.plan_display_name : undefined) ??
    user?.personal_plan_id ??
    user?.plan;

  const personalHub = hubs.find((h) => h.hub.hub_type === "personal");
  const defaultHubId = personalHub?.hub.id ?? "";
  const backdropRef = useRef<HTMLDivElement>(null);

  const [scope, setScope] = useState<SettingsScope>({
    kind: "you",
    section: "profile",
  });

  // Reset state when dialog reopens, apply defaultScope if set
  useEffect(() => {
    if (isOpen) {
      if (defaultScope) {
        setScope(defaultScope);
      } else {
        setScope({ kind: "you", section: "profile" });
      }
    }
  }, [isOpen, defaultScope]);

  // ESC to close
  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [isOpen, close]);

  // Block wheel scroll on backdrop — must be non-passive to call preventDefault
  useEffect(() => {
    const el = backdropRef.current;
    if (!isOpen || !el) return;
    const onWheel = (e: WheelEvent) => e.preventDefault();
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [isOpen]);

  const handleSectionChange = (section: string) => {
    if (scope.kind === "you") {
      setScope({ kind: "you", section: section as YouSection });
    } else {
      setScope({ ...scope, section: section as HubSection });
    }
  };

  const handleScopeChange = (newScope: SettingsScope) => {
    setScope(newScope);
  };

  const handleSignOut = () => {
    logout();
    close();
  };

  const { items, activeSection } = useSettingsNavItems(scope);
  const sectionTitle = getSectionTitle(scope, t);

  return (
    <AnimatePresence>
      {isOpen && (
        <>
          {/* Backdrop — blocks touch scroll-through */}
          <motion.div
            className="fixed inset-0 z-modal"
            style={{
              background: "rgba(0,0,0,0.4)",
              touchAction: "none",
              overscrollBehavior: "contain",
            }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: FAST }}
            ref={backdropRef}
            onClick={close}
            onTouchMove={(e) => e.preventDefault()}
          />

          {/* Dialog */}
          <motion.div
            className="fixed inset-0 z-modal flex items-center justify-center pointer-events-none"
            style={{ padding: isMobile ? 0 : "24px" }}
            initial={{ opacity: 0, scale: 0.97 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.97 }}
            transition={{ duration: FAST, ease: [0.16, 1, 0.3, 1] }}
          >
            <div
              className={`pointer-events-auto flex overflow-hidden ${
                isMobile
                  ? // h-[100dvh] (not h-full) so when the mobile keyboard
                    // opens, the dialog resizes to the visible viewport
                    // instead of staying at full screen height with the
                    // focused input hidden under the keyboard.
                    "w-full h-[100dvh] flex-col"
                  : // Desktop Settings stays opaque by design — it's a
                    // workspace, not a floating dropdown. Glass would
                    // reduce readability on dense forms and long lists.
                    // Bar / Topic Explorer / popovers own the glass
                    // family; Settings dialog stays solid.
                    "w-full max-w-[880px] h-[80vh] max-h-180 rounded-surface"
              }`}
              style={{
                background: "var(--background)",
                border: isMobile ? "none" : "1px solid var(--border)",
                boxShadow: isMobile
                  ? "none"
                  : "0 25px 50px -12px rgba(0,0,0,0.25)",
              }}
            >
              {/* ── Mobile: scope + horizontal tabs ── */}
              {isMobile && (
                <div
                  className="shrink-0 px-4 pt-3 pb-2"
                  style={{
                    paddingTop: "max(16px, calc(8px + var(--safe-top, 0px)))",
                    background: "var(--background)",
                    borderBottom: "1px solid var(--border)",
                  }}
                >
                  <div className="flex items-center justify-between mb-2">
                    <h2
                      className="text-[19px] font-semibold text-foreground"
                      style={{ letterSpacing: "-0.02em" }}
                    >
                      {t.userSettings?.title ?? "Settings"}
                    </h2>
                    <button
                      onClick={close}
                      className="flex items-center justify-center min-h-[44px] min-w-[44px] -m-1.5 rounded-lg text-fg-3 hover:text-fg-2 transition-colors cursor-pointer"
                      aria-label={t.userSettings?.closeAria ?? "Close settings"}
                    >
                      <X className="h-5 w-5" />
                    </button>
                  </div>
                  <div className="mb-2">
                    <SettingsScopeSwitcher
                      scope={scope}
                      onScopeChange={handleScopeChange}
                    />
                  </div>
                  <SettingsNavMobile
                    items={items}
                    activeSection={activeSection}
                    onSectionChange={handleSectionChange}
                  />
                </div>
              )}

              {/* ── Desktop: left sidebar ── */}
              {!isMobile && (
                <div
                  className="w-[220px] shrink-0 flex flex-col py-5 px-3"
                  style={{ borderRight: "1px solid var(--border)" }}
                >
                  {/* Account card */}
                  <div className="px-2 mb-3">
                    <div className="flex items-center gap-2.5">
                      {user?.avatar_url ? (
                        <img
                          src={user.avatar_url}
                          alt=""
                          className="h-8 w-8 rounded-full shrink-0"
                        />
                      ) : (
                        <div
                          className="h-8 w-8 rounded-full shrink-0 flex items-center justify-center text-[14px] font-medium text-fg-2"
                          style={{
                            background:
                              "oklch(from var(--foreground) l c h / 0.06)",
                          }}
                        >
                          {user?.name?.[0]?.toUpperCase()}
                        </div>
                      )}
                      <div className="min-w-0 flex-1">
                        <p className="text-[14px] font-medium text-foreground truncate">
                          {user?.display_name || user?.name}
                        </p>
                        {planLabel && (
                          <p className="text-[12px] text-fg-3">{planLabel}</p>
                        )}
                      </div>
                    </div>
                  </div>

                  {/* Scope switcher */}
                  <div className="mb-3">
                    <SettingsScopeSwitcher
                      scope={scope}
                      onScopeChange={handleScopeChange}
                    />
                  </div>

                  {/* Nav items + sign out */}
                  <SettingsNavDesktop
                    items={items}
                    activeSection={activeSection}
                    onSectionChange={handleSectionChange}
                    onSignOut={handleSignOut}
                  />
                </div>
              )}

              {/* ── Right content ── */}
              <div
                className={`flex-1 overflow-y-auto ${
                  isMobile ? "px-5 py-5" : "py-6 px-8"
                }`}
                // Mobile content needs safe-area-aware bottom/inline
                // padding so notched devices (esp. landscape) don't
                // clip content under the home indicator or beneath
                // the cutout. Header already pads --safe-top; this
                // closes the other three insets.
                style={{
                  scrollbarGutter: "stable",
                  ...(isMobile && {
                    paddingBottom:
                      "max(24px, calc(20px + var(--safe-bottom, 0px)))",
                    paddingLeft:
                      "max(20px, calc(20px + var(--safe-left, 0px)))",
                    paddingRight:
                      "max(20px, calc(20px + var(--safe-right, 0px)))",
                  }),
                }}
              >
                {/* Desktop: section title + close */}
                {!isMobile && (
                  <div className="flex items-center justify-between mb-6">
                    <h2
                      className="text-[19px] font-semibold text-foreground"
                      style={{ letterSpacing: "-0.02em" }}
                    >
                      {sectionTitle}
                    </h2>
                    <button
                      onClick={close}
                      className="p-1.5 rounded-lg text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </div>
                )}

                {/* Section content */}
                <SettingsSectionContent
                  scope={scope}
                  onScopeChange={handleScopeChange}
                />
              </div>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}

function SettingsSectionContent({
  scope,
  onScopeChange,
}: {
  scope: SettingsScope;
  onScopeChange: (scope: SettingsScope) => void;
}) {
  if (scope.kind === "you") {
    switch (scope.section ?? "profile") {
      case "profile":
        return <YouProfile />;
      case "appearance":
        return <YouAppearance />;
      case "agents":
        return <AgentConfigsSection hideKeyManagement />;
      case "api-keys":
        return <YouApiKeys />;
      case "connected-accounts":
        return <YouConnectedAccounts />;
      default:
        return <YouProfile />;
    }
  }

  const hubId = scope.hubId;
  switch (scope.section ?? "general") {
    case "general":
      return <HubGeneral hubId={hubId} />;
    case "appearance":
      return <HubAppearance hubId={hubId} />;
    case "intelligence":
      return <HubIntelligence hubId={hubId} />;
    case "members":
      return (
        <HubMembers
          hubId={hubId}
          onNavigateToKeys={() =>
            onScopeChange({ kind: "you", section: "api-keys" })
          }
        />
      );
    case "permissions":
      return <HubPermissions hubId={hubId} />;
    case "danger-zone":
      return <HubDangerZoneSection hubId={hubId} />;
    default:
      return <HubGeneral hubId={hubId} />;
  }
}
