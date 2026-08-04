"use client";

import { useEffect, useRef, useState } from "react";
import { interpolate } from "@/i18n";

// Slow enough to read, fast enough to feel alive. Swap is two-phase:
// fade out → change word → fade in, so a single element carries the cycle.
const ROTATE_MS = 2600;
const SWAP_MS = 240;

/**
 * Display headline with a rotating completion line:
 *
 *   Your                ← fixed possessive prefix
 *   second brain.       ← whole line cycles through `words`
 *
 * Whole-line rotation (vs an inline {word} slot) keeps the layout stable at
 * display sizes — no mid-line wrap jumps when a long word rotates in.
 * Respects prefers-reduced-motion by pinning the first word. Remount with a
 * `key` when the word set changes (pivot/locale switch) to reset the cycle.
 */
export function RotatingHeadline({
  prefix,
  words,
  wordLine,
}: {
  prefix: string;
  words: readonly string[];
  /** Per-locale template with a {word} slot carrying punctuation. */
  wordLine: string;
}) {
  const [index, setIndex] = useState(0);
  const [visible, setVisible] = useState(true);
  const [reduced, setReduced] = useState(false);
  const swapTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReduced(mq.matches);
    const onChange = (e: MediaQueryListEvent) => setReduced(e.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    if (reduced || words.length < 2) return;
    const interval = setInterval(() => {
      setVisible(false);
      swapTimeout.current = setTimeout(() => {
        setIndex((i) => (i + 1) % words.length);
        setVisible(true);
      }, SWAP_MS);
    }, ROTATE_MS);
    return () => {
      clearInterval(interval);
      if (swapTimeout.current) clearTimeout(swapTimeout.current);
    };
  }, [reduced, words.length]);

  const currentLine = interpolate(wordLine, { word: words[index] ?? "" });

  return (
    <h1
      className="font-bold text-fg-1 text-[3rem] sm:text-[3.75rem] lg:text-[5rem] leading-[1.02]"
      style={{ letterSpacing: "-0.045em" }}
    >
      <span className="block">{prefix}</span>
      {/* Static first word for screen readers — the animated line below is
          aria-hidden so assistive tech isn't re-announced every cycle. */}
      <span className="sr-only">
        {interpolate(wordLine, { word: words[0] ?? "" })}
      </span>
      <span
        aria-hidden
        className="block"
        style={{
          opacity: visible ? 1 : 0,
          transform: visible ? "translateY(0)" : "translateY(0.18em)",
          transition: `opacity ${SWAP_MS}ms var(--ease-spring), transform ${SWAP_MS}ms var(--ease-spring)`,
        }}
      >
        {currentLine}
      </span>
    </h1>
  );
}
