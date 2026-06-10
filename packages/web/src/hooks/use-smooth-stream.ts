"use client";

/**
 * useSmoothStream — paces visible characters of a streaming string.
 *
 * The chat SSE stream lands `model.delta` events as soon as the
 * upstream model emits them, which produces visible jitter when
 * the upstream chunk size is irregular ("…and then" arriving as
 * 3 chars + 50 chars + 1 char back-to-back). LLM chat UIs (ChatGPT,
 * Claude.ai) hide that jitter by buffering raw chunks and revealing
 * characters at a steady pace via rAF — the visible cadence
 * decouples from the network cadence.
 *
 * Contract:
 *   - `rawText`     — the authoritative aggregate text from the
 *                     stream. May only grow during a live turn.
 *   - `terminal`    — `true` once the turn is finalized; the hook
 *                     snaps `displayed` to `rawText.length` so we
 *                     never trail final content behind the cursor.
 *   - `charsPerSec` — target reveal rate. ~120 cps matches
 *                     ChatGPT's perceived cadence; faster than that
 *                     starts to feel like a paste rather than a
 *                     stream.
 *
 * Reduced-motion: when the user prefers reduced motion, smoothing
 * is bypassed (we render the raw aggregate directly). Smooth
 * typewriter motion is decorative; users who turn it off should not
 * pay the latency.
 */

import { useEffect, useRef, useState } from "react";

interface UseSmoothStreamOpts {
  /** True once the turn is finalized (server emitted a terminal). */
  terminal: boolean;
  /** Target reveal rate; default 120 cps (~ChatGPT cadence). */
  charsPerSec?: number;
}

const DEFAULT_CPS = 120;

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined") return false;
  // jsdom doesn't implement matchMedia; treat as "no preference"
  // there so component tests render the smoothed text path
  // without crashing (vitest runs against jsdom).
  if (typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function useSmoothStream(
  rawText: string,
  { terminal, charsPerSec = DEFAULT_CPS }: UseSmoothStreamOpts,
): string {
  const [displayed, setDisplayed] = useState<string>(() =>
    terminal ? rawText : "",
  );

  const rawRef = useRef<string>(rawText);
  const displayedLenRef = useRef<number>(displayed.length);
  const rafRef = useRef<number | null>(null);
  const lastTickRef = useRef<number>(0);

  // Keep refs in sync with props each render so the rAF loop reads
  // the freshest values without re-binding the loop on every change.
  rawRef.current = rawText;

  useEffect(() => {
    // Terminal frame: snap to full text. Cancel any pending rAF —
    // the typewriter shouldn't hang behind the server's done signal.
    if (terminal) {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      displayedLenRef.current = rawText.length;
      setDisplayed(rawText);
      return;
    }

    // Reduced motion: no smoothing.
    if (prefersReducedMotion()) {
      displayedLenRef.current = rawText.length;
      setDisplayed(rawText);
      return;
    }

    // If the typewriter has caught up, idle until more raw text
    // arrives (this effect re-fires on rawText changes, so no need
    // to keep the rAF spinning while caught up).
    if (displayedLenRef.current >= rawText.length) {
      setDisplayed(rawText);
      return;
    }

    const tick = (ts: number) => {
      const dt = lastTickRef.current ? (ts - lastTickRef.current) / 1000 : 0;
      lastTickRef.current = ts;
      // Advance by at least 1 char per tick so a slow rAF (page
      // backgrounded, low-end device) still progresses; the dt
      // multiplier biases the steady-state pace toward charsPerSec.
      const advance = Math.max(1, Math.floor(charsPerSec * dt));
      const target = rawRef.current.length;
      const next = Math.min(target, displayedLenRef.current + advance);
      displayedLenRef.current = next;
      setDisplayed(rawRef.current.slice(0, next));
      if (next < target) {
        rafRef.current = requestAnimationFrame(tick);
      } else {
        rafRef.current = null;
        lastTickRef.current = 0;
      }
    };

    if (rafRef.current === null) {
      lastTickRef.current = 0;
      rafRef.current = requestAnimationFrame(tick);
    }

    return () => {
      if (rafRef.current !== null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [rawText, terminal, charsPerSec]);

  return displayed;
}
