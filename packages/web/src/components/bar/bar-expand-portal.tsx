"use client";

import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useBar } from "@/contexts/bar-context";
import { ExpandSearchResults } from "./expand-search-results";
import { NORMAL } from "@memaxlabs/ui/tokens/motion";

/**
 * Portals content into #bar-expand-slot.
 *
 * Uses a single unified ExpandSearchResults component for all 4 layers
 * of kitchen 24c progressive search (keyword → FTS → semantic → AI).
 * No component swapping — seamless progressive enhancement.
 */
export function BarExpandPortal() {
  const { isMobile, surface } = useBar();
  const [desktopLiftSettled, setDesktopLiftSettled] = useState(true);
  const hasExpandContent = surface.showExpandSurface;

  const shouldUseLiftedPosition =
    !isMobile && surface.visibilityState === "engaged";

  useEffect(() => {
    if (isMobile) {
      setDesktopLiftSettled(true);
      return;
    }
    if (!shouldUseLiftedPosition) {
      setDesktopLiftSettled(true);
      return;
    }
    setDesktopLiftSettled(false);
    const timeout = window.setTimeout(() => {
      setDesktopLiftSettled(true);
    }, NORMAL * 1000);
    return () => window.clearTimeout(timeout);
  }, [isMobile, shouldUseLiftedPosition]);

  const slot =
    typeof document !== "undefined"
      ? document.getElementById("bar-expand-slot")
      : null;
  if (!slot) return null;

  return createPortal(
    !isMobile && hasExpandContent && desktopLiftSettled ? (
      <ExpandSearchResults />
    ) : null,
    slot,
  );
}
