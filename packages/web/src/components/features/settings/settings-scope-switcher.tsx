"use client";

import { useState, useRef, useEffect } from "react";
import { ChevronDown, Plus, User } from "lucide-react";
import { useAuth, type HubWithRole } from "@/lib/auth";
import { useLocale } from "@/i18n";
import { getHubDisplayInitial } from "@/lib/hub-display";
import { HubBadge } from "@/components/features/hub/hub-badge";
import { HubCreateDialog } from "@/components/features/settings/hub-create-dialog";
import { acquireBodyScrollLock } from "@/lib/scroll-lock";
import type { SettingsScope } from "@/contexts/settings-dialog-context";

export function SettingsScopeSwitcher({
  scope,
  onScopeChange,
}: {
  scope: SettingsScope;
  onScopeChange: (scope: SettingsScope) => void;
}) {
  const { hubs } = useAuth();
  const { t } = useLocale();
  const [open, setOpen] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const createBackdropRef = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
        setShowCreate(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  // Scroll lock + wheel blocker for create dialog overlay
  useEffect(() => {
    if (!showCreate) return;
    const release = acquireBodyScrollLock();
    const el = createBackdropRef.current;
    const onWheel = (e: WheelEvent) => e.preventDefault();
    if (el) el.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      release();
      if (el) el.removeEventListener("wheel", onWheel);
    };
  }, [showCreate]);

  const personalHub = hubs.find((h) => h.hub.hub_type === "personal");
  const teamHubs = hubs.filter((h) => h.hub.hub_type === "team");

  const currentLabel =
    scope.kind === "you"
      ? (t.settingsScope?.you ?? "")
      : (() => {
          const hub = hubs.find((h) => h.hub.id === scope.hubId);
          return hub?.hub.name ?? "";
        })();

  const currentHub =
    scope.kind === "you"
      ? null
      : (hubs.find((h) => h.hub.id === scope.hubId) ?? null);

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => {
          setOpen(!open);
          if (open) setShowCreate(false);
        }}
        className="w-full flex items-center gap-2.5 px-3 py-2 rounded-surface text-left transition-colors cursor-pointer hover:bg-surface-2"
        style={{
          background: open
            ? "oklch(from var(--foreground) l c h / 0.05)"
            : undefined,
        }}
      >
        {scope.kind === "you" ? (
          <div
            className="h-6 w-6 rounded-md flex items-center justify-center shrink-0"
            style={{
              background: "oklch(from var(--foreground) l c h / 0.06)",
            }}
          >
            <User className="h-3.5 w-3.5 text-fg-2" />
          </div>
        ) : currentHub ? (
          <HubBadge
            kind={currentHub.hub.hub_type === "team" ? "team" : "personal"}
            label={getHubDisplayInitial(currentHub.hub, t)}
            accent={currentHub.hub.accent}
            size="lg"
          />
        ) : null}
        <span className="flex-1 text-[14px] font-medium text-foreground truncate">
          {currentLabel}
        </span>
        <ChevronDown
          className={`h-3.5 w-3.5 text-fg-3 transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

      {open && (
        <div className="glass-dropdown backdrop-blur-sm absolute left-0 right-0 top-full mt-1 z-10 overflow-hidden">
          {/* You */}
          <ScopeItem
            label={t.settingsScope?.you ?? ""}
            icon={<User className="h-3.5 w-3.5 text-fg-2" />}
            isActive={scope.kind === "you"}
            onClick={() => {
              onScopeChange({ kind: "you", section: "profile" });
              setOpen(false);
              setShowCreate(false);
            }}
          />

          <div className="h-px mx-3" style={{ background: "var(--border)" }} />

          {/* Personal hub */}
          {personalHub && (
            <HubScopeItem
              hubWithRole={personalHub}
              isActive={
                scope.kind === "hub" && scope.hubId === personalHub.hub.id
              }
              onClick={() => {
                onScopeChange({
                  kind: "hub",
                  hubId: personalHub.hub.id,
                  section: "general",
                });
                setOpen(false);
                setShowCreate(false);
              }}
              t={t}
            />
          )}

          {/* Team hubs */}
          {teamHubs.map((h) => (
            <HubScopeItem
              key={h.hub.id}
              hubWithRole={h}
              isActive={scope.kind === "hub" && scope.hubId === h.hub.id}
              onClick={() => {
                onScopeChange({
                  kind: "hub",
                  hubId: h.hub.id,
                  section: "general",
                });
                setOpen(false);
                setShowCreate(false);
              }}
              t={t}
            />
          ))}

          <div className="h-px mx-3" style={{ background: "var(--border)" }} />

          {/* Create hub CTA — opens a separate dialog */}
          <button
            onClick={() => {
              setOpen(false);
              setShowCreate(true);
            }}
            className="w-full flex items-center gap-2.5 px-3 py-2.5 text-left transition-colors cursor-pointer hover:bg-surface-1 text-fg-3 hover:text-fg-2"
          >
            <div className="h-6 w-6 rounded-md flex items-center justify-center shrink-0 border border-dashed border-border">
              <Plus className="h-3.5 w-3.5" />
            </div>
            <span className="text-[14px]">
              {t.settingsScope?.createHub ?? ""}
            </span>
          </button>
        </div>
      )}

      {/* Create hub dialog — centered modal overlay */}
      {showCreate && (
        <>
          <div
            ref={createBackdropRef}
            className="fixed inset-0 z-takeover"
            style={{
              background: "rgba(0,0,0,0.4)",
              touchAction: "none",
              overscrollBehavior: "contain",
            }}
            onClick={() => setShowCreate(false)}
            onTouchMove={(e) => e.preventDefault()}
          />
          <div className="fixed inset-0 z-takeover flex items-center justify-center pointer-events-none p-6">
            <div className="pointer-events-auto w-full max-w-md">
              <HubCreateDialog
                onCreated={(hub) => {
                  setShowCreate(false);
                  // Settings-local scope change only — does NOT call switchHub()
                  onScopeChange({
                    kind: "hub",
                    hubId: hub.id,
                    section: "general",
                  });
                }}
                onCancel={() => setShowCreate(false)}
              />
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function ScopeItem({
  label,
  icon,
  isActive,
  onClick,
}: {
  label: string;
  icon: React.ReactNode;
  isActive: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full flex items-center gap-2.5 px-3 py-2.5 text-left transition-colors cursor-pointer ${
        isActive ? "bg-surface-2" : "hover:bg-surface-1"
      }`}
    >
      <div
        className="h-6 w-6 rounded-md flex items-center justify-center shrink-0"
        style={{
          background: "oklch(from var(--foreground) l c h / 0.06)",
        }}
      >
        {icon}
      </div>
      <span
        className={`text-[14px] truncate ${isActive ? "font-medium text-foreground" : "text-fg-2"}`}
      >
        {label}
      </span>
    </button>
  );
}

function HubScopeItem({
  hubWithRole,
  isActive,
  onClick,
  t,
}: {
  hubWithRole: HubWithRole;
  isActive: boolean;
  onClick: () => void;
  t: ReturnType<typeof useLocale>["t"];
}) {
  const { hub } = hubWithRole;
  const isPersonal = hub.hub_type === "personal";
  const initial = getHubDisplayInitial(hub, t);

  return (
    <button
      onClick={onClick}
      className={`w-full flex items-center gap-2.5 px-3 py-2.5 text-left transition-colors cursor-pointer ${
        isActive ? "bg-surface-2" : "hover:bg-surface-1"
      }`}
    >
      <HubBadge
        kind={isPersonal ? "personal" : "team"}
        label={initial}
        accent={hub.accent}
        size="lg"
      />
      <span
        className={`flex-1 text-[14px] truncate ${isActive ? "font-medium text-foreground" : "text-fg-2"}`}
      >
        {hub.name}
      </span>
      {isPersonal && (
        <span className="shrink-0 rounded-md bg-surface-2 px-1.5 py-0.5 text-[10px] font-medium text-fg-3">
          {t.hubs?.personal ?? ""}
        </span>
      )}
    </button>
  );
}
