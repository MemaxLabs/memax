"use client";

import type { SurfaceTransitionRequest } from "@/lib/recent-navigation";
import { HubBadge } from "@/components/features/hub/hub-badge";

function getTransitionOverlayBackground(
  kind: SurfaceTransitionRequest["kind"],
) {
  switch (kind) {
    case "brain-to-memory":
      return "radial-gradient(circle at 50% 38%, oklch(0.55 0.15 290 / 0.06), transparent 54%), linear-gradient(180deg, oklch(from var(--background) l c h / 0.78), oklch(from var(--background) l c h / 0.9))";
    case "memory-to-brain":
      return "radial-gradient(circle at 50% 58%, oklch(0.55 0.15 290 / 0.05), transparent 52%), linear-gradient(180deg, oklch(from var(--background) l c h / 0.9), oklch(from var(--background) l c h / 0.8))";
    case "hub-switch":
      return "radial-gradient(circle at 50% 18%, oklch(0.55 0.15 290 / 0.08), transparent 55%), oklch(from var(--background) l c h / 0.86)";
  }
}

export function getSurfaceTransitionContentTransform(
  kind: SurfaceTransitionRequest["kind"],
  ready: boolean,
) {
  if (ready) return "translateY(0px) scale(1)";
  switch (kind) {
    case "brain-to-memory":
      return "translateY(10px) scale(0.992)";
    case "memory-to-brain":
      return "translateY(-6px) scale(0.996)";
    case "hub-switch":
      return "translateY(6px) scale(0.996)";
  }
}

export function SurfaceTransitionOverlay({
  request,
  visible,
  label,
}: {
  request: SurfaceTransitionRequest;
  visible: boolean;
  label?: string | null;
}) {
  return (
    <div
      className="fixed inset-0 z-takeover transition-opacity duration-200"
      style={{
        background: getTransitionOverlayBackground(request.kind),
        opacity: visible ? 1 : 0,
      }}
    >
      {label ? (
        <div className="absolute inset-0 flex items-center justify-center">
          {/* Transition pill — neutral glass chrome (.glass-pill, same
              family as the bar + topic explorer) with a small hub icon
              leading the label. Previously used `shadow-glow` whose
              first layer was a 1px violet ring — that came off as a
              "weird purple border" around the whole pill. Dropped the
              chromatic glow; kept the hub icon (size sm) so the user
              can still see which hub they're switching into. */}
          <div className="flex items-center gap-2 rounded-full px-4 py-2 animate-fade-up glass-pill">
            {request.kind === "hub-switch" &&
            request.hubBadgeLabel &&
            request.hubKind ? (
              // size="md" (h-5 w-5) — at 20px the rounded-md (6px) radius
              // reads as a distinct rounded-square, matching the design
              // system rule that hubs are containers (rounded-square) and
              // users stay circular. size="sm" (16px) made the same badge
              // look nearly circular and conflicted with that rule.
              <HubBadge
                kind={request.hubKind}
                label={request.hubBadgeLabel}
                accent={request.hubAccent}
                size="md"
              />
            ) : null}
            <span className="text-[13px] text-fg-2">{label}</span>
          </div>
        </div>
      ) : null}
    </div>
  );
}
