"use client";

import { useEffect, useRef, useState } from "react";
import { Pencil } from "lucide-react";
import { useLocale } from "@/i18n";
import { useUpdateHub } from "@/hooks/use-hub-management";
import { getHubDisplayInitial, getHubDisplayName } from "@/lib/hub-display";
import { HUB_ACCENTS, getHubAccentStyle, type HubAccent } from "@memaxlabs/ui";

interface HubIdentityValue {
  id: string;
  name: string;
  icon?: string;
  accent?: string;
  hub_type?: string;
  hubType?: string;
}

export function HubIdentityEditor({
  hub,
  canEdit,
  viewerName,
  viewerDisplayName,
  badge,
  subtitle,
  onUpdated,
}: {
  hub: HubIdentityValue;
  canEdit: boolean;
  viewerName?: string;
  viewerDisplayName?: string;
  badge?: React.ReactNode;
  subtitle?: React.ReactNode;
  onUpdated?: () => Promise<void> | void;
}) {
  const { t } = useLocale();
  const updateHub = useUpdateHub();
  const saveNameRef = useRef(false);
  const saveIconRef = useRef(false);
  const [editingName, setEditingName] = useState(false);
  const [editingIcon, setEditingIcon] = useState(false);
  const [nameDraft, setNameDraft] = useState("");
  const [iconDraft, setIconDraft] = useState("");
  const [accentDraft, setAccentDraft] = useState<string>(hub.accent ?? "");
  const [savingAccent, setSavingAccent] = useState(false);
  const displayName = getHubDisplayName(hub, t, {
    name: viewerName,
    displayName: viewerDisplayName,
  });
  const avatarLabel = getHubDisplayInitial(hub, t, {
    name: viewerName,
    displayName: viewerDisplayName,
  });
  const hubType = hub.hub_type ?? hub.hubType ?? "personal";
  const accentStyle = getHubAccentStyle(accentDraft || hub.accent, hubType);
  const accentLabels: Record<HubAccent, string> = {
    violet: t.hubs.accentViolet,
    blue: t.hubs.accentBlue,
    green: t.hubs.accentGreen,
    amber: t.hubs.accentAmber,
    rose: t.hubs.accentRose,
    slate: t.hubs.accentSlate,
  };

  useEffect(() => {
    if (!editingName) setNameDraft(displayName);
  }, [displayName, editingName]);

  useEffect(() => {
    if (!editingIcon) setIconDraft(hub.icon ?? "");
  }, [editingIcon, hub.icon]);

  useEffect(() => {
    setAccentDraft(hub.accent ?? "");
  }, [hub.accent]);

  const saveName = async () => {
    if (saveNameRef.current) return;
    saveNameRef.current = true;
    const trimmed = nameDraft.trim();
    try {
      if (!trimmed || trimmed === displayName) {
        setEditingName(false);
        setNameDraft(displayName);
        return;
      }
      await updateHub.mutateAsync({ hubId: hub.id, name: trimmed });
      await onUpdated?.();
      setEditingName(false);
    } finally {
      saveNameRef.current = false;
    }
  };

  const saveIcon = async () => {
    if (saveIconRef.current) return;
    saveIconRef.current = true;
    const trimmed = iconDraft.trim();
    try {
      if (trimmed === (hub.icon ?? "")) {
        setEditingIcon(false);
        return;
      }
      await updateHub.mutateAsync({ hubId: hub.id, icon: trimmed });
      await onUpdated?.();
      setEditingIcon(false);
    } finally {
      saveIconRef.current = false;
    }
  };

  const saveAccent = async (accent: HubAccent) => {
    if (savingAccent) return;
    const previous = accentDraft || hub.accent || "";
    if (accent === previous) return;
    setSavingAccent(true);
    setAccentDraft(accent);
    try {
      await updateHub.mutateAsync({ hubId: hub.id, accent });
      await onUpdated?.();
    } catch {
      setAccentDraft(previous);
    } finally {
      setSavingAccent(false);
    }
  };

  return (
    <div className="flex items-start gap-3.5">
      {editingIcon ? (
        <input
          type="text"
          value={iconDraft}
          onChange={(e) => setIconDraft(e.target.value)}
          onBlur={() => void saveIcon()}
          onKeyDown={(e) => {
            if (e.key === "Enter") void saveIcon();
            if (e.key === "Escape") {
              setEditingIcon(false);
              setIconDraft(hub.icon ?? "");
            }
          }}
          maxLength={8}
          placeholder={t.hubs.iconPlaceholder}
          className="shrink-0 h-10 w-10 rounded-chrome border border-border/60 bg-transparent text-center text-[18px] text-fg-2 outline-none"
          style={{
            background: accentStyle.background,
            color: accentStyle.foreground,
          }}
          autoFocus
        />
      ) : (
        <button
          onClick={() => {
            if (!canEdit) return;
            setEditingIcon(true);
            setIconDraft(hub.icon ?? "");
          }}
          disabled={!canEdit}
          title={canEdit ? t.hubs.changeIcon : undefined}
          className={`flex shrink-0 items-center justify-center h-10 w-10 rounded-chrome border border-border/60 text-[18px] ${canEdit ? "cursor-pointer transition-colors hover:bg-surface-1" : "cursor-default"}`}
          style={{
            background: accentStyle.background,
            color: accentStyle.foreground,
          }}
        >
          <span>{avatarLabel}</span>
        </button>
      )}

      <div className="min-w-0 flex-1 pt-0.5">
        {editingName ? (
          <input
            type="text"
            value={nameDraft}
            onChange={(e) => setNameDraft(e.target.value)}
            onBlur={() => void saveName()}
            onKeyDown={(e) => {
              if (e.key === "Enter") void saveName();
              if (e.key === "Escape") {
                setEditingName(false);
                setNameDraft(displayName);
              }
            }}
            className="w-full border-b border-foreground/30 bg-transparent text-[16px] font-semibold text-foreground outline-none"
            style={{ letterSpacing: "-0.02em" }}
            autoFocus
          />
        ) : (
          <div className="flex items-center gap-2">
            <p
              className="min-w-0 truncate text-[16px] font-semibold text-foreground"
              style={{ letterSpacing: "-0.02em" }}
            >
              {displayName}
            </p>
            {badge}
          </div>
        )}
        {subtitle ? (
          <div className="mt-0.5 text-[13px] text-fg-3">{subtitle}</div>
        ) : null}
        {canEdit ? (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <span className="text-[12px] text-fg-3">{t.hubs.accentLabel}</span>
            {HUB_ACCENTS.map((accent) => {
              const swatch = getHubAccentStyle(accent, hubType);
              const active = accent === accentStyle.key;
              return (
                <button
                  key={accent}
                  type="button"
                  onClick={() => void saveAccent(accent)}
                  disabled={savingAccent}
                  title={accentLabels[accent]}
                  className={`h-5 w-5 rounded-full border transition-transform ${
                    active
                      ? "scale-110 border-foreground/35"
                      : "border-border/60 hover:scale-105"
                  } ${savingAccent ? "cursor-wait" : "cursor-pointer"}`}
                  style={{
                    background: swatch.swatch,
                    boxShadow: active
                      ? "0 0 0 2px oklch(from var(--background) l c h / 1)"
                      : undefined,
                  }}
                />
              );
            })}
          </div>
        ) : null}
      </div>

      {canEdit && !editingName ? (
        <button
          onClick={() => {
            setEditingName(true);
            setNameDraft(displayName);
          }}
          className="shrink-0 cursor-pointer p-1 text-fg-3 transition-colors hover:text-fg-2"
          title={t.topics.rename}
        >
          <Pencil className="h-3.5 w-3.5" />
        </button>
      ) : null}
    </div>
  );
}
