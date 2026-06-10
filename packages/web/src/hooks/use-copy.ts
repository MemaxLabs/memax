"use client";

import { useState, useCallback } from "react";

/**
 * Copy-to-clipboard hook with visual feedback state.
 * Replaces 9+ inline navigator.clipboard + setTimeout patterns.
 */
export function useCopy(feedbackMs = 2000) {
  const [copied, setCopied] = useState(false);

  const copy = useCallback(
    (text: string) => {
      navigator.clipboard.writeText(text).then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), feedbackMs);
      });
    },
    [feedbackMs],
  );

  return { copied, copy };
}
