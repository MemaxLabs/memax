"use client";

import { useEffect, useState } from "react";
import {
  consumeSurfaceTransition,
  type SurfaceTransitionRequest,
} from "@/lib/recent-navigation";
import { useLocale } from "@/i18n";
import {
  getSurfaceTransitionContentTransform,
  SurfaceTransitionOverlay,
} from "@/components/features/surface-transition-overlay";
import { useIsMobile } from "@/hooks/use-is-mobile";

/**
 * Template — re-mounts on every navigation.
 * Desktop: CSS fade-in + optional surface transition overlay.
 * Mobile: instant — no fade, no overlay. Cross-tab dock swap is snap
 * per explicit user preference.
 */
export default function AppTemplate({
  children,
}: {
  children: React.ReactNode;
}) {
  const { t } = useLocale();
  const isMobile = useIsMobile();
  const [transition] = useState<SurfaceTransitionRequest | null>(() =>
    consumeSurfaceTransition(),
  );
  const [showTransitionOverlay, setShowTransitionOverlay] = useState(
    Boolean(transition) && !isMobile,
  );
  const [transitionOverlayVisible, setTransitionOverlayVisible] = useState(
    Boolean(transition) && !isMobile,
  );
  const [contentReady, setContentReady] = useState(!transition || isMobile);

  useEffect(() => {
    if (!transition || isMobile) {
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => setTransitionOverlayVisible(false));
      setContentReady(true);
    });
    const timeout = window.setTimeout(
      () => {
        setShowTransitionOverlay(false);
      },
      transition.kind === "hub-switch" ? 220 : 180,
    );
    return () => {
      window.cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
    };
  }, [transition, isMobile]);

  const hubSwitchLabel =
    transition?.kind === "hub-switch" && transition.hubName
      ? t.hubs.switchingTo.replace("{name}", transition.hubName)
      : null;

  // Mobile: render children directly, no wrapper animation.
  if (isMobile) {
    return <div className="relative">{children}</div>;
  }

  return (
    <div className="relative">
      {showTransitionOverlay && transition && (
        <SurfaceTransitionOverlay
          request={transition}
          visible={transitionOverlayVisible}
          label={hubSwitchLabel}
        />
      )}
      <div
        className={transition ? undefined : "animate-fade-in"}
        style={
          transition
            ? {
                opacity: contentReady ? 1 : 0.9,
                transform: getSurfaceTransitionContentTransform(
                  transition.kind,
                  contentReady,
                ),
                transition:
                  "opacity 0.18s var(--ease-spring), transform 0.22s var(--ease-spring)",
              }
            : undefined
        }
      >
        {children}
      </div>
    </div>
  );
}
