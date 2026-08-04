"use client";

import { useEffect, useRef, useState } from "react";
import { interpolate } from "@/i18n";

// Slow enough to read, fast enough to feel alive. Swap is two-phase:
// whole-line blur-out → change word → per-character blur-up entrance.
const ROTATE_MS = 1800;
const OUT_MS = 180;
const CHAR_STAGGER_MS = 28;

/**
 * Display headline with a rotating completion line:
 *
 *   Your                ← fixed possessive prefix
 *   second brain.       ← whole line cycles through `words`
 *
 * The incoming word enters per-character — each char rises with an 8px
 * deblur (`.landing-char-in` in globals.css), staggered left-to-right —
 * while the outgoing word exits as one line (rise + blur). Whole-line
 * rotation (vs an inline {word} slot) keeps the layout stable at display
 * sizes. Respects prefers-reduced-motion by pinning the first word.
 * Remount with a `key` when the word set changes (pivot/locale switch)
 * to reset the cycle.
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
      }, OUT_MS);
    }, ROTATE_MS);
    return () => {
      clearInterval(interval);
      if (swapTimeout.current) clearTimeout(swapTimeout.current);
    };
  }, [reduced, words.length]);

  const currentLine = interpolate(wordLine, { word: words[index] ?? "" });
  // Code-point split handles CJK; spaces become NBSP so they keep width as
  // inline-block spans.
  const chars = Array.from(currentLine);

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
        style={
          visible
            ? undefined
            : {
                // Whole-line exit: rise + deblur-in-reverse. Reset is
                // instant on re-entry (no style → no transition) so the
                // per-char entrance owns the comeback.
                opacity: 0,
                transform: "translateY(-0.12em)",
                filter: "blur(6px)",
                transition: `opacity ${OUT_MS}ms var(--ease-spring), transform ${OUT_MS}ms var(--ease-spring), filter ${OUT_MS}ms var(--ease-spring)`,
              }
        }
      >
        {reduced
          ? currentLine
          : chars.map((ch, i) => (
              <span
                key={`${index}-${i}`}
                className="landing-char-in"
                style={{ animationDelay: `${i * CHAR_STAGGER_MS}ms` }}
              >
                {ch === " " ? "\u00A0" : ch}
              </span>
            ))}
      </span>
    </h1>
  );
}
