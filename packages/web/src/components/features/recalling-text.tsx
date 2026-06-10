"use client";

import { useState, useEffect, useRef } from "react";
import { useLocale } from "@/i18n";

/**
 * Crossfade cycling messages with progressive timing.
 *
 * Kitchen 12 rule: gaps get longer to convey "working harder."
 * Thresholds: 0ms → 1.5s → 3s → 5s → 8s (each gap widens).
 * Animation: smooth opacity crossfade (300ms), no typewriter.
 *
 * Props:
 *   compact — smaller text for dropdown status
 *   loop — restart from first message after reaching the end (for idle cycling)
 */

const THRESHOLDS = [0, 1500, 3000, 5000, 8000];

export function RecallingText({
  compact = false,
  loop = false,
  variant = "recall",
}: {
  compact?: boolean;
  loop?: boolean;
  variant?: "recall" | "ai";
}) {
  const { t } = useLocale();
  const messages = variant === "ai" ? t.recall.aiMessages : t.recall.messages;
  const [current, setCurrent] = useState(messages[0]);
  const [fading, setFading] = useState(false);
  const startRef = useRef(0);
  const idxRef = useRef(0);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    startRef.current = performance.now();
    idxRef.current = 0;
    let transitioning = false;

    const tick = () => {
      const elapsed = performance.now() - startRef.current;
      const nextIdx = idxRef.current + 1;
      const maxIdx = Math.min(messages.length, THRESHOLDS.length);

      if (
        !transitioning &&
        nextIdx < maxIdx &&
        elapsed >= THRESHOLDS[nextIdx]
      ) {
        transitioning = true;
        setFading(true);
        setTimeout(() => {
          idxRef.current = nextIdx;
          setCurrent(messages[nextIdx]);
          setFading(false);
          transitioning = false;
        }, 300);
      }

      // Loop: reset when we've shown all messages and passed the last threshold
      if (
        loop &&
        idxRef.current >= maxIdx - 1 &&
        elapsed >= THRESHOLDS[maxIdx - 1] + 2000
      ) {
        idxRef.current = 0;
        startRef.current = performance.now();
        setCurrent(messages[0]);
      }

      rafRef.current = requestAnimationFrame(tick);
    };

    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [messages, loop]);

  const textClass = compact
    ? "text-[13px] leading-[20px] text-fg-3"
    : "text-[16px] md:text-[15px] leading-[24px] text-fg-2";

  return (
    <span
      className={`${textClass} inline-flex min-h-6 min-w-35 items-center transition-opacity duration-300`}
      style={{ opacity: fading ? 0 : 1 }}
    >
      {current}
    </span>
  );
}
