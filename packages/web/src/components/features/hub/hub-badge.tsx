"use client";

import { getHubAccentStyle } from "@memaxlabs/ui";

/**
 * HubBadge — small hub avatar (personal or team).
 *
 * Shape: `rounded-md` (6px) — in between square and circular.
 * At small badge sizes (16–24px) the 6px radius gives a ~25–37%
 * proportion: distinctly rounded but never reads as a circle.
 *
 * Hubs are conceptually containers (folders, projects, workspaces), so
 * the rounded-square shape differentiates them from USER avatars which
 * stay circular (contributor previews, account chip, etc.). This rule
 * is applied consistently across every hub render surface — HubBadge
 * here and HubAvatar in hub-header-banner — so settings, dropdown,
 * pinned tree, and hub header all show the same hub identity shape.
 */
export function HubBadge({
  kind,
  label,
  accent,
  size = "md",
}: {
  kind: "personal" | "team";
  label: string;
  accent?: string;
  size?: "sm" | "md" | "lg";
}) {
  const accentStyle = getHubAccentStyle(accent, kind);
  const dim =
    size === "lg"
      ? "h-6 w-6 text-[11px]"
      : size === "sm"
        ? "h-4 w-4 text-[9px]"
        : "h-5 w-5 text-[10px]";

  return (
    <div
      className={`flex shrink-0 items-center justify-center rounded-md font-medium ${dim}`}
      style={{
        background: accentStyle.background,
        color: accentStyle.foreground,
      }}
    >
      {label.trim() || "?"}
    </div>
  );
}
