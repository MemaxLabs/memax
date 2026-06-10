"use client";

import { createPortal } from "react-dom";
import { useBar } from "@/contexts/bar-context";
import { InlineFileChips } from "./inline-file-chips";

export function BarChipPortal() {
  const { stagedFiles, detectedUrl, isMobile, mobileComposeOpen } = useBar();

  const slot =
    typeof document !== "undefined"
      ? document.getElementById("bar-chip-slot")
      : null;
  if (!slot || (isMobile && mobileComposeOpen)) return null;

  return createPortal(
    stagedFiles.length > 0 || detectedUrl ? <InlineFileChips /> : null,
    slot,
  );
}
