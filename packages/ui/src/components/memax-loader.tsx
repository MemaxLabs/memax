"use client";

import { useEffect, useRef, useState } from "react";

/**
 * MemaxLoader — page and inline loading indicators.
 *
 * Two visual modes:
 *   Full-screen (default) — breathing signature orb, centered on page.
 *     When `ready` becomes true, the orb flares outward then calls
 *     `onArrived` so the parent can unmount the loader and show content.
 *   Inline / compact — signature-colored sequential pulse dots (unchanged).
 *
 * Keyframes (memax-orb-breathe, memax-orb-flare, memax-dot-pulse) are
 * defined in globals.css to avoid hydration mismatches from inline <style>.
 */

const FLARE_MS = 500;

export function MemaxLoader({
  inline = false,
  compact = false,
  label,
  ready = false,
  onArrived,
}: {
  inline?: boolean;
  compact?: boolean;
  label?: string;
  /** When true, the full-screen orb plays the flare arrival animation. */
  ready?: boolean;
  /** Called after the flare finishes — parent should unmount the loader. */
  onArrived?: () => void;
  /** @deprecated No longer changes visual — kept for API compat */
  thinking?: boolean;
}) {
  const [flaring, setFlaring] = useState(false);
  const onArrivedRef = useRef(onArrived);
  onArrivedRef.current = onArrived;

  useEffect(() => {
    if (!ready || inline) return;
    setFlaring(true);
    const t = setTimeout(() => onArrivedRef.current?.(), FLARE_MS);
    return () => clearTimeout(t);
    // Only fire once when ready becomes true — ref handles callback identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ready, inline]);

  // ── Inline / compact: three sequential pulse dots (unchanged) ──
  if (inline) {
    const dotSize = compact ? 4 : 6;
    const gap = compact ? 4 : 6;
    return (
      <div className="flex flex-col items-center gap-3">
        <div className="flex items-center" style={{ gap }}>
          {[0, 1, 2].map((i) => (
            <span
              key={i}
              className="rounded-full"
              style={{
                width: dotSize,
                height: dotSize,
                backgroundColor: "var(--signature)",
                animation: `memax-dot-pulse 1.2s ease-in-out ${i * 0.15}s infinite`,
              }}
            />
          ))}
        </div>
        {label && <span className="text-[15px] text-fg-3">{label}</span>}
      </div>
    );
  }

  // ── Full-screen: breathing orb → flare on arrival ──
  //
  // Orb is the geometrically centered element; the label sits below it as
  // an absolutely positioned overlay so that transitions between labeled
  // and unlabeled states (e.g. Suspense fallback → CallbackHandler labeled
  // state → app-shell unlabeled state, or the mid-flare moment where the
  // label hides) do not shift the orb's Y position. Before this, the orb
  // and label shared a flex-column whose *group* was centered, which meant
  // the orb dropped/rose by ~(label height + gap) whenever `label` toggled.
  return (
    <div className="flex h-screen items-center justify-center bg-background">
      <div className="relative flex items-center justify-center">
        <div
          className="memax-orb rounded-full"
          style={{
            width: 10,
            height: 10,
            background: "var(--signature)",
            animation: flaring
              ? "memax-orb-flare 0.5s cubic-bezier(0.16, 1, 0.3, 1) forwards"
              : "memax-orb-breathe 2.4s ease-in-out infinite",
            willChange: "transform, opacity, box-shadow",
          }}
        />
        {label && !flaring && (
          <span className="pointer-events-none absolute left-1/2 top-full mt-3 block w-max max-w-[calc(100vw-2rem)] -translate-x-1/2 text-center text-[14px] text-fg-3 break-words">
            {label}
          </span>
        )}
      </div>
    </div>
  );
}
