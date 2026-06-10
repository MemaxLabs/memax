"use client";

import {
  User,
  Palette,
  Bot,
  Key,
  Link2,
  AlertTriangle,
  Settings2,
  Moon,
  Users,
  Shield,
  LogOut,
} from "lucide-react";
import { useAuth, type HubWithRole } from "@/lib/auth";
import { useLocale } from "@/i18n";
import type {
  SettingsScope,
  YouSection,
  HubSection,
} from "@/contexts/settings-dialog-context";

interface NavItem<S extends string> {
  key: S;
  icon: typeof User;
  label: string;
  /** Hide from nav when false */
  visible?: boolean;
}

export function useSettingsNavItems(scope: SettingsScope): {
  items: NavItem<string>[];
  activeSection: string;
} {
  const { hubs, user } = useAuth();
  const { t } = useLocale();

  if (scope.kind === "you") {
    // Intelligence moved to per-hub scope in the per-hub
    // intelligence release — dream settings live on the hub row,
    // so they belong on each hub's tab list, not on You.
    const items: NavItem<YouSection>[] = [
      {
        key: "profile",
        icon: User,
        label: t.userSettings?.account ?? "Profile",
      },
      {
        key: "appearance",
        icon: Palette,
        label: t.userSettings?.appearance ?? "Appearance",
      },
      { key: "agents", icon: Bot, label: t.userSettings?.agents ?? "Agents" },
      { key: "api-keys", icon: Key, label: t.apiKeys?.title ?? "API Keys" },
      {
        key: "connected-accounts",
        icon: Link2,
        label: t.userSettings?.connectedAccounts ?? "Connected Accounts",
      },
    ];
    return { items, activeSection: scope.section ?? "profile" };
  }

  // Hub scope — adapt items based on hub type and role
  const hubWithRole = hubs.find((h) => h.hub.id === scope.hubId);
  const isTeam = hubWithRole?.hub.hub_type === "team";
  const role = hubWithRole?.role ?? "viewer";
  const canManage = role === "owner" || role === "admin";

  const items: NavItem<HubSection>[] = [
    { key: "general", icon: Settings2, label: t.hubs?.general ?? "General" },
    {
      key: "appearance",
      icon: Palette,
      label: t.userSettings?.appearance ?? "Appearance",
    },
    {
      key: "intelligence",
      icon: Moon,
      label: t.userSettings?.intelligence ?? "Intelligence",
    },
    {
      key: "members",
      icon: Users,
      label: t.hubs?.membersTab ?? "Members",
      visible: isTeam,
    },
    {
      key: "permissions",
      icon: Shield,
      label: t.hubs?.permissions ?? "Permissions",
      visible: isTeam && canManage,
    },
    {
      key: "danger-zone",
      icon: AlertTriangle,
      label: t.userSettings?.dangerZone ?? "Danger Zone",
    },
  ];

  return {
    items: items.filter((i) => i.visible !== false),
    activeSection: scope.section ?? "general",
  };
}

export function SettingsNavDesktop({
  items,
  activeSection,
  onSectionChange,
  onSignOut,
}: {
  items: NavItem<string>[];
  activeSection: string;
  onSectionChange: (section: string) => void;
  onSignOut: () => void;
}) {
  const { t } = useLocale();

  return (
    <>
      <nav className="flex-1 space-y-0.5">
        {items.map(({ key, icon: Icon, label }) => (
          <button
            key={key}
            onClick={() => onSectionChange(key)}
            className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-[15px] text-left transition-colors cursor-pointer ${
              activeSection === key
                ? "bg-surface-3 text-foreground font-medium"
                : "text-fg-2 hover:text-fg-2 hover:bg-surface-2"
            }`}
          >
            <Icon className="h-4 w-4 shrink-0" />
            <span className="min-w-0 leading-snug">{label}</span>
          </button>
        ))}
      </nav>

      <button
        onClick={onSignOut}
        className="w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-[14px] text-left text-fg-3 hover:text-fg-2 hover:bg-surface-2 transition-colors cursor-pointer mt-2"
      >
        <LogOut className="h-3.5 w-3.5 shrink-0" />
        <span className="min-w-0 leading-snug">
          {t.settings?.signOut ?? "Sign out"}
        </span>
      </button>
    </>
  );
}

export function SettingsNavMobile({
  items,
  activeSection,
  onSectionChange,
}: {
  items: NavItem<string>[];
  activeSection: string;
  onSectionChange: (section: string) => void;
}) {
  return (
    <div className="flex items-center gap-1 overflow-x-auto">
      {items.map(({ key, label }) => (
        <button
          key={key}
          onClick={() => onSectionChange(key)}
          className={`px-3.5 py-1.5 rounded-lg text-[14px] whitespace-nowrap cursor-pointer transition-colors duration-150 shrink-0 ${
            activeSection === key
              ? "bg-foreground text-background font-medium"
              : "text-fg-2 hover:text-fg-2 hover:bg-surface-2"
          }`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}
